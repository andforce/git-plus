package taskservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	connectvalidate "connectrpc.com/validate"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
	taskv1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/task/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/task/v1/taskv1connect"
	"github.com/ImSingee/git-plus/pkg/task"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultStubStepDelay = 75 * time.Millisecond

type serviceServer struct {
	dataDir            string
	manager            *task.Manager
	stepDelay          time.Duration
	progressTick       time.Duration
	sourceSyncDuration func() time.Duration
}

type Option func(*serviceServer)

func NewHandler(dataDir string, manager *task.Manager, options ...Option) http.Handler {
	rpcMux := http.NewServeMux()
	RegisterHandlers(rpcMux, dataDir, manager, options...)
	return http.StripPrefix("/api", rpcMux)
}

func RegisterHandlers(mux *http.ServeMux, dataDir string, manager *task.Manager, options ...Option) {
	path, handler := taskv1connect.NewTaskServiceHandler(
		newServiceServer(dataDir, manager, options...),
		connect.WithInterceptors(mustValidateInterceptor()),
	)
	mux.Handle(path, handler)
}

func newServiceServer(dataDir string, manager *task.Manager, options ...Option) *serviceServer {
	server := &serviceServer{
		dataDir:            dataDir,
		manager:            manager,
		stepDelay:          defaultStubStepDelay,
		progressTick:       time.Second,
		sourceSyncDuration: randomSourceSyncDuration,
	}
	for _, option := range options {
		option(server)
	}

	return server
}

func WithSourceSyncDuration(fn func() time.Duration) Option {
	return func(server *serviceServer) {
		if fn != nil {
			server.sourceSyncDuration = fn
		}
	}
}

func WithProgressTick(interval time.Duration) Option {
	return func(server *serviceServer) {
		if interval > 0 {
			server.progressTick = interval
		}
	}
}

func (s *serviceServer) GetTaskRuntime(
	_ context.Context,
	_ *connect.Request[taskv1.GetTaskRuntimeRequest],
) (*connect.Response[taskv1.GetTaskRuntimeResponse], error) {
	runtime := s.manager.Runtime()
	queuedTasks := make([]*taskv1.Task, 0, len(runtime.Queued))
	for _, queuedTask := range runtime.Queued {
		queuedTasks = append(queuedTasks, toProtoTask(queuedTask))
	}

	var runningTask *taskv1.Task
	if runtime.Running != nil {
		runningTask = toProtoTask(*runtime.Running)
	}

	return connect.NewResponse(&taskv1.GetTaskRuntimeResponse{
		RunningTask: runningTask,
		QueuedTasks: queuedTasks,
	}), nil
}

func (s *serviceServer) EnqueueFullSync(
	_ context.Context,
	_ *connect.Request[taskv1.EnqueueFullSyncRequest],
) (*connect.Response[taskv1.EnqueueFullSyncResponse], error) {
	result, snapshot, err := s.manager.Enqueue(task.Spec{
		JobID:   task.JobIDSyncAll,
		JobType: task.JobTypeSyncAll,
		Name:    "Sync all sources",
		Args:    nil,
		Run:     s.syncAllRunner(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("enqueue full sync: %w", err))
	}

	return connect.NewResponse(&taskv1.EnqueueFullSyncResponse{
		Result: enqueueResultPtr(toProtoEnqueueResult(result)),
		Task:   toProtoTask(snapshot),
	}), nil
}

func (s *serviceServer) EnqueueSourceSync(
	_ context.Context,
	req *connect.Request[taskv1.EnqueueSourceSyncRequest],
) (*connect.Response[taskv1.EnqueueSourceSyncResponse], error) {
	sourceID := strings.TrimSpace(req.Msg.GetSourceId())
	if err := s.ensureSourceExists(sourceID); err != nil {
		return nil, err
	}

	result, snapshot, err := s.manager.Enqueue(s.sourceSyncSpec(sourceID, ""))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("enqueue source sync: %w", err))
	}

	return connect.NewResponse(&taskv1.EnqueueSourceSyncResponse{
		Result: enqueueResultPtr(toProtoEnqueueResult(result)),
		Task:   toProtoTask(snapshot),
	}), nil
}

func NewSyncAllRun(dataDir string, manager *task.Manager, logger *log.Logger) func(*task.ExecutionContext) error {
	server := newServiceServer(dataDir, manager)

	return server.syncAllRunnerWithLogger(logger)
}

