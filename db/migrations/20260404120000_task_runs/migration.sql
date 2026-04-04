CREATE TABLE `task_runs` (
	`task_id` text PRIMARY KEY,
	`parent_task_id` text,
	`job_id` text NOT NULL,
	`job_type` text NOT NULL,
	`name` text NOT NULL,
	`status` text NOT NULL,
	`created_at` text NOT NULL,
	`started_at` text NOT NULL,
	`finished_at` text,
	`error_message` text,
	`last_progress_summary` text,
	`last_progress_meta_json` text,
	`updated_at` text NOT NULL
);

CREATE INDEX `task_runs_started_at_idx` ON `task_runs` (`started_at` DESC);
CREATE INDEX `task_runs_job_type_idx` ON `task_runs` (`job_type`);
CREATE INDEX `task_runs_parent_task_id_idx` ON `task_runs` (`parent_task_id`);

CREATE TABLE `task_run_logs` (
	`id` integer PRIMARY KEY AUTOINCREMENT,
	`task_id` text NOT NULL,
	`event_type` text NOT NULL,
	`summary` text,
	`meta_json` text,
	`error_message` text,
	`created_at` text NOT NULL,
	FOREIGN KEY (`task_id`) REFERENCES `task_runs` (`task_id`) ON DELETE CASCADE
);

CREATE INDEX `task_run_logs_task_id_created_at_idx`
	ON `task_run_logs` (`task_id`, `created_at`, `id`);
