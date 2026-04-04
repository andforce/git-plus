package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	"github.com/ImSingee/git-plus/pkg/task"
)

const (
	logEventStarted  = "started"
	logEventProgress = "progress"
	logEventFinished = "finished"
	logEventFailed   = "failed"
)

type Recorder struct {
	db      *sql.DB
	queries *dbsqlc.Queries
}

func NewRecorder(db *sql.DB) *Recorder {
	if db == nil {
		return nil
	}

	return &Recorder{
		db:      db,
		queries: dbsqlc.New(db),
	}
}

func (recorder *Recorder) RecordStarted(snapshot task.Snapshot) error {
	if recorder == nil {
		return nil
	}

	startedAt := snapshot.StartedAtValue()
	argsJSON, err := marshalMeta(snapshot.Args)
	if err != nil {
		return fmt.Errorf("marshal task args: %w", err)
	}

	tx, err := recorder.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin task start transaction: %w", err)
	}

	queries := recorder.queries.WithTx(tx)
	if err := queries.CreateTaskRun(context.Background(), dbsqlc.CreateTaskRunParams{
		TaskID:               snapshot.TaskID,
		ParentTaskID:         nullableString(snapshot.ParentTaskID),
		JobID:                snapshot.JobID,
		JobType:              snapshot.JobType,
		Name:                 snapshot.Name,
		ArgsJson:             nullableString(argsJSON),
		Status:               string(task.StateRunning),
		CreatedAt:            formatTime(snapshot.CreatedAt),
		StartedAt:            formatTime(startedAt),
		FinishedAt:           sql.NullString{},
		ErrorMessage:         sql.NullString{},
		LastProgressSummary:  sql.NullString{},
		LastProgressMetaJson: sql.NullString{},
		UpdatedAt:            formatTime(startedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("create task run: %w", err)
	}

	if err := queries.CreateTaskRunLog(context.Background(), dbsqlc.CreateTaskRunLogParams{
		TaskID:       snapshot.TaskID,
		EventType:    logEventStarted,
		Summary:      nullableString("Task started"),
		MetaJson:     sql.NullString{},
		ErrorMessage: sql.NullString{},
		CreatedAt:    formatTime(startedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("create task start log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task start transaction: %w", err)
	}

	return nil
}

func (recorder *Recorder) RecordProgress(snapshot task.Snapshot) error {
	if recorder == nil || snapshot.Progress == nil {
		return nil
	}

	metaJSON, err := marshalMeta(snapshot.Progress.Meta)
	if err != nil {
		return fmt.Errorf("marshal progress metadata: %w", err)
	}

	updatedAt := snapshot.Progress.UpdatedAt
	tx, err := recorder.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin task progress transaction: %w", err)
	}

	queries := recorder.queries.WithTx(tx)
	if err := queries.UpdateTaskRunProgress(context.Background(), dbsqlc.UpdateTaskRunProgressParams{
		TaskID:               snapshot.TaskID,
		LastProgressSummary:  nullableString(snapshot.Progress.Summary),
		LastProgressMetaJson: nullableString(metaJSON),
		UpdatedAt:            formatTime(updatedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update task run progress: %w", err)
	}

	if err := queries.CreateTaskRunLog(context.Background(), dbsqlc.CreateTaskRunLogParams{
		TaskID:       snapshot.TaskID,
		EventType:    logEventProgress,
		Summary:      nullableString(snapshot.Progress.Summary),
		MetaJson:     nullableString(metaJSON),
		ErrorMessage: sql.NullString{},
		CreatedAt:    formatTime(updatedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("create task progress log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task progress transaction: %w", err)
	}

	return nil
}

func (recorder *Recorder) RecordFinished(snapshot task.Snapshot) error {
	if recorder == nil {
		return nil
	}

	finishedAt := snapshot.FinishedAtValue()
	tx, err := recorder.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin task finish transaction: %w", err)
	}

	queries := recorder.queries.WithTx(tx)
	if err := queries.FinishTaskRun(context.Background(), dbsqlc.FinishTaskRunParams{
		TaskID:     snapshot.TaskID,
		FinishedAt: nullableString(formatTime(finishedAt)),
		UpdatedAt:  formatTime(finishedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("finish task run: %w", err)
	}

	if err := queries.CreateTaskRunLog(context.Background(), dbsqlc.CreateTaskRunLogParams{
		TaskID:       snapshot.TaskID,
		EventType:    logEventFinished,
		Summary:      nullableString("Task finished"),
		MetaJson:     sql.NullString{},
		ErrorMessage: sql.NullString{},
		CreatedAt:    formatTime(finishedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("create task finished log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task finish transaction: %w", err)
	}

	return nil
}

func (recorder *Recorder) RecordFailed(snapshot task.Snapshot, cause error) error {
	if recorder == nil {
		return nil
	}

	finishedAt := snapshot.FinishedAtValue()
	errorMessage := ""
	if cause != nil {
		errorMessage = cause.Error()
	}

	tx, err := recorder.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("begin task failure transaction: %w", err)
	}

	queries := recorder.queries.WithTx(tx)
	if err := queries.FailTaskRun(context.Background(), dbsqlc.FailTaskRunParams{
		TaskID:       snapshot.TaskID,
		FinishedAt:   nullableString(formatTime(finishedAt)),
		ErrorMessage: nullableString(errorMessage),
		UpdatedAt:    formatTime(finishedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("fail task run: %w", err)
	}

	if err := queries.CreateTaskRunLog(context.Background(), dbsqlc.CreateTaskRunLogParams{
		TaskID:       snapshot.TaskID,
		EventType:    logEventFailed,
		Summary:      nullableString("Task failed"),
		MetaJson:     sql.NullString{},
		ErrorMessage: nullableString(errorMessage),
		CreatedAt:    formatTime(finishedAt),
	}); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("create task failed log: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task failure transaction: %w", err)
	}

	return nil
}

func formatTime(value time.Time) string {
	return value.Format(time.RFC3339Nano)
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}

	return sql.NullString{
		String: value,
		Valid:  true,
	}
}

func marshalMeta(meta map[string]any) (string, error) {
	if len(meta) == 0 {
		return "", nil
	}

	encoded, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}

	return string(encoded), nil
}