func (s *serviceServer) CancelQueuedTask(
	_ context.Context,
	req *connect.Request[taskv1.CancelQueuedTaskRequest],
) (*connect.Response[taskv1.CancelQueuedTaskResponse], error) {
	taskID := strings.TrimSpace(req.Msg.GetTaskId())

	snapshot, err := s.manager.CancelQueuedTask(taskID)
	switch {
	case errors.Is(err, task.ErrTaskNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task %q was not found", taskID))
	case errors.Is(err, task.ErrTaskNotQueued):
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("task %q is not queued", taskID))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cancel queued task: %w", err))
	default:
		return connect.NewResponse(&taskv1.CancelQueuedTaskResponse{
			Task: toProtoTask(snapshot),
		}), nil
	}
}

func (s *serviceServer) EnqueueTestTask(
	_ context.Context,
	req *connect.Request[taskv1.EnqueueTestTaskRequest],
) (*connect.Response[taskv1.EnqueueTestTaskResponse], error) {
	variant := int(req.Msg.GetVariant())
	jobID := fmt.Sprintf("test-%d", variant)
	duration := time.Duration(variant*2) * time.Second

	result, snapshot, err := s.manager.Enqueue(task.Spec{
		JobID:   jobID,
		JobType: "test",
		Name:    fmt.Sprintf("Test task %d (%ds)", variant, variant*2),
		Args: map[string]any{
			"variant": variant,
		},
		Run: testRunner(variant, duration),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("enqueue test task: %w", err))
	}

	return connect.NewResponse(&taskv1.EnqueueTestTaskResponse{
		Result: enqueueResultPtr(toProtoEnqueueResult(result)),
		Task:   toProtoTask(snapshot),
	}), nil
}

func (s *serviceServer) ensureSourceExists(sourceID string) error {
	loaded, _, err := appconfig.LoadOrDefault(appconfig.PathForDataDir(s.dataDir))
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("load config: %w", err))
	}

	for _, source := range loaded.Data.Sources {
		if source.ID == sourceID {
			return nil
		}
	}

	return connect.NewError(connect.CodeNotFound, fmt.Errorf("source %q was not found", sourceID))
}

func (s *serviceServer) sourceSyncSpec(sourceID string, parentTaskID string) task.Spec {
	return task.Spec{
		ParentTaskID: parentTaskID,
		JobID:        task.BuildSourceSyncJobID(sourceID),
		JobType:      task.JobTypeSyncSource,
		Name:         fmt.Sprintf("Sync source %s", sourceID),
		Args: map[string]any{
			"source_id": sourceID,
		},
		Run: s.sourceSyncRunner(sourceID),
	}
}

func (s *serviceServer) syncAllRunner() func(*task.ExecutionContext) error {
	return s.syncAllRunnerWithLogger(log.Default())
}

func (s *serviceServer) syncAllRunnerWithLogger(logger *log.Logger) func(*task.ExecutionContext) error {
	if logger == nil {
		logger = log.Default()
	}

	return func(ctx *task.ExecutionContext) error {
		if err := ctx.SetProgress("Loading sources", map[string]any{
			"job_type": task.JobTypeSyncAll,
			"phase":    "load_sources",
		}); err != nil {
			return err
		}

		loaded, _, err := appconfig.LoadOrDefault(appconfig.PathForDataDir(s.dataDir))
		if err != nil {
			if progressErr := ctx.SetProgress("Failed to load sources", map[string]any{
				"job_type": task.JobTypeSyncAll,
				"phase":    "load_sources",
				"error":    err.Error(),
			}); progressErr != nil {
				return progressErr
			}
			logger.Printf("sync-all load config failed: %v", err)
			return nil
		}

		total := len(loaded.Data.Sources)
		if total == 0 {
			if err := ctx.SetProgress("No source configured", map[string]any{
				"job_type": task.JobTypeSyncAll,
				"phase":    "enqueue_sources",
				"total":    0,
			}); err != nil {
				return err
			}
			return nil
		}

		startedCount := 0
		queuedCount := 0
		dedupedCount := 0
		failedCount := 0

		for index, source := range loaded.Data.Sources {
			if err := ctx.SetProgress(fmt.Sprintf("Queueing source %s (%d/%d)", source.ID, index+1, total), map[string]any{
				"job_type":  task.JobTypeSyncAll,
				"phase":     "enqueue_sources",
				"source_id": source.ID,
				"index":     index + 1,
				"total":     total,
			}); err != nil {
				return err
			}

			result, _, enqueueErr := s.manager.Enqueue(s.sourceSyncSpec(source.ID, ctx.TaskID()))
			if enqueueErr != nil {
				failedCount++
				logger.Printf("sync-all enqueue source %q failed: %v", source.ID, enqueueErr)
				continue
			}

			switch result {
			case task.EnqueueResultStarted:
				startedCount++
			case task.EnqueueResultQueued:
				queuedCount++
			case task.EnqueueResultDeduped:
				dedupedCount++
			}
		}

		if err := ctx.SetProgress("Queued source sync tasks", map[string]any{
			"job_type": task.JobTypeSyncAll,
			"phase":    "done",
			"total":    total,
			"started":  startedCount,
			"queued":   queuedCount,
			"deduped":  dedupedCount,
			"failed":   failedCount,
		}); err != nil {
			return err
		}

		return nil
	}
}

