package task

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	JobTypeSyncAll    = "sync-all"
	JobTypeSyncSource = "sync-source"
	JobIDSyncAll      = "sync-all"
)

type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
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

type Option func(*Manager)

type Manager struct {
	mu        sync.Mutex
	running   *entry
	queue     []*entry
	closed    bool
	now       func() time.Time
	idGen     func() string
	logger    *log.Logger
	startHook func(Snapshot)
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
		now:   time.Now,
		idGen: uuid.NewString,
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

func WithStartHook(hook func(Snapshot)) Option {
	return func(manager *Manager) {
		manager.startHook = hook
	}
}

func (manager *Manager) Enqueue(spec Spec) (EnqueueResult, Snapshot, error) {
	if err := validateSpec(spec); err != nil {
		return 0, Snapshot{}, err
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.closed {
		return 0, Snapshot{}, ErrManagerClosed
	}

	for _, queuedTask := range manager.queue {
		if queuedTask.jobID == spec.JobID {
			return EnqueueResultDeduped, queuedTask.snapshot(), nil
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

	if manager.running == nil {
		manager.startTaskLocked(taskEntry)
		return EnqueueResultStarted, taskEntry.snapshot(), nil
	}

	manager.queue = append(manager.queue, taskEntry)
	return EnqueueResultQueued, taskEntry.snapshot(), nil
}

func (manager *Manager) Runtime() RuntimeSnapshot {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	return manager.runtimeLocked()
}

func (manager *Manager) CancelQueuedTask(taskID string) (Snapshot, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.running != nil && manager.running.taskID == taskID {
		return Snapshot{}, ErrTaskNotQueued
	}

	for index, queuedTask := range manager.queue {
		if queuedTask.taskID != taskID {
			continue
		}

		manager.queue = append(manager.queue[:index], manager.queue[index+1:]...)
		return queuedTask.snapshot(), nil
	}

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

func (manager *Manager) startTaskLocked(taskEntry *entry) {
	startedAt := manager.now()
	taskEntry.state = StateRunning
	taskEntry.startedAt = &startedAt
	manager.running = taskEntry

	snapshot := taskEntry.snapshot()
	if manager.startHook != nil {
		manager.startHook(snapshot)
	}

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
	defer manager.mu.Unlock()

	if manager.running == nil || manager.running.taskID != taskID {
		return
	}

	manager.running = nil
	if len(manager.queue) == 0 {
		return
	}

	nextTask := manager.queue[0]
	manager.queue = manager.queue[1:]
	manager.startTaskLocked(nextTask)
}

func (manager *Manager) setProgress(taskID string, summary string, meta map[string]any) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.running == nil || manager.running.taskID != taskID {
		return
	}

	manager.running.progress = &Progress{
		Summary:   summary,
		Meta:      cloneMeta(meta),
		UpdatedAt: manager.now(),
	}
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
