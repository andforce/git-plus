package task

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ImSingee/git-plus/pkg/eventbus"
	"github.com/google/uuid"
)

const (
	JobTypeSyncAll    = "sync-all"
	JobTypeSyncSource = "sync-source"
	JobIDSyncAll      = "sync-all"
	TaskChannel       = "task"
)

type State string

const (
	StateQueued   State = "queued"
	StateRunning  State = "running"
	StateFinished State = "finished"
	StateFailed   State = "failed"
)

type EventName string

const (
	EventTaskEnqueued EventName = "task.enqueued"
	EventTaskStarted  EventName = "task.started"
	EventTaskProgress EventName = "task.progress"
	EventTaskCanceled EventName = "task.canceled"
	EventTaskFinished EventName = "task.finished"
	EventTaskFailed   EventName = "task.failed"
)

var (
	ErrTaskNotFound    = errors.New("task not found")
	ErrTaskNotQueued   = errors.New("task is not queued")
	ErrManagerClosed   = errors.New("task manager is closed")
	ErrMissingJobID    = errors.New("job_id is required")
	ErrMissingJobType  = errors.New("job_type is required")
	ErrMissingTaskName = errors.New("task name is required")
	ErrMissingRunFunc  = errors.New("task run func is required")
)

type EnqueueResult int

const (
	EnqueueResultStarted EnqueueResult = iota + 1
	EnqueueResultQueued
	EnqueueResultDeduped
)

type Progress struct {
	Summary   string
	Meta      map[string]any
	UpdatedAt time.Time
}

type Snapshot struct {
	TaskID       string
	ParentTaskID string
	JobID        string
	JobType      string
	Name         string
	Args         map[string]any
	State        State
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ErrorMessage string
	Progress     *Progress
}

type Event struct {
	Name         EventName
	Task         Snapshot
	OccurredAt   time.Time
	ErrorMessage string
}

type RuntimeSnapshot struct {
	Running *Snapshot
	Queued  []Snapshot
}

type Spec struct {
	ParentTaskID string
	JobID        string
	JobType      string
	Name         string
	Args         map[string]any
	Run          func(*ExecutionContext) error
}

type ExecutionContext struct {
	manager *Manager
	taskID  string
}

type Recorder interface {
	RecordStarted(snapshot Snapshot) error
	RecordProgress(snapshot Snapshot) error
	RecordFinished(snapshot Snapshot) error
	RecordFailed(snapshot Snapshot, cause error) error
}

type Observer func(Event)

type Option func(*Manager)

type Manager struct {
	mu             sync.Mutex
	running        *entry
	queue          []*entry
	closed         bool
	now            func() time.Time
	idGen          func() string
	logger         *log.Logger
	bus            *eventbus.Bus
	recorder       Recorder
	nextObserverID uint64
	observers      map[uint64]Observer
}

type entry struct {
	taskID       string
	parentTaskID string
	jobID        string
	jobType      string
	name         string
	args         map[string]any
	state        State
	createdAt    time.Time
	startedAt    *time.Time
	finishedAt   *time.Time
	errorMessage string
	progress     *Progress
	run          func(*ExecutionContext) error
}

func NewManager(options ...Option) *Manager {
	manager := &Manager{
		now:       time.Now,
		idGen:     uuid.NewString,
		observers: make(map[uint64]Observer),
	}

	for _, option := range options {
		option(manager)
	}

	return manager
}

func WithNow(now func() time.Time) Option {
	return func(manager *Manager) {
		if now != nil {
			manager.now = now
		}
	}
}

func WithIDGenerator(idGen func() string) Option {
	return func(manager *Manager) {
		if idGen != nil {
			manager.idGen = idGen
		}
	}
}

func WithLogger(logger *log.Logger) Option {
	return func(manager *Manager) {
		manager.logger = logger
	}
}

func WithEventBus(bus *eventbus.Bus) Option {
	return func(manager *Manager) {
		manager.bus = bus
	}
}

func WithRecorder(recorder Recorder) Option {
	return func(manager *Manager) {
		manager.recorder = recorder
	}
}

func (manager *Manager) SetEventBus(bus *eventbus.Bus) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.bus = bus
}