func (s *serviceServer) sourceSyncRunner(sourceID string) func(*task.ExecutionContext) error {
	return func(ctx *task.ExecutionContext) error {
		duration := s.sourceSyncDuration()
		totalSeconds := int(duration / time.Second)
		if totalSeconds < 1 {
			totalSeconds = 1
		}

		for i := 1; i <= totalSeconds; i++ {
			if err := ctx.SetProgress(
				fmt.Sprintf("Processing (%d/%ds)", i, totalSeconds),
				map[string]any{
					"job_type":  task.JobTypeSyncSource,
					"source_id": sourceID,
					"step":      i,
					"total":     totalSeconds,
				},
			); err != nil {
				return err
			}
			time.Sleep(s.progressTick)
		}

		return nil
	}
}

func randomSourceSyncDuration() time.Duration {
	return time.Duration(rand.IntN(9)+2) * time.Second
}

func testRunner(variant int, duration time.Duration) func(*task.ExecutionContext) error {
	return func(ctx *task.ExecutionContext) error {
		totalSeconds := int(duration.Seconds())
		for i := 1; i <= totalSeconds; i++ {
			if err := ctx.SetProgress(
				fmt.Sprintf("Processing (%d/%ds)", i, totalSeconds),
				map[string]any{
					"variant": variant,
					"step":    i,
					"total":   totalSeconds,
				},
			); err != nil {
				return err
			}
			time.Sleep(time.Second)
		}

		return nil
	}
}

func toProtoTask(snapshot task.Snapshot) *taskv1.Task {
	protoTask := &taskv1.Task{
		TaskId:       stringPtr(snapshot.TaskID),
		ParentTaskId: stringPtr(snapshot.ParentTaskID),
		JobId:        stringPtr(snapshot.JobID),
		JobType:      stringPtr(snapshot.JobType),
		Name:         stringPtr(snapshot.Name),
		State:        taskStatePtr(toProtoTaskState(snapshot.State)),
		CreatedAt:    timestamppb.New(snapshot.CreatedAt),
	}
	if args := toProtoStruct(snapshot.Args); args != nil {
		protoTask.Args = args
	}
	if snapshot.StartedAt != nil {
		protoTask.StartedAt = timestamppb.New(*snapshot.StartedAt)
	}
	if snapshot.Progress != nil {
		protoTask.Progress = toProtoProgress(*snapshot.Progress)
	}

	return protoTask
}

func toProtoProgress(progress task.Progress) *taskv1.TaskProgress {
	return &taskv1.TaskProgress{
		Summary:   stringPtr(progress.Summary),
		Meta:      toProtoStruct(progress.Meta),
		UpdatedAt: timestamppb.New(progress.UpdatedAt),
	}
}

func toProtoStruct(meta map[string]any) *structpb.Struct {
	if len(meta) == 0 {
		return nil
	}

	value, err := structpb.NewStruct(meta)
	if err != nil {
		log.Printf("task metadata conversion failed: %v", err)
		return nil
	}

	return value
}

func toProtoTaskState(state task.State) taskv1.TaskState {
	switch state {
	case task.StateQueued:
		return taskv1.TaskState_TASK_STATE_QUEUED
	case task.StateRunning:
		return taskv1.TaskState_TASK_STATE_RUNNING
	default:
		return taskv1.TaskState_TASK_STATE_UNSPECIFIED
	}
}

func toProtoEnqueueResult(result task.EnqueueResult) taskv1.TaskEnqueueResult {
	switch result {
	case task.EnqueueResultStarted:
		return taskv1.TaskEnqueueResult_TASK_ENQUEUE_RESULT_STARTED
	case task.EnqueueResultQueued:
		return taskv1.TaskEnqueueResult_TASK_ENQUEUE_RESULT_QUEUED
	case task.EnqueueResultDeduped:
		return taskv1.TaskEnqueueResult_TASK_ENQUEUE_RESULT_DEDUPED
	default:
		return taskv1.TaskEnqueueResult_TASK_ENQUEUE_RESULT_UNSPECIFIED
	}
}

func mustValidateInterceptor() connect.Interceptor {
	interceptor, err := connectvalidate.NewInterceptor()
	if err != nil {
		panic(fmt.Sprintf("create connect validate interceptor: %v", err))
	}

	return interceptor
}

func stringPtr(value string) *string {
	return &value
}

func taskStatePtr(value taskv1.TaskState) *taskv1.TaskState {
	return &value
}

func enqueueResultPtr(value taskv1.TaskEnqueueResult) *taskv1.TaskEnqueueResult {
	return &value
}
