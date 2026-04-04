package task

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestEnqueueStartsTaskImmediatelyWhenIdle(t *testing.T) {
	manager := newTestManager()
	block := make(chan struct{})
	defer close(block)

	result, snapshot, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			<-block
			return nil
		},
	})
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	if result != EnqueueResultStarted {
		t.Fatalf("expected started result, got %v", result)
	}
	if snapshot.State != StateRunning {
		t.Fatalf("expected running state, got %q", snapshot.State)
	}

	runtime := manager.Runtime()
	if runtime.Running == nil {
		t.Fatal("expected running task")
	}
	if runtime.Running.TaskID != snapshot.TaskID {
		t.Fatalf("unexpected running task id: %q", runtime.Running.TaskID)
	}
	if len(runtime.Queued) != 0 {
		t.Fatalf("expected no queued tasks, got %#v", runtime.Queued)
	}
}

func TestEnqueueQueuesTaskWhenAnotherTaskIsRunning(t *testing.T) {
	manager := newTestManager()
	firstBlock := make(chan struct{})
	secondBlock := make(chan struct{})
	defer close(firstBlock)
	defer close(secondBlock)

	if _, _, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", firstBlock)); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}

	result, snapshot, err := manager.Enqueue(blockingSpec(BuildSourceSyncJobID("github"), JobTypeSyncSource, "Sync source github", secondBlock))
	if err != nil {
		t.Fatalf("enqueue second task: %v", err)
	}

	if result != EnqueueResultQueued {
		t.Fatalf("expected queued result, got %v", result)
	}
	if snapshot.State != StateQueued {
		t.Fatalf("expected queued state, got %q", snapshot.State)
	}

	runtime := manager.Runtime()
	if len(runtime.Queued) != 1 {
		t.Fatalf("expected one queued task, got %d", len(runtime.Queued))
	}
	if runtime.Queued[0].TaskID != snapshot.TaskID {
		t.Fatalf("unexpected queued task id: %q", runtime.Queued[0].TaskID)
	}
}

func TestEnqueueDedupesOnlyQueuedTasks(t *testing.T) {
	manager := newTestManager()
	runningBlock := make(chan struct{})
	queuedBlock := make(chan struct{})
	defer close(runningBlock)
	defer close(queuedBlock)

	if _, _, err := manager.Enqueue(blockingSpec(JobTypeSyncSource+"::github", JobTypeSyncSource, "Sync source github", runningBlock)); err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}

	firstQueuedResult, firstQueuedSnapshot, err := manager.Enqueue(blockingSpec(BuildSourceSyncJobID("octocat"), JobTypeSyncSource, "Sync source octocat", queuedBlock))
	if err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}
	if firstQueuedResult != EnqueueResultQueued {
		t.Fatalf("expected queued result, got %v", firstQueuedResult)
	}

	dedupedResult, dedupedSnapshot, err := manager.Enqueue(blockingSpec(BuildSourceSyncJobID("octocat"), JobTypeSyncSource, "Sync source octocat", queuedBlock))
	if err != nil {
		t.Fatalf("enqueue duplicate queued task: %v", err)
	}
	if dedupedResult != EnqueueResultDeduped {
		t.Fatalf("expected deduped result, got %v", dedupedResult)
	}
	if dedupedSnapshot.TaskID != firstQueuedSnapshot.TaskID {
		t.Fatalf("expected deduped snapshot to reference queued task %q, got %q", firstQueuedSnapshot.TaskID, dedupedSnapshot.TaskID)
	}

	allowedResult, allowedSnapshot, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", queuedBlock))
	if err != nil {
		t.Fatalf("enqueue distinct task: %v", err)
	}
	if allowedResult != EnqueueResultQueued {
		t.Fatalf("expected queued result, got %v", allowedResult)
	}
	if allowedSnapshot.TaskID == firstQueuedSnapshot.TaskID {
		t.Fatal("expected a new queued task")
	}
}

func TestEnqueueAllowsSameJobIDWhenCurrentlyRunning(t *testing.T) {
	manager := newTestManager()
	runningBlock := make(chan struct{})
	queuedBlock := make(chan struct{})
	defer close(runningBlock)
	defer close(queuedBlock)

	if _, _, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", runningBlock)); err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}

	result, snapshot, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", queuedBlock))
	if err != nil {
		t.Fatalf("enqueue same job id while running: %v", err)
	}

	if result != EnqueueResultQueued {
		t.Fatalf("expected queued result, got %v", result)
	}
	if snapshot.JobID != JobIDSyncAll {
		t.Fatalf("unexpected job id: %q", snapshot.JobID)
	}
}