func (manager *Manager) SetRecorder(recorder Recorder) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.recorder = recorder
}

func WithObserver(observer Observer) Option {
	return func(manager *Manager) {
		if observer == nil {
			return
		}
		manager.nextObserverID++
		manager.observers[manager.nextObserverID] = observer
	}
}

func (manager *Manager) AddObserver(observer Observer) func() {
	if observer == nil {
		return func() {}
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.nextObserverID++
	observerID := manager.nextObserverID
	manager.observers[observerID] = observer

	return func() {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		delete(manager.observers, observerID)
	}
}

func (manager *Manager) Enqueue(spec Spec) (EnqueueResult, Snapshot, error) {
	if err := validateSpec(spec); err != nil {
		return 0, Snapshot{}, err
	}

	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return 0, Snapshot{}, ErrManagerClosed
	}

	for _, queuedTask := range manager.queue {
		if queuedTask.jobID == spec.JobID {
			snapshot := queuedTask.snapshot()
			manager.mu.Unlock()
			return EnqueueResultDeduped, snapshot, nil
		}
	}

	createdAt := manager.now()
	taskEntry := &entry{
		taskID:       manager.idGen(),
		parentTaskID: spec.ParentTaskID,
		jobID:        spec.JobID,
		jobType:      spec.JobType,
		name:         spec.Name,
		args:         cloneMeta(spec.Args),
		state:        StateQueued,
		createdAt:    createdAt,
		run:          spec.Run,
	}
	enqueuedSnapshot := taskEntry.snapshot()

	if manager.running == nil {
		startedSnapshot := manager.startTaskLocked(taskEntry)
		manager.mu.Unlock()
		if err := manager.recordTaskStarted(startedSnapshot); err != nil {
			manager.failToStartTask(taskEntry.taskID, err)
			return 0, Snapshot{}, err
		}
		manager.dispatch(Event{
			Name:       EventTaskEnqueued,
			Task:       enqueuedSnapshot,
			OccurredAt: createdAt,
		})
		manager.dispatch(Event{
			Name:       EventTaskStarted,
			Task:       startedSnapshot,
			OccurredAt: startedSnapshot.StartedAtValue(),
		})
		manager.launchTask(taskEntry)
		return EnqueueResultStarted, startedSnapshot, nil
	}

	manager.queue = append(manager.queue, taskEntry)
	manager.mu.Unlock()

	manager.dispatch(Event{
		Name:       EventTaskEnqueued,
		Task:       enqueuedSnapshot,
		OccurredAt: createdAt,
	})

	return EnqueueResultQueued, enqueuedSnapshot, nil
}

func (manager *Manager) Runtime() RuntimeSnapshot {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	return manager.runtimeLocked()
}

func (manager *Manager) CancelQueuedTask(taskID string) (Snapshot, error) {
	manager.mu.Lock()

	if manager.running != nil && manager.running.taskID == taskID {
		manager.mu.Unlock()
		return Snapshot{}, ErrTaskNotQueued
	}

	for index, queuedTask := range manager.queue {
		if queuedTask.taskID != taskID {
			continue
		}

		manager.queue = append(manager.queue[:index], manager.queue[index+1:]...)
		snapshot := queuedTask.snapshot()
		occurredAt := manager.now()
		manager.mu.Unlock()

		manager.dispatch(Event{
			Name:       EventTaskCanceled,
			Task:       snapshot,
			OccurredAt: occurredAt,
		})

		return snapshot, nil
	}

	manager.mu.Unlock()
	return Snapshot{}, ErrTaskNotFound
}

func (manager *Manager) Close() {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.closed = true
}

func (ctx *ExecutionContext) SetProgress(summary string, meta map[string]any) error {
	if ctx == nil || ctx.manager == nil {
		return nil
	}

	return ctx.manager.setProgress(ctx.taskID, summary, meta)
}

func (ctx *ExecutionContext) TaskID() string {
	if ctx == nil {
		return ""
	}

	return ctx.taskID
}

