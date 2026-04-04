CREATE TABLE app_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE task_runs (
  task_id TEXT PRIMARY KEY,
  parent_task_id TEXT,
  job_id TEXT NOT NULL,
  job_type TEXT NOT NULL,
  name TEXT NOT NULL,
  args_json TEXT,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  error_message TEXT,
  last_progress_summary TEXT,
  last_progress_meta_json TEXT,
  updated_at TEXT NOT NULL
);

CREATE INDEX task_runs_started_at_idx ON task_runs (started_at DESC);
CREATE INDEX task_runs_job_type_idx ON task_runs (job_type);
CREATE INDEX task_runs_parent_task_id_idx ON task_runs (parent_task_id);

CREATE TABLE task_run_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  summary TEXT,
  meta_json TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  FOREIGN KEY (task_id) REFERENCES task_runs (task_id) ON DELETE CASCADE
);

CREATE INDEX task_run_logs_task_id_created_at_idx
  ON task_run_logs (task_id, created_at, id);
