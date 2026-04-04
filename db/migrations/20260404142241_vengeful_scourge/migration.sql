ALTER TABLE `repo_ref_changes` ADD `new_commit_authored_at` text;--> statement-breakpoint
ALTER TABLE `repo_ref_changes` ADD `new_commit_committed_at` text;--> statement-breakpoint
ALTER TABLE `repo_ref_changes` ADD `new_commit_author_name` text;--> statement-breakpoint
ALTER TABLE `repo_ref_changes` ADD `new_commit_author_email` text;--> statement-breakpoint
ALTER TABLE `repo_ref_changes` ADD `new_commit_message` text;--> statement-breakpoint
ALTER TABLE `repo_refs_current` ADD `current_commit_authored_at` text;--> statement-breakpoint
ALTER TABLE `repo_refs_current` ADD `current_commit_committed_at` text;--> statement-breakpoint
ALTER TABLE `repo_refs_current` ADD `current_commit_author_name` text;--> statement-breakpoint
ALTER TABLE `repo_refs_current` ADD `current_commit_author_email` text;--> statement-breakpoint
ALTER TABLE `repo_refs_current` ADD `current_commit_message` text;