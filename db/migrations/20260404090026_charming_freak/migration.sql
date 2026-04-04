CREATE TABLE `app_meta` (
	`key` text PRIMARY KEY,
	`value` text NOT NULL
);
--> statement-breakpoint
CREATE TABLE `repos` (
	`id` integer PRIMARY KEY AUTOINCREMENT,
	`source_id` text NOT NULL,
	`platform` text NOT NULL,
	`ref_id` text NOT NULL,
	`status` text DEFAULT 'active' NOT NULL,
	`name` text NOT NULL,
	`full_name` text NOT NULL,
	`owner` text NOT NULL,
	`description` text,
	`html_url` text,
	`clone_url` text,
	`ssh_url` text,
	`default_branch` text,
	`visibility` text,
	`is_private` integer DEFAULT 0 NOT NULL,
	`is_fork` integer DEFAULT 0 NOT NULL,
	`is_archived` integer DEFAULT 0 NOT NULL,
	`origin` text DEFAULT '{}' NOT NULL,
	`meta` text NOT NULL,
	`last_seen_at` text NOT NULL,
	`disabled_at` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT "repos_status_check" CHECK("status" IN ('active', 'auto_excluded')),
	CONSTRAINT "repos_origin_json_check" CHECK(json_valid("origin")),
	CONSTRAINT "repos_meta_json_check" CHECK(json_valid("meta"))
);
--> statement-breakpoint
CREATE TABLE `task_run_logs` (
	`id` integer PRIMARY KEY AUTOINCREMENT,
	`task_id` text NOT NULL,
	`event_type` text NOT NULL,
	`summary` text,
	`meta_json` text,
	`error_message` text,
	`created_at` text NOT NULL,
	CONSTRAINT `fk_task_run_logs_task_id_task_runs_task_id_fk` FOREIGN KEY (`task_id`) REFERENCES `task_runs`(`task_id`) ON DELETE CASCADE
);
--> statement-breakpoint
CREATE TABLE `task_runs` (
	`task_id` text PRIMARY KEY,
	`parent_task_id` text,
	`job_id` text NOT NULL,
	`job_type` text NOT NULL,
	`name` text NOT NULL,
	`args_json` text,
	`status` text NOT NULL,
	`created_at` text NOT NULL,
	`started_at` text NOT NULL,
	`finished_at` text,
	`error_message` text,
	`last_progress_summary` text,
	`last_progress_meta_json` text,
	`updated_at` text NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `repos_source_ref_unique` ON `repos` (`source_id`,`ref_id`);--> statement-breakpoint
CREATE INDEX `repos_source_status_idx` ON `repos` (`source_id`,`status`);--> statement-breakpoint
CREATE INDEX `repos_source_full_name_idx` ON `repos` (`source_id`,`full_name`);--> statement-breakpoint
CREATE INDEX `task_run_logs_task_id_created_at_idx` ON `task_run_logs` (`task_id`,`created_at`,`id`);--> statement-breakpoint
CREATE INDEX `task_runs_started_at_idx` ON `task_runs` (`started_at`);--> statement-breakpoint
CREATE INDEX `task_runs_job_type_idx` ON `task_runs` (`job_type`);--> statement-breakpoint
CREATE INDEX `task_runs_parent_task_id_idx` ON `task_runs` (`parent_task_id`);