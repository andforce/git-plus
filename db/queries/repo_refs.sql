-- name: ListRepoRefsCurrentByRepoID :many
SELECT
  id,
  repo_id,
  ref_name,
  ref_kind,
  current_hash,
  status,
  archive_ref_name,
  first_seen_at,
  last_seen_at,
  last_hash_updated_at,
  current_commit_authored_at,
  current_commit_committed_at,
  current_commit_author_name,
  current_commit_author_email,
  current_commit_message,
  deleted_at,
  created_at,
  updated_at
FROM repo_refs_current
WHERE repo_id = ?1
ORDER BY ref_name;

-- name: UpsertRepoRefCurrent :exec
INSERT INTO repo_refs_current (
  repo_id,
  ref_name,
  ref_kind,
  current_hash,
  status,
  archive_ref_name,
  first_seen_at,
  last_seen_at,
  last_hash_updated_at,
  current_commit_authored_at,
  current_commit_committed_at,
  current_commit_author_name,
  current_commit_author_email,
  current_commit_message,
  deleted_at,
  created_at,
  updated_at
) VALUES (
  ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17
)
ON CONFLICT(repo_id, ref_name) DO UPDATE SET
  ref_kind = excluded.ref_kind,
  current_hash = excluded.current_hash,
  status = excluded.status,
  archive_ref_name = excluded.archive_ref_name,
  last_seen_at = excluded.last_seen_at,
  last_hash_updated_at = CASE
    WHEN repo_refs_current.current_hash <> excluded.current_hash THEN excluded.last_hash_updated_at
    ELSE repo_refs_current.last_hash_updated_at
  END,
  current_commit_authored_at = excluded.current_commit_authored_at,
  current_commit_committed_at = excluded.current_commit_committed_at,
  current_commit_author_name = excluded.current_commit_author_name,
  current_commit_author_email = excluded.current_commit_author_email,
  current_commit_message = excluded.current_commit_message,
  deleted_at = excluded.deleted_at,
  updated_at = excluded.updated_at;

-- name: MarkRepoRefCurrentDeleted :exec
UPDATE repo_refs_current
SET
  status = 'deleted',
  last_seen_at = ?3,
  deleted_at = CASE WHEN deleted_at IS NULL THEN ?3 ELSE deleted_at END,
  updated_at = ?4
WHERE repo_id = ?1
  AND ref_name = ?2
  AND status <> 'deleted';

-- name: CreateRepoRefChange :exec
INSERT INTO repo_ref_changes (
  repo_id,
  task_run_id,
  ref_name,
  ref_kind,
  action,
  old_hash,
  new_hash,
  new_commit_authored_at,
  new_commit_committed_at,
  new_commit_author_name,
  new_commit_author_email,
  new_commit_message,
  archive_ref_name,
  created_at
) VALUES (
  ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14
);