func validateSpec(spec Spec) error {
	switch {
	case spec.JobID == "":
		return ErrMissingJobID
	case spec.JobType == "":
		return ErrMissingJobType
	case spec.Name == "":
		return ErrMissingTaskName
	case spec.Run == nil:
		return ErrMissingRunFunc
	default:
		return nil
	}
}

func (manager *Manager) startTaskLocked(taskEntry *entry) Snapshot {
	startedAt := manager.now()
	taskEntry.state = StateRunning
	taskEntry.startedAt = &startedAt
	manager.running = taskEntry

	return taskEntry.snapshot()
}

func (manager *Manager) launchTask(taskEntry *entry) {
	go manager.runTask(taskEntry)
}

func (manager *Manager) runTask(taskEntry *entry) {
	var runErr error

	defer func() {
		if recovered := recover(); recovered != nil {
			runErr = fmt.Errorf("panic: %v", recovered)
			if manager.logger != nil {
				manager.logger.Printf("task panic [%s]: %v", taskEntry.taskID, recovered)
			}
		}
		manager.completeTask(taskEntry.taskID, runErr)
	}()

	runErr = taskEntry.run(&ExecutionContext{
		manager: manager,
		taskID:  taskEntry.taskID,
	})
}

func (manager *Manager) completeTask(taskID string, cause error) {
	manager.mu.Lock()
	if manager.running == nil || manager.running.taskID != taskID {
		manager.mu.Unlock()
		return
	}

	terminalSnapshot := manager.running.snapshot()
	finishedAt := manager.now()
	terminalSnapshot.FinishedAt = &finishedAt
	if cause == nil {
		terminalSnapshot.State = StateFinished
		terminalSnapshot.ErrorMessage = ""
	} else {
		terminalSnapshot.State = StateFailed
		terminalSnapshot.ErrorMessage = cause.Error()
	}
	manager.mu.Unlock()

	terminalSnapshot, eventName, eventCause, persisted := manager.persistTerminalSnapshot(taskID, terminalSnapshot, cause)
	manager.mu.Lock()
	if manager.running == nil || manager.running.taskID != taskID {
		manager.mu.Unlock()
		return
	}
	manager.running = nil
	var startedSnapshot *Snapshot
	var nextTask *entry
	if len(manager.queue) > 0 {
		nextTask = manager.queue[0]
		manager.queue = manager.queue[1:]
		snapshot := manager.startTaskLocked(nextTask)
		startedSnapshot = &snapshot
	}
	manager.mu.Unlock()

	if persisted {
		if eventName == EventTaskFinished {
			manager.dispatch(Event{
				Name:       eventName,
				Task:       terminalSnapshot,
				OccurredAt: finishedAt,
			})
		} else {
			manager.dispatch(Event{
				Name:         eventName,
				Task:         terminalSnapshot,
				OccurredAt:   finishedAt,
				ErrorMessage: eventCause.Error(),
			})
		}
	}

	manager.startNextTask(startedSnapshot, nextTask)
}

func (manager *Manager) persistTerminalSnapshot(taskID string, terminalSnapshot Snapshot, cause error) (Snapshot, EventName, error, bool) {
	if cause != nil {
		if err := manager.recordTaskFailed(terminalSnapshot, cause); err != nil {
			if manager.logger != nil {
				manager.logger.Printf("record task failed [%s]: %v", taskID, err)
			}
			return Snapshot{}, "", nil, false
		}

		return terminalSnapshot, EventTaskFailed, cause, true
	}

	if err := manager.recordTaskFinished(terminalSnapshot); err == nil {
		return terminalSnapshot, EventTaskFinished, nil, true
	} else {
		if manager.logger != nil {
			manager.logger.Printf("record task finished [%s]: %v", taskID, err)
		}

		fallbackErr := fmt.Errorf("persist finished state: %w", err)
		terminalSnapshot.State = StateFailed
		terminalSnapshot.ErrorMessage = fallbackErr.Error()
		if fallbackRecordErr := manager.recordTaskFailed(terminalSnapshot, fallbackErr); fallbackRecordErr != nil {
			if manager.logger != nil {
				manager.logger.Printf("record fallback failed state [%s]: %v", taskID, fallbackRecordErr)
			}
			return Snapshot{}, "", nil, false
		}

		return terminalSnapshot, EventTaskFailed, fallbackErr, true
	}
}

