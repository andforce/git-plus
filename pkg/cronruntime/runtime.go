package cronruntime

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	appconfig "github.com/ImSingee/git-plus/pkg/config"
	"github.com/ImSingee/git-plus/pkg/task"
	"github.com/go-co-op/gocron/v2"
)

const DefaultNextRunsLimit = 5

var ErrInvalidCronConfig = errors.New("invalid cron config")

type Snapshot struct {
	Enabled   bool
	Cron      string
	NextRuns  []time.Time
	LastError string
}

type Option func(*Runtime)

type Runtime struct {
	mu         sync.Mutex
	path       string
	manager    *task.Manager
	scheduler  gocron.Scheduler
	job        gocron.Job
	logger     *log.Logger
	syncAllRun func(*task.ExecutionContext)
	cron       string
	lastError  string
}

func New(path string, manager *task.Manager, options ...Option) (*Runtime, error) {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}

	runtime := &Runtime{
		path:       path,
		manager:    manager,
		scheduler:  scheduler,
		syncAllRun: defaultSyncAllRun,
	}
	for _, option := range options {
		option(runtime)
	}

	runtime.scheduler.Start()

	return runtime, nil
}

func WithLogger(logger *log.Logger) Option {
	return func(runtime *Runtime) {
		runtime.logger = logger
	}
}

func WithSyncAllRun(run func(*task.ExecutionContext)) Option {
	return func(runtime *Runtime) {
		if run != nil {
			runtime.syncAllRun = run
		}
	}
}

func (runtime *Runtime) SetTaskManager(manager *task.Manager) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	runtime.manager = manager
}

func (runtime *Runtime) LoadFromFileAndApply() error {
	loaded, _, err := appconfig.LoadOrDefault(runtime.path)
	if err != nil {
		return runtime.disableWithError(fmt.Errorf("%w: %v", ErrInvalidCronConfig, err))
	}

	cronExpr := strings.TrimSpace(loaded.Data.Cron)
	if err := appconfig.ValidateCronExpression(cronExpr); err != nil {
		return runtime.disableWithError(fmt.Errorf("%w: cron must be a valid 5-field cron expression", ErrInvalidCronConfig))
	}

	if err := runtime.ApplyCron(cronExpr); err != nil {
		return err
	}

	return nil
}

func (runtime *Runtime) ApplyCron(expr string) error {
	trimmedExpr := strings.TrimSpace(expr)
	if err := appconfig.ValidateCronExpression(trimmedExpr); err != nil {
		return fmt.Errorf("validate cron: %w", err)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	return runtime.applyCronLocked(trimmedExpr)
}

func (runtime *Runtime) Snapshot(limit int) (Snapshot, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	snapshot := Snapshot{
		Enabled:   runtime.job != nil && runtime.cron != "",
		Cron:      runtime.cron,
		LastError: runtime.lastError,
	}
	if limit <= 0 || runtime.job == nil {
		return snapshot, nil
	}

	nextRuns, err := runtime.job.NextRuns(limit)
	if err != nil {
		return Snapshot{}, fmt.Errorf("get next runs: %w", err)
	}
	snapshot.NextRuns = append(snapshot.NextRuns, nextRuns...)

	return snapshot, nil
}

func (runtime *Runtime) Close() error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	return runtime.scheduler.Shutdown()
}

func (runtime *Runtime) applyCronLocked(expr string) error {
	if expr == "" {
		if err := runtime.removeJobLocked(); err != nil {
			runtime.lastError = err.Error()

			return fmt.Errorf("disable cron: %w", err)
		}
		runtime.cron = ""
		runtime.lastError = ""

		return nil
	}

	if runtime.job != nil && runtime.cron == expr {
		runtime.lastError = ""

		return nil
	}

	if err := runtime.removeJobLocked(); err != nil {
		runtime.lastError = err.Error()

		return fmt.Errorf("replace cron: %w", err)
	}

	job, err := runtime.scheduler.NewJob(
		gocron.CronJob(expr, false),
		gocron.NewTask(runtime.enqueueSyncAll),
		gocron.WithName("sync-all cron"),
	)
	if err != nil {
		runtime.cron = ""
		runtime.lastError = err.Error()

		return fmt.Errorf("create cron job: %w", err)
	}

	runtime.job = job
	runtime.cron = expr
	runtime.lastError = ""

	return nil
}

func (runtime *Runtime) removeJobLocked() error {
	if runtime.job == nil {
		return nil
	}

	jobID := runtime.job.ID()
	if err := runtime.scheduler.RemoveJob(jobID); err != nil {
		return err
	}

	runtime.job = nil

	return nil
}

func (runtime *Runtime) disableWithError(err error) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()

	if removeErr := runtime.removeJobLocked(); removeErr != nil {
		runtime.lastError = removeErr.Error()

		return fmt.Errorf("disable cron after reload error: %w", removeErr)
	}

	runtime.cron = ""
	runtime.lastError = err.Error()

	return err
}

func (runtime *Runtime) enqueueSyncAll() {
	runtime.mu.Lock()
	manager := runtime.manager
	run := runtime.syncAllRun
	logger := runtime.logger
	runtime.mu.Unlock()

	if manager == nil {
		if logger != nil {
			logger.Print("cron enqueue skipped: task manager is not configured")
		}
		return
	}

	_, _, err := manager.Enqueue(task.Spec{
		JobID:   task.JobIDSyncAll,
		JobType: task.JobTypeSyncAll,
		Name:    "Sync all sources",
		Run:     run,
	})
	if err != nil && logger != nil {
		logger.Printf("cron enqueue failed: %v", err)
	}
}

func defaultSyncAllRun(ctx *task.ExecutionContext) {
	steps := []struct {
		summary string
		phase   string
	}{
		{summary: "Preparing task", phase: "prepare"},
		{summary: "Running task", phase: "sync"},
		{summary: "Finishing task", phase: "finalize"},
	}

	for _, step := range steps {
		ctx.SetProgress(step.summary, map[string]any{
			"job_type": task.JobTypeSyncAll,
			"phase":    step.phase,
			"trigger":  "cron",
		})
		time.Sleep(75 * time.Millisecond)
	}
}