func TestCancelQueuedTaskRemovesTaskFromQueue(t *testing.T) {
	manager := newTestManager()
	runningBlock := make(chan struct{})
	queuedBlock := make(chan struct{})
	defer close(runningBlock)
	defer close(queuedBlock)

	if _, _, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", runningBlock)); err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}
	_, queuedSnapshot, err := manager.Enqueue(blockingSpec(BuildSourceSyncJobID("github"), JobTypeSyncSource, "Sync source github", queuedBlock))
	if err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}

	canceledSnapshot, err := manager.CancelQueuedTask(queuedSnapshot.TaskID)
	if err != nil {
		t.Fatalf("cancel queued task: %v", err)
	}
	if canceledSnapshot.TaskID != queuedSnapshot.TaskID {
		t.Fatalf("unexpected canceled task id: %q", canceledSnapshot.TaskID)
	}

	runtime := manager.Runtime()
	if len(runtime.Queued) != 0 {
		t.Fatalf("expected empty queue, got %#v", runtime.Queued)
	}
}

func TestCancelQueuedTaskRejectsRunningTask(t *testing.T) {
	manager := newTestManager()
	block := make(chan struct{})
	defer close(block)

	_, runningSnapshot, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", block))
	if err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}

	_, err = manager.CancelQueuedTask(runningSnapshot.TaskID)
	if !errors.Is(err, ErrTaskNotQueued) {
		t.Fatalf("expected ErrTaskNotQueued, got %v", err)
	}
}