func (manager *Manager) setProgress(taskID string, summary string, meta map[string]any) error {
	manager.mu.Lock()
	if manager.running == nil || manager.running.taskID != taskID {
		manager.mu.Unlock()
		return nil
	}

	manager.running.progress = &Progress{
		Summary:   summary,
		Meta:      cloneMeta(meta),
		UpdatedAt: manager.now(),
	}
	progressSnapshot := manager.running.snapshot()
	manager.mu.Unlock()

	if err := manager.recordTaskProgress(progressSnapshot); err != nil {
		return err
	}

	manager.dispatch(Event{
		Name:       EventTaskProgress,
		Task:       progressSnapshot,
		OccurredAt: progressSnapshot.Progress.UpdatedAt,
	})

	return nil
}

func (manager *Manager) startNextTask(startedSnapshot *Snapshot, nextTask *entry) {
	if startedSnapshot == nil || nextTask == nil {
		return
	}

	if err := manager.recordTaskStarted(*startedSnapshot); err != nil {
		if manager.logger != nil {
			manager.logger.Printf("record task start [%s]: %v", nextTask.taskID, err)
		}
		manager.failToStartTask(nextTask.taskID, err)
		return
	}

	manager.dispatch(Event{
		Name:       EventTaskStarted,
		Task:       *startedSnapshot,
		OccurredAt: startedSnapshot.StartedAtValue(),
	})
	manager.launchTask(nextTask)
}

func (manager *Manager) failToStartTask(taskID string, cause error) {
	manager.mu.Lock()
	if manager.running == nil || manager.running.taskID != taskID {
		manager.mu.Unlock()
		return
	}

	failedSnapshot := manager.running.snapshot()
	finishedAt := manager.now()
	failedSnapshot.State = StateFailed
	failedSnapshot.FinishedAt = &finishedAt
	failedSnapshot.ErrorMessage = cause.Error()
	manager.running = nil

	var startedSnapshot *Snapshot
	var nextTask *entry
	if len(manager.queue) > 0 {
		nextTask = manager.queue[0]
		manager.queue = manager.queue[1:]
		snapshot := manager.startTaskLocked(nextTask)
		startedSnapshot = &snapshot
	}
	manager.mu.Unlock()

	if manager.logger != nil {
		manager.logger.Printf("task start failed [%s]: %v", taskID, cause)
	}

	manager.dispatch(Event{
		Name:         EventTaskFailed,
		Task:         failedSnapshot,
		OccurredAt:   finishedAt,
		ErrorMessage: cause.Error(),
	})

	manager.startNextTask(startedSnapshot, nextTask)
}

func (manager *Manager) recordTaskStarted(snapshot Snapshot) error {
	manager.mu.Lock()
	recorder := manager.recorder
	manager.mu.Unlock()

	if recorder == nil {
		return nil
	}

	return recorder.RecordStarted(snapshot)
}

func (manager *Manager) recordTaskProgress(snapshot Snapshot) error {
	manager.mu.Lock()
	recorder := manager.recorder
	manager.mu.Unlock()

	if recorder == nil {
		return nil
	}

	return recorder.RecordProgress(snapshot)
}

func (manager *Manager) recordTaskFinished(snapshot Snapshot) error {
	manager.mu.Lock()
	recorder := manager.recorder
	manager.mu.Unlock()

	if recorder == nil {
		return nil
	}

	return recorder.RecordFinished(snapshot)
}

func (manager *Manager) recordTaskFailed(snapshot Snapshot, cause error) error {
	manager.mu.Lock()
	recorder := manager.recorder
	manager.mu.Unlock()

	if recorder == nil {
		return nil
	}

	return recorder.RecordFailed(snapshot, cause)
}

func (manager *Manager) runtimeLocked() RuntimeSnapshot {
	runtime := RuntimeSnapshot{
		Queued: make([]Snapshot, 0, len(manager.queue)),
	}

	if manager.running != nil {
		runningSnapshot := manager.running.snapshot()
		runtime.Running = &runningSnapshot
	}

	for _, queuedTask := range manager.queue {
		runtime.Queued = append(runtime.Queued, queuedTask.snapshot())
	}

	return runtime
}

