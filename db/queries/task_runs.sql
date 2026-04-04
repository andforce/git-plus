-- name: CreateTaskRun :exec
INSERT INTO task_runs (
  task_id,
  parent_task_id,
  job_id,
  job_type,
  name,
  args_json,
  status,
  created_at,
  started_at,
  finished_at,
  error_message,
  last_progress_summary,
  last_progress_meta_json,
  updated_at
) VALUES (
  ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14
);

-- name: UpdateTaskRunProgress :exec
UPDATE task_runs
SET
  last_progress_summary = ?2,
  last_progress_meta_json = ?3,
  updated_at = ?4
WHERE task_id = ?1;

-- name: FinishTaskRun :exec
UPDATE task_runs
SET
  status = 'finished',
  finished_at = ?2,
  error_message = NULL,
  updated_at = ?3
WHERE task_id = ?1;

-- name: FailTaskRun :exec
UPDATE task_runs
SET
  status = 'failed',
  finished_at = ?2,
  error_message = ?3,
  updated_at = ?4
WHERE task_id = ?1;

-- name: CreateTaskRunLog :exec
INSERT INTO task_run_logs (
  task_id,
  event_type,
  summary,
  meta_json,
  error_message,
  created_at
) VALUES (
  ?1, ?2, ?3, ?4, ?5, ?6
);

-- name: GetTaskRun :one
SELECT
  task_id,
  parent_task_id,
  job_id,
  job_type,
  name,
  args_json,
  status,
  created_at,
  started_at,
  finished_at,
  error_message,
  last_progress_summary,
  last_progress_meta_json,
  updated_at
FROM task_runs
WHERE task_id = ?1
LIMIT 1;

-- name: CountTaskRuns :one
SELECT COUNT(1)
FROM task_runs
WHERE (?1 IS NULL OR job_type = ?1)
  AND (?2 IS NULL OR parent_task_id = ?2);

-- name: ListTaskRunsPaginated :many
SELECT
  task_id,
  parent_task_id,
  job_id,
  job_type,
  name,
  args_json,
  status,
  created_at,
  started_at,
  finished_at,
  error_message,
  last_progress_summary,
  last_progress_meta_json,
  updated_at
FROM task_runs
WHERE (?1 IS NULL OR job_type = ?1)
  AND (?2 IS NULL OR parent_task_id = ?2)
ORDER BY started_at DESC, task_id DESC
LIMIT ?3 OFFSET ?4;

-- name: ListTaskRunLogs :many
SELECT
  id,
  task_id,
  event_type,
  summary,
  meta_json,
  error_message,
  created_at
FROM task_run_logs
WHERE task_id = ?1
ORDER BY created_at, id;