func TestSetProgressKeepsOnlyLastUpdate(t *testing.T) {
	manager := newTestManager()
	release := make(chan struct{})
	defer close(release)

	started := make(chan struct{})
	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(ctx *ExecutionContext) error {
			if err := ctx.SetProgress("phase 1", map[string]any{"step": float64(1)}); err != nil {
				return err
			}
			if err := ctx.SetProgress("phase 2", map[string]any{"step": float64(2)}); err != nil {
				return err
			}
			close(started)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	<-started
	runtime := manager.Runtime()
	if runtime.Running == nil || runtime.Running.Progress == nil {
		t.Fatalf("expected running task progress, got %#v", runtime.Running)
	}
	if runtime.Running.Progress.Summary != "phase 2" {
		t.Fatalf("unexpected progress summary: %q", runtime.Running.Progress.Summary)
	}
	if runtime.Running.Progress.Meta["step"] != float64(2) {
		t.Fatalf("unexpected progress meta: %#v", runtime.Running.Progress.Meta)
	}
}

func TestQueuedTaskStartsAfterRunningTaskCompletes(t *testing.T) {
	manager := newTestManager()
	firstDone := make(chan struct{})
	secondStarted := make(chan struct{})

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			close(firstDone)
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}

	if _, _, err := manager.Enqueue(Spec{
		JobID:   BuildSourceSyncJobID("github"),
		JobType: JobTypeSyncSource,
		Name:    "Sync source github",
		Run: func(*ExecutionContext) error {
			close(secondStarted)
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue second task: %v", err)
	}

	waitFor(t, firstDone, "first task completion")
	waitFor(t, secondStarted, "second task start")
	waitUntil(t, func() bool {
		runtime := manager.Runtime()
		return runtime.Running == nil && len(runtime.Queued) == 0
	}, "queue to drain")
}

func TestBuildSourceSyncJobID(t *testing.T) {
	if got := BuildSourceSyncJobID("octocat"); got != "sync-source::octocat" {
		t.Fatalf("unexpected source sync job id: %q", got)
	}
}

func TestSnapshotsAreSafeCopies(t *testing.T) {
	manager := newTestManager()
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(ctx *ExecutionContext) error {
			if err := ctx.SetProgress("phase", map[string]any{"step": float64(1)}); err != nil {
				return err
			}
			close(started)
			<-release
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	<-started
	runtime := manager.Runtime()
	runtime.Running.Progress.Meta["step"] = float64(9)

	latestRuntime := manager.Runtime()
	if latestRuntime.Running.Progress.Meta["step"] != float64(1) {
		t.Fatalf("expected internal progress meta to stay unchanged, got %#v", latestRuntime.Running.Progress.Meta)
	}
}

func newTestManager() *Manager {
	counter := 0
	return NewManager(
		WithIDGenerator(func() string {
			counter++
			return fmt.Sprintf("task-%d", counter)
		}),
	)
}

func blockingSpec(jobID string, jobType string, name string, block <-chan struct{}) Spec {
	return Spec{
		JobID:   jobID,
		JobType: jobType,
		Name:    name,
		Run: func(*ExecutionContext) error {
			<-block
			return nil
		},
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitUntil(t *testing.T, condition func() bool, description string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", description)
}

func TestRuntimeQueuedOrderIsFIFO(t *testing.T) {
	manager := newTestManager()
	runningBlock := make(chan struct{})
	defer close(runningBlock)

	if _, _, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", runningBlock)); err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}

	jobIDs := []string{
		BuildSourceSyncJobID("a"),
		BuildSourceSyncJobID("b"),
		BuildSourceSyncJobID("c"),
	}
	for _, jobID := range jobIDs {
		if _, _, err := manager.Enqueue(blockingSpec(jobID, JobTypeSyncSource, jobID, runningBlock)); err != nil {
			t.Fatalf("enqueue queued task %q: %v", jobID, err)
		}
	}

	runtime := manager.Runtime()
	got := make([]string, 0, len(runtime.Queued))
	for _, queued := range runtime.Queued {
		got = append(got, queued.JobID)
	}
	if !slices.Equal(got, jobIDs) {
		t.Fatalf("unexpected queued order: got=%v want=%v", got, jobIDs)
	}
}

func TestManagerEmitsTaskLifecycleEvents(t *testing.T) {
	release := make(chan struct{})
	events := make(chan Event, 8)
	manager := NewManager(
		WithIDGenerator(func() string { return "task-1" }),
		WithObserver(func(event Event) {
			events <- event
		}),
	)

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(ctx *ExecutionContext) error {
			if err := ctx.SetProgress("Running", map[string]any{"phase": "sync"}); err != nil {
				return err
			}
			<-release
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	assertEventName(t, receiveTaskEvent(t, events), EventTaskEnqueued)
	assertEventName(t, receiveTaskEvent(t, events), EventTaskStarted)
	assertEventName(t, receiveTaskEvent(t, events), EventTaskProgress)
	close(release)
	finishedEvent := receiveTaskEvent(t, events)
	assertEventName(t, finishedEvent, EventTaskFinished)
	if finishedEvent.Task.State != StateFinished {
		t.Fatalf("expected finished event state %q, got %q", StateFinished, finishedEvent.Task.State)
	}
}

func TestManagerEmitsCanceledEvent(t *testing.T) {
	events := make(chan Event, 4)
	counter := 0
	manager := NewManager(
		WithIDGenerator(func() string {
			counter++
			return fmt.Sprintf("task-%d", counter)
		}),
		WithObserver(func(event Event) {
			events <- event
		}),
	)
	runningBlock := make(chan struct{})
	defer close(runningBlock)

	if _, _, err := manager.Enqueue(blockingSpec(JobIDSyncAll, JobTypeSyncAll, "Sync all sources", runningBlock)); err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}
	_, queuedSnapshot, err := manager.Enqueue(blockingSpec(BuildSourceSyncJobID("github"), JobTypeSyncSource, "Sync source github", runningBlock))
	if err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}

	if _, err := manager.CancelQueuedTask(queuedSnapshot.TaskID); err != nil {
		t.Fatalf("cancel queued task: %v", err)
	}

	received := make([]EventName, 0, 3)
	for len(received) < 3 {
		received = append(received, receiveTaskEvent(t, events).Name)
	}
	if !slices.Equal(received, []EventName{EventTaskEnqueued, EventTaskStarted, EventTaskEnqueued}) {
		t.Fatalf("unexpected leading events: %v", received)
	}
	assertEventName(t, receiveTaskEvent(t, events), EventTaskCanceled)
}

func TestQueuedTasksDoNotHitRecorderUntilStarted(t *testing.T) {
	recorder := &stubRecorder{}
	manager := NewManager(
		WithRecorder(recorder),
		WithIDGenerator(func() string { return "task-1" }),
	)
	block := make(chan struct{})
	defer close(block)

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			<-block
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue running task: %v", err)
	}

	if _, _, err := manager.Enqueue(Spec{
		JobID:   BuildSourceSyncJobID("github"),
		JobType: JobTypeSyncSource,
		Name:    "Sync source github",
		Run: func(*ExecutionContext) error {
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}

	if len(recorder.started) != 1 {
		t.Fatalf("expected only running task to be recorded, got %d", len(recorder.started))
	}
}

func TestRecordStartedFailurePreventsTaskExecution(t *testing.T) {
	recorder := &stubRecorder{
		recordStartedErr: errors.New("db unavailable"),
	}
	executed := false
	manager := NewManager(WithRecorder(recorder))

	_, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			executed = true
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected enqueue to fail when record started fails")
	}
	if executed {
		t.Fatal("expected task run function not to execute")
	}
	if runtime := manager.Runtime(); runtime.Running != nil || len(runtime.Queued) != 0 {
		t.Fatalf("expected manager to stay idle, got %#v", runtime)
	}
}

func TestRecordProgressFailureFailsTask(t *testing.T) {
	events := make(chan Event, 8)
	recorder := &stubRecorder{
		recordProgressErr: errors.New("write progress failed"),
	}
	manager := NewManager(
		WithRecorder(recorder),
		WithObserver(func(event Event) {
			events <- event
		}),
	)

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(ctx *ExecutionContext) error {
			return ctx.SetProgress("phase 1", map[string]any{"step": float64(1)})
		},
	}); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	assertEventName(t, receiveTaskEvent(t, events), EventTaskEnqueued)
	assertEventName(t, receiveTaskEvent(t, events), EventTaskStarted)
	failedEvent := receiveTaskEvent(t, events)
	assertEventName(t, failedEvent, EventTaskFailed)
	if failedEvent.Task.State != StateFailed {
		t.Fatalf("expected failed state, got %q", failedEvent.Task.State)
	}
	if len(recorder.failed) != 1 {
		t.Fatalf("expected one failed record, got %d", len(recorder.failed))
	}
}

func TestFinishedRecorderFailureFallsBackToFailedTerminalState(t *testing.T) {
	events := make(chan Event, 8)
	recorder := &stubRecorder{
		recordFinishedErr: errors.New("finish write failed"),
	}
	manager := NewManager(
		WithRecorder(recorder),
		WithObserver(func(event Event) {
			events <- event
		}),
	)

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue task: %v", err)
	}

	assertEventName(t, receiveTaskEvent(t, events), EventTaskEnqueued)
	assertEventName(t, receiveTaskEvent(t, events), EventTaskStarted)
	failedEvent := receiveTaskEvent(t, events)
	assertEventName(t, failedEvent, EventTaskFailed)
	if failedEvent.Task.State != StateFailed {
		t.Fatalf("expected failed terminal state, got %q", failedEvent.Task.State)
	}
	if len(recorder.failed) != 1 {
		t.Fatalf("expected fallback failed record, got %d", len(recorder.failed))
	}
	waitUntil(t, func() bool {
		runtime := manager.Runtime()
		return runtime.Running == nil && len(runtime.Queued) == 0
	}, "manager to drain after fallback failure persist")
}

func TestTerminalRecorderFailureDoesNotPublishCompletionButStillAdvancesQueue(t *testing.T) {
	events := make(chan Event, 8)
	recorder := &stubRecorder{
		recordFinishedErr: errors.New("finish write failed"),
		recordFailedErr:   errors.New("failed write also failed"),
	}
	manager := NewManager(
		WithRecorder(recorder),
		WithObserver(func(event Event) {
			events <- event
		}),
	)

	firstDone := make(chan struct{})
	secondStarted := make(chan struct{})

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			close(firstDone)
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}
	if _, _, err := manager.Enqueue(Spec{
		JobID:   BuildSourceSyncJobID("github"),
		JobType: JobTypeSyncSource,
		Name:    "Sync source github",
		Run: func(*ExecutionContext) error {
			close(secondStarted)
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue second task: %v", err)
	}

	assertEventName(t, receiveTaskEvent(t, events), EventTaskEnqueued)
	assertEventName(t, receiveTaskEvent(t, events), EventTaskStarted)
	assertEventName(t, receiveTaskEvent(t, events), EventTaskEnqueued)
	waitFor(t, firstDone, "first task completion")
	time.Sleep(50 * time.Millisecond)

	select {
	case event := <-events:
		if event.Name != EventTaskStarted {
			t.Fatalf("expected only next task start event, got %q", event.Name)
		}
	default:
		t.Fatal("expected next queued task to start")
	}
	waitFor(t, secondStarted, "queued task start after terminal recorder failure")
	waitUntil(t, func() bool {
		runtime := manager.Runtime()
		return runtime.Running == nil && len(runtime.Queued) == 0
	}, "manager to drain after terminal recorder failure")
}

func TestStartFailureFallsThroughToNextQueuedTask(t *testing.T) {
	recorder := &stubRecorder{
		failStartedCalls: map[int]error{
			2: errors.New("second task start failed"),
		},
	}
	manager := NewManager(
		WithRecorder(recorder),
		WithIDGenerator(func() string {
			recorder.generated++
			return fmt.Sprintf("task-%d", recorder.generated)
		}),
	)
	runningBlock := make(chan struct{})
	secondStarted := make(chan struct{})
	thirdStarted := make(chan struct{})

	if _, _, err := manager.Enqueue(Spec{
		JobID:   JobIDSyncAll,
		JobType: JobTypeSyncAll,
		Name:    "Sync all sources",
		Run: func(*ExecutionContext) error {
			<-runningBlock
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}

	if _, _, err := manager.Enqueue(Spec{
		JobID:   BuildSourceSyncJobID("second"),
		JobType: JobTypeSyncSource,
		Name:    "Sync source second",
		Run: func(*ExecutionContext) error {
			close(secondStarted)
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue second task: %v", err)
	}

	if _, _, err := manager.Enqueue(Spec{
		JobID:   BuildSourceSyncJobID("third"),
		JobType: JobTypeSyncSource,
		Name:    "Sync source third",
		Run: func(*ExecutionContext) error {
			close(thirdStarted)
			return nil
		},
	}); err != nil {
		t.Fatalf("enqueue third task: %v", err)
	}

	close(runningBlock)
	waitFor(t, thirdStarted, "third task start after second fails to start")

	select {
	case <-secondStarted:
		t.Fatal("expected second task not to run")
	default:
	}

	waitUntil(t, func() bool {
		runtime := manager.Runtime()
		return runtime.Running == nil && len(runtime.Queued) == 0
	}, "queue to drain")
}

type stubRecorder struct {
	started           []Snapshot
	progressed        []Snapshot
	finished          []Snapshot
	failed            []Snapshot
	recordStartedErr  error
	recordProgressErr error
	recordFinishedErr error
	recordFailedErr   error
	failStartedCalls  map[int]error
	startCalls        int
	generated         int
}

func (recorder *stubRecorder) RecordStarted(snapshot Snapshot) error {
	recorder.startCalls++
	if recorder.failStartedCalls != nil {
		if err, ok := recorder.failStartedCalls[recorder.startCalls]; ok {
			return err
		}
	}
	if recorder.recordStartedErr != nil {
		return recorder.recordStartedErr
	}
	recorder.started = append(recorder.started, snapshot)
	return nil
}

func (recorder *stubRecorder) RecordProgress(snapshot Snapshot) error {
	if recorder.recordProgressErr != nil {
		return recorder.recordProgressErr
	}
	recorder.progressed = append(recorder.progressed, snapshot)
	return nil
}

func (recorder *stubRecorder) RecordFinished(snapshot Snapshot) error {
	if recorder.recordFinishedErr != nil {
		return recorder.recordFinishedErr
	}
	recorder.finished = append(recorder.finished, snapshot)
	return nil
}

func (recorder *stubRecorder) RecordFailed(snapshot Snapshot, _ error) error {
	if recorder.recordFailedErr != nil {
		return recorder.recordFailedErr
	}
	recorder.failed = append(recorder.failed, snapshot)
	return nil
}

func receiveTaskEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()

	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task event")
		return Event{}
	}
}

func assertEventName(t *testing.T, event Event, expected EventName) {
	t.Helper()

	if event.Name != expected {
		t.Fatalf("unexpected event name: got=%q want=%q", event.Name, expected)
	}
}
