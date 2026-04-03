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
)

type EventName string

const (
	EventTaskEnqueued EventName = "task.enqueued"
	EventTaskStarted  EventName = "task.started"
	EventTaskProgress EventName = "task.progress"
	EventTaskCanceled EventName = "task.canceled"
	EventTaskFinished EventName = "task.finished"
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
	TaskID    string
	JobID     string
	JobType   string
	Name      string
	State     State
	CreatedAt time.Time
	StartedAt *time.Time
	Progress  *Progress
}

type Event struct {
	Name       EventName
	Task       Snapshot
	OccurredAt time.Time
}

type RuntimeSnapshot struct {
	Running *Snapshot
	Queued  []Snapshot
}

type Spec struct {
	JobID   string
	JobType string
	Name    string
	Run     func(*ExecutionContext)
}

type ExecutionContext struct {
	manager *Manager
	taskID  string
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
	nextObserverID uint64
	observers      map[uint64]Observer
}

type entry struct {
	taskID    string
	jobID     string
	jobType   string
	name      string
	state     State
	createdAt time.Time
	startedAt *time.Time
	progress  *Progress
	run       func(*ExecutionContext)
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

func (manager *Manager) SetEventBus(bus *eventbus.Bus) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	manager.bus = bus
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
		taskID:    manager.idGen(),
		jobID:     spec.JobID,
		jobType:   spec.JobType,
		name:      spec.Name,
		state:     StateQueued,
		createdAt: createdAt,
		run:       spec.Run,
	}
	enqueuedSnapshot := taskEntry.snapshot()

	if manager.running == nil {
		startedSnapshot := manager.startTaskLocked(taskEntry)
		manager.mu.Unlock()
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

func (ctx *ExecutionContext) SetProgress(summary string, meta map[string]any) {
	if ctx == nil || ctx.manager == nil {
		return
	}

	ctx.manager.setProgress(ctx.taskID, summary, meta)
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
	defer func() {
		if recovered := recover(); recovered != nil && manager.logger != nil {
			manager.logger.Printf("task panic [%s]: %v", taskEntry.taskID, recovered)
		}
		manager.finishTask(taskEntry.taskID)
	}()

	taskEntry.run(&ExecutionContext{
		manager: manager,
		taskID:  taskEntry.taskID,
	})
}

func (manager *Manager) finishTask(taskID string) {
	manager.mu.Lock()
	if manager.running == nil || manager.running.taskID != taskID {
		manager.mu.Unlock()
		return
	}

	finishedSnapshot := manager.running.snapshot()
	finishedSnapshot.State = StateFinished
	finishedAt := manager.now()
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

	manager.dispatch(Event{
		Name:       EventTaskFinished,
		Task:       finishedSnapshot,
		OccurredAt: finishedAt,
	})
	if startedSnapshot != nil {
		manager.dispatch(Event{
			Name:       EventTaskStarted,
			Task:       *startedSnapshot,
			OccurredAt: startedSnapshot.StartedAtValue(),
		})
		manager.launchTask(nextTask)
	}
}

func (manager *Manager) setProgress(taskID string, summary string, meta map[string]any) {
	manager.mu.Lock()
	if manager.running == nil || manager.running.taskID != taskID {
		manager.mu.Unlock()
		return
	}

	manager.running.progress = &Progress{
		Summary:   summary,
		Meta:      cloneMeta(meta),
		UpdatedAt: manager.now(),
	}
	progressSnapshot := manager.running.snapshot()
	manager.mu.Unlock()

	manager.dispatch(Event{
		Name:       EventTaskProgress,
		Task:       progressSnapshot,
		OccurredAt: progressSnapshot.Progress.UpdatedAt,
	})
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
		TaskID:    taskEntry.taskID,
		JobID:     taskEntry.jobID,
		JobType:   taskEntry.jobType,
		Name:      taskEntry.name,
		State:     taskEntry.state,
		CreatedAt: taskEntry.createdAt,
		StartedAt: cloneTimePointer(taskEntry.startedAt),
		Progress:  cloneProgress(taskEntry.progress),
	}
}

func (snapshot Snapshot) StartedAtValue() time.Time {
	if snapshot.StartedAt == nil {
		return snapshot.CreatedAt
	}

	return *snapshot.StartedAt
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
	return map[string]any{
		"event_name":  string(event.Name),
		"channel":     TaskChannel,
		"occurred_at": event.OccurredAt.Format(time.RFC3339Nano),
		"task_id":     event.Task.TaskID,
		"job_id":      event.Task.JobID,
		"job_type":    event.Task.JobType,
		"data": map[string]any{
			"task": taskSnapshotValue(event.Task),
		},
	}
}

func taskSnapshotValue(snapshot Snapshot) map[string]any {
	value := map[string]any{
		"task_id":    snapshot.TaskID,
		"job_id":     snapshot.JobID,
		"job_type":   snapshot.JobType,
		"name":       snapshot.Name,
		"state":      string(snapshot.State),
		"created_at": snapshot.CreatedAt.Format(time.RFC3339Nano),
	}
	if snapshot.StartedAt != nil {
		value["started_at"] = snapshot.StartedAt.Format(time.RFC3339Nano)
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
