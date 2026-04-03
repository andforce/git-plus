package taskservice

import (
	"context"
	"errors"
	"fmt"
	"log"
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

const (
	defaultStubStepDelay = 75 * time.Millisecond
)

type serviceServer struct {
	dataDir   string
	manager   *task.Manager
	stepDelay time.Duration
}

func NewHandler(dataDir string, manager *task.Manager) http.Handler {
	rpcMux := http.NewServeMux()
	RegisterHandlers(rpcMux, dataDir, manager)
	return http.StripPrefix("/api", rpcMux)
}

func RegisterHandlers(mux *http.ServeMux, dataDir string, manager *task.Manager) {
	path, handler := taskv1connect.NewTaskServiceHandler(
		&serviceServer{
			dataDir:   dataDir,
			manager:   manager,
			stepDelay: defaultStubStepDelay,
		},
		connect.WithInterceptors(mustValidateInterceptor()),
	)
	mux.Handle(path, handler)
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
		Run:     s.stubRunner(task.JobTypeSyncAll, ""),
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

	result, snapshot, err := s.manager.Enqueue(task.Spec{
		JobID:   task.BuildSourceSyncJobID(sourceID),
		JobType: task.JobTypeSyncSource,
		Name:    fmt.Sprintf("Sync source %s", sourceID),
		Run:     s.stubRunner(task.JobTypeSyncSource, sourceID),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("enqueue source sync: %w", err))
	}

	return connect.NewResponse(&taskv1.EnqueueSourceSyncResponse{
		Result: enqueueResultPtr(toProtoEnqueueResult(result)),
		Task:   toProtoTask(snapshot),
	}), nil
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

func (s *serviceServer) stubRunner(jobType string, sourceID string) func(*task.ExecutionContext) {
	return func(ctx *task.ExecutionContext) {
		steps := []struct {
			summary string
			meta    map[string]any
		}{
			{
				summary: "Preparing task",
				meta: map[string]any{
					"job_type": jobType,
					"phase":    "prepare",
				},
			},
			{
				summary: "Running task",
				meta: map[string]any{
					"job_type": jobType,
					"phase":    "sync",
				},
			},
			{
				summary: "Finishing task",
				meta: map[string]any{
					"job_type": jobType,
					"phase":    "finalize",
				},
			},
		}
		if sourceID != "" {
			for _, step := range steps {
				step.meta["source_id"] = sourceID
			}
		}

		for _, step := range steps {
			ctx.SetProgress(step.summary, step.meta)
			time.Sleep(s.stepDelay)
		}
	}
}

func toProtoTask(snapshot task.Snapshot) *taskv1.Task {
	protoTask := &taskv1.Task{
		TaskId:    stringPtr(snapshot.TaskID),
		JobId:     stringPtr(snapshot.JobID),
		JobType:   stringPtr(snapshot.JobType),
		Name:      stringPtr(snapshot.Name),
		State:     taskStatePtr(toProtoTaskState(snapshot.State)),
		CreatedAt: timestamppb.New(snapshot.CreatedAt),
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
	meta, err := structpb.NewStruct(progress.Meta)
	if err != nil {
		log.Printf("task progress metadata conversion failed: %v", err)
		meta = nil
	}

	return &taskv1.TaskProgress{
		Summary:   stringPtr(progress.Summary),
		Meta:      meta,
		UpdatedAt: timestamppb.New(progress.UpdatedAt),
	}
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
