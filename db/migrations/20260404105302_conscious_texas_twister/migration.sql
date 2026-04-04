CREATE TABLE `repo_ref_changes` (
	`id` integer PRIMARY KEY AUTOINCREMENT,
	`repo_id` integer NOT NULL,
	`task_run_id` text NOT NULL,
	`ref_name` text NOT NULL,
	`ref_kind` text NOT NULL,
	`action` text NOT NULL,
	`old_hash` text,
	`new_hash` text,
	`archive_ref_name` text,
	`created_at` text NOT NULL,
	CONSTRAINT `fk_repo_ref_changes_repo_id_repos_id_fk` FOREIGN KEY (`repo_id`) REFERENCES `repos`(`id`) ON DELETE CASCADE,
	CONSTRAINT `fk_repo_ref_changes_task_run_id_task_runs_task_id_fk` FOREIGN KEY (`task_run_id`) REFERENCES `task_runs`(`task_id`) ON DELETE CASCADE,
	CONSTRAINT "repo_ref_changes_kind_check" CHECK("ref_kind" IN ('head', 'tag')),
	CONSTRAINT "repo_ref_changes_action_check" CHECK("action" IN ('create', 'update', 'delete'))
);
--> statement-breakpoint
CREATE TABLE `repo_refs_current` (
	`id` integer PRIMARY KEY AUTOINCREMENT,
	`repo_id` integer NOT NULL,
	`ref_name` text NOT NULL,
	`ref_kind` text NOT NULL,
	`current_hash` text NOT NULL,
	`status` text NOT NULL,
	`archive_ref_name` text,
	`first_seen_at` text NOT NULL,
	`last_seen_at` text NOT NULL,
	`deleted_at` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT `fk_repo_refs_current_repo_id_repos_id_fk` FOREIGN KEY (`repo_id`) REFERENCES `repos`(`id`) ON DELETE CASCADE,
	CONSTRAINT "repo_refs_current_kind_check" CHECK("ref_kind" IN ('head', 'tag')),
	CONSTRAINT "repo_refs_current_status_check" CHECK("status" IN ('active', 'deleted'))
);
--> statement-breakpoint
CREATE UNIQUE INDEX `repo_ref_changes_repo_run_ref_unique` ON `repo_ref_changes` (`repo_id`,`task_run_id`,`ref_name`);--> statement-breakpoint
CREATE INDEX `repo_ref_changes_repo_created_at_idx` ON `repo_ref_changes` (`repo_id`,`created_at`);--> statement-breakpoint
CREATE INDEX `repo_ref_changes_task_run_id_idx` ON `repo_ref_changes` (`task_run_id`);--> statement-breakpoint
CREATE INDEX `repo_ref_changes_repo_ref_created_at_idx` ON `repo_ref_changes` (`repo_id`,`ref_name`,`created_at`);--> statement-breakpoint
CREATE UNIQUE INDEX `repo_refs_current_repo_ref_unique` ON `repo_refs_current` (`repo_id`,`ref_name`);--> statement-breakpoint
CREATE INDEX `repo_refs_current_repo_status_idx` ON `repo_refs_current` (`repo_id`,`status`);--> statement-breakpoint
CREATE INDEX `repo_refs_current_repo_kind_idx` ON `repo_refs_current` (`repo_id`,`ref_kind`);