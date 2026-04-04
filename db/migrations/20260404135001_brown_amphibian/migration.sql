ALTER TABLE `repo_refs_current` RENAME TO `__old_repo_refs_current`;
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
	`last_hash_updated_at` text NOT NULL,
	`deleted_at` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT `fk_repo_refs_current_repo_id_repos_id_fk` FOREIGN KEY (`repo_id`) REFERENCES `repos`(`id`) ON DELETE CASCADE,
	CONSTRAINT "repo_refs_current_kind_check" CHECK("ref_kind" IN ('head', 'tag')),
	CONSTRAINT "repo_refs_current_status_check" CHECK("status" IN ('active', 'deleted'))
);
INSERT INTO `repo_refs_current` (
	`id`,
	`repo_id`,
	`ref_name`,
	`ref_kind`,
	`current_hash`,
	`status`,
	`archive_ref_name`,
	`first_seen_at`,
	`last_seen_at`,
	`last_hash_updated_at`,
	`deleted_at`,
	`created_at`,
	`updated_at`
)
SELECT
	`id`,
	`repo_id`,
	`ref_name`,
	`ref_kind`,
	`current_hash`,
	`status`,
	`archive_ref_name`,
	`first_seen_at`,
	`last_seen_at`,
	`updated_at`,
	`deleted_at`,
	`created_at`,
	`updated_at`
FROM `__old_repo_refs_current`;
DROP TABLE `__old_repo_refs_current`;
CREATE UNIQUE INDEX `repo_refs_current_repo_ref_unique` ON `repo_refs_current` (`repo_id`,`ref_name`);
CREATE INDEX `repo_refs_current_repo_status_idx` ON `repo_refs_current` (`repo_id`,`status`);
CREATE INDEX `repo_refs_current_repo_kind_idx` ON `repo_refs_current` (`repo_id`,`ref_kind`);