func (manager *Manager) dispatch(event Event) {
	if manager.bus != nil {
		manager.bus.Publish(TaskChannel, taskEventEnvelope(event))
	}

	manager.mu.Lock()
	observers := make([]Observer, 0, len(manager.observers))
	for _, observer := range manager.observers {
		observers = append(observers, observer)
	}
	manager.mu.Unlock()

	for _, observer := range observers {
		observer(event)
	}
}

func (taskEntry *entry) snapshot() Snapshot {
	return Snapshot{
		TaskID:       taskEntry.taskID,
		ParentTaskID: taskEntry.parentTaskID,
		JobID:        taskEntry.jobID,
		JobType:      taskEntry.jobType,
		Name:         taskEntry.name,
		Args:         cloneMeta(taskEntry.args),
		State:        taskEntry.state,
		CreatedAt:    taskEntry.createdAt,
		StartedAt:    cloneTimePointer(taskEntry.startedAt),
		FinishedAt:   cloneTimePointer(taskEntry.finishedAt),
		ErrorMessage: taskEntry.errorMessage,
		Progress:     cloneProgress(taskEntry.progress),
	}
}

func (snapshot Snapshot) StartedAtValue() time.Time {
	if snapshot.StartedAt == nil {
		return snapshot.CreatedAt
	}

	return *snapshot.StartedAt
}

func (snapshot Snapshot) FinishedAtValue() time.Time {
	if snapshot.FinishedAt == nil {
		return snapshot.StartedAtValue()
	}

	return *snapshot.FinishedAt
}

func cloneProgress(progress *Progress) *Progress {
	if progress == nil {
		return nil
	}

	return &Progress{
		Summary:   progress.Summary,
		Meta:      cloneMeta(progress.Meta),
		UpdatedAt: progress.UpdatedAt,
	}
}

func cloneMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}

	cloned := make(map[string]any, len(meta))
	for key, value := range meta {
		cloned[key] = value
	}

	return cloned
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	cloned := *value
	return &cloned
}

func BuildSourceSyncJobID(sourceID string) string {
	return fmt.Sprintf("%s::%s", JobTypeSyncSource, sourceID)
}

func taskEventEnvelope(event Event) map[string]any {
	envelope := map[string]any{
		"event_name":     string(event.Name),
		"channel":        TaskChannel,
		"occurred_at":    event.OccurredAt.Format(time.RFC3339Nano),
		"task_id":        event.Task.TaskID,
		"parent_task_id": event.Task.ParentTaskID,
		"job_id":         event.Task.JobID,
		"job_type":       event.Task.JobType,
		"data": map[string]any{
			"task": taskSnapshotValue(event.Task),
		},
	}
	if event.ErrorMessage != "" {
		envelope["error_message"] = event.ErrorMessage
	}

	return envelope
}

func taskSnapshotValue(snapshot Snapshot) map[string]any {
	value := map[string]any{
		"task_id":        snapshot.TaskID,
		"parent_task_id": snapshot.ParentTaskID,
		"job_id":         snapshot.JobID,
		"job_type":       snapshot.JobType,
		"name":           snapshot.Name,
		"args":           cloneMeta(snapshot.Args),
		"state":          string(snapshot.State),
		"created_at":     snapshot.CreatedAt.Format(time.RFC3339Nano),
	}
	if snapshot.StartedAt != nil {
		value["started_at"] = snapshot.StartedAt.Format(time.RFC3339Nano)
	}
	if snapshot.FinishedAt != nil {
		value["finished_at"] = snapshot.FinishedAt.Format(time.RFC3339Nano)
	}
	if snapshot.ErrorMessage != "" {
		value["error_message"] = snapshot.ErrorMessage
	}
	if snapshot.Progress != nil {
		progress := map[string]any{
			"summary":    snapshot.Progress.Summary,
			"updated_at": snapshot.Progress.UpdatedAt.Format(time.RFC3339Nano),
		}
		if snapshot.Progress.Meta != nil {
			progress["meta"] = snapshot.Progress.Meta
		}
		value["progress"] = progress
	}

	return value
}
