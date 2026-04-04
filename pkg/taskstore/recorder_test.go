package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	"github.com/ImSingee/git-plus/pkg/task"
)

func TestRecorderPersistsTaskLifecycle(t *testing.T) {
	sqliteDB := openTestDB(t)
	recorder := NewRecorder(sqliteDB)
	queries := dbsqlc.New(sqliteDB)

	startedAt := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	progressAt := startedAt.Add(2 * time.Second)
	finishedAt := startedAt.Add(4 * time.Second)
	snapshot := task.Snapshot{
		TaskID:       "task-1",
		ParentTaskID: "parent-1",
		JobID:        task.JobIDSyncAll,
		JobType:      task.JobTypeSyncAll,
		Name:         "Sync all sources",
		Args: map[string]any{
			"mode": "full",
		},
		State:     task.StateRunning,
		CreatedAt: startedAt.Add(-time.Second),
		StartedAt: &startedAt,
	}

	if err := recorder.RecordStarted(snapshot); err != nil {
		t.Fatalf("record started: %v", err)
	}

	snapshot.Progress = &task.Progress{
		Summary:   "Running",
		Meta:      map[string]any{"step": float64(1)},
		UpdatedAt: progressAt,
	}
	if err := recorder.RecordProgress(snapshot); err != nil {
		t.Fatalf("record progress: %v", err)
	}

	snapshot.State = task.StateFinished
	snapshot.FinishedAt = &finishedAt
	if err := recorder.RecordFinished(snapshot); err != nil {
		t.Fatalf("record finished: %v", err)
	}

	taskRun, err := queries.GetTaskRun(context.Background(), snapshot.TaskID)
	if err != nil {
		t.Fatalf("get task run: %v", err)
	}
	if taskRun.Status != string(task.StateFinished) {
		t.Fatalf("unexpected task status: %q", taskRun.Status)
	}
	if !taskRun.ParentTaskID.Valid || taskRun.ParentTaskID.String != "parent-1" {
		t.Fatalf("unexpected parent_task_id: %#v", taskRun.ParentTaskID)
	}
	if !taskRun.ArgsJson.Valid || taskRun.ArgsJson.String != `{"mode":"full"}` {
		t.Fatalf("unexpected args_json: %#v", taskRun.ArgsJson)
	}
	if !taskRun.LastProgressSummary.Valid || taskRun.LastProgressSummary.String != "Running" {
		t.Fatalf("unexpected last progress summary: %#v", taskRun.LastProgressSummary)
	}
	if !taskRun.FinishedAt.Valid {
		t.Fatal("expected finished_at to be persisted")
	}

	logs, err := queries.ListTaskRunLogs(context.Background(), snapshot.TaskID)
	if err != nil {
		t.Fatalf("list task logs: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected three task logs, got %d", len(logs))
	}
	if logs[0].EventType != logEventStarted || logs[1].EventType != logEventProgress || logs[2].EventType != logEventFinished {
		t.Fatalf("unexpected log event order: %#v", logs)
	}
}

func TestRecorderPersistsTaskFailure(t *testing.T) {
	sqliteDB := openTestDB(t)
	recorder := NewRecorder(sqliteDB)
	queries := dbsqlc.New(sqliteDB)

	startedAt := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)
	snapshot := task.Snapshot{
		TaskID:  "task-2",
		JobID:   task.BuildSourceSyncJobID("octocat"),
		JobType: task.JobTypeSyncSource,
		Name:    "Sync source octocat",
		Args: map[string]any{
			"source_id": "octocat",
		},
		State:     task.StateRunning,
		CreatedAt: startedAt,
		StartedAt: &startedAt,
	}

	if err := recorder.RecordStarted(snapshot); err != nil {
		t.Fatalf("record started: %v", err)
	}

	snapshot.State = task.StateFailed
	snapshot.FinishedAt = &finishedAt
	snapshot.ErrorMessage = "boom"
	if err := recorder.RecordFailed(snapshot, errors.New("boom")); err != nil {
		t.Fatalf("record failed: %v", err)
	}

	taskRun, err := queries.GetTaskRun(context.Background(), snapshot.TaskID)
	if err != nil {
		t.Fatalf("get task run: %v", err)
	}
	if taskRun.Status != string(task.StateFailed) {
		t.Fatalf("unexpected task status: %q", taskRun.Status)
	}
	if !taskRun.ErrorMessage.Valid || taskRun.ErrorMessage.String != "boom" {
		t.Fatalf("unexpected error message: %#v", taskRun.ErrorMessage)
	}
	if !taskRun.ArgsJson.Valid || taskRun.ArgsJson.String != `{"source_id":"octocat"}` {
		t.Fatalf("unexpected args_json: %#v", taskRun.ArgsJson)
	}

	logs, err := queries.ListTaskRunLogs(context.Background(), snapshot.TaskID)
	if err != nil {
		t.Fatalf("list task logs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected two task logs, got %d", len(logs))
	}
	if logs[1].EventType != logEventFailed {
		t.Fatalf("unexpected failure log: %#v", logs[1])
	}
}

func TestRecorderRejectsNonJSONMetadata(t *testing.T) {
	sqliteDB := openTestDB(t)
	recorder := NewRecorder(sqliteDB)

	startedAt := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	snapshot := task.Snapshot{
		TaskID:    "task-3",
		JobID:     task.JobIDSyncAll,
		JobType:   task.JobTypeSyncAll,
		Name:      "Sync all sources",
		State:     task.StateRunning,
		CreatedAt: startedAt,
		StartedAt: &startedAt,
	}

	if err := recorder.RecordStarted(snapshot); err != nil {
		t.Fatalf("record started: %v", err)
	}

	snapshot.Progress = &task.Progress{
		Summary:   "Running",
		Meta:      map[string]any{"stream": make(chan int)},
		UpdatedAt: startedAt.Add(time.Second),
	}

	if err := recorder.RecordProgress(snapshot); err == nil {
		t.Fatal("expected non-JSON metadata to fail")
	}
}

func TestRecorderCommitsWhileAnotherConnectionHasOpenReadTransaction(t *testing.T) {
	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	writerDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open writer database: %v", err)
	}
	defer writerDB.Close()

	readerDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open reader database: %v", err)
	}
	defer readerDB.Close()

	recorder := NewRecorder(writerDB)

	startedAt := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)
	snapshot := task.Snapshot{
		TaskID:    "task-reader-lock",
		JobID:     task.BuildSourceSyncJobID("octocat"),
		JobType:   task.JobTypeSyncSource,
		Name:      "Sync source octocat",
		State:     task.StateRunning,
		CreatedAt: startedAt,
		StartedAt: &startedAt,
	}

	if err := recorder.RecordStarted(snapshot); err != nil {
		t.Fatalf("record started: %v", err)
	}

	readTx, err := readerDB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("begin read transaction: %v", err)
	}
	defer readTx.Rollback()

	var count int
	if err := readTx.QueryRowContext(context.Background(), "SELECT COUNT(1) FROM task_runs").Scan(&count); err != nil {
		t.Fatalf("query task runs in read transaction: %v", err)
	}

	snapshot.State = task.StateFailed
	snapshot.FinishedAt = &finishedAt
	snapshot.ErrorMessage = "boom"
	if err := recorder.RecordFailed(snapshot, errors.New("boom")); err != nil {
		t.Fatalf("record failed with concurrent reader: %v", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	return sqliteDB
}
