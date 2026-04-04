-- name: ListReposForSource :many
SELECT
  id,
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE source_id = ?1
ORDER BY ref_id;

-- name: ListActiveReposForSource :many
SELECT
  id,
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE source_id = ?1
  AND status = 'active'
ORDER BY id;

-- name: CreateRepo :exec
INSERT INTO repos (
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
) VALUES (
  ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, ?20, ?21, ?22, ?23
);

-- name: UpdateRepo :exec
UPDATE repos
SET
  platform = ?3,
  status = ?4,
  name = ?5,
  full_name = ?6,
  owner = ?7,
  description = ?8,
  html_url = ?9,
  clone_url = ?10,
  ssh_url = ?11,
  default_branch = ?12,
  visibility = ?13,
  is_private = ?14,
  is_fork = ?15,
  is_archived = ?16,
  origin = ?17,
  meta = ?18,
  archive_repo_size_bytes = COALESCE(?19, archive_repo_size_bytes),
  last_seen_at = ?20,
  disabled_at = ?21,
  updated_at = ?22
WHERE source_id = ?1 AND ref_id = ?2;

-- name: UpdateRepoArchiveSize :exec
UPDATE repos
SET
  archive_repo_size_bytes = ?2,
  updated_at = ?3
WHERE id = ?1;

-- name: MarkRepoAutoExcluded :exec
UPDATE repos
SET
  status = ?3,
  disabled_at = CASE WHEN disabled_at IS NULL THEN ?4 ELSE disabled_at END,
  updated_at = ?5
WHERE source_id = ?1 AND ref_id = ?2 AND status <> ?3;

-- name: CountReposFiltered :one
SELECT COUNT(1)
FROM repos
WHERE (?1 IS NULL OR source_id = ?1)
  AND (?2 IS NULL OR (full_name LIKE '%' || ?2 || '%' OR description LIKE '%' || ?2 || '%'));

-- name: ListReposFilteredCreatedAtDesc :many
SELECT
  id,
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE (
    sqlc.narg(source_id) IS NULL
    OR repos.source_id = sqlc.narg(source_id)
  )
  AND (
    sqlc.narg(search) IS NULL
    OR (
      repos.full_name LIKE '%' || sqlc.narg(search) || '%'
      OR repos.description LIKE '%' || sqlc.narg(search) || '%'
    )
  )
ORDER BY repos.created_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: ListReposFilteredCreatedAtAsc :many
SELECT
  id,
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE (
    sqlc.narg(source_id) IS NULL
    OR repos.source_id = sqlc.narg(source_id)
  )
  AND (
    sqlc.narg(search) IS NULL
    OR (
      repos.full_name LIKE '%' || sqlc.narg(search) || '%'
      OR repos.description LIKE '%' || sqlc.narg(search) || '%'
    )
  )
ORDER BY repos.created_at ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: ListReposFilteredNameAsc :many
SELECT
  id,
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE (
    sqlc.narg(source_id) IS NULL
    OR repos.source_id = sqlc.narg(source_id)
  )
  AND (
    sqlc.narg(search) IS NULL
    OR (
      repos.full_name LIKE '%' || sqlc.narg(search) || '%'
      OR repos.description LIKE '%' || sqlc.narg(search) || '%'
    )
  )
ORDER BY repos.name ASC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: ListReposFilteredNameDesc :many
SELECT
  id,
  source_id,
  platform,
  ref_id,
  status,
  name,
  full_name,
  owner,
  description,
  html_url,
  clone_url,
  ssh_url,
  default_branch,
  visibility,
  is_private,
  is_fork,
  is_archived,
  origin,
  meta,
  archive_repo_size_bytes,
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE (
    sqlc.narg(source_id) IS NULL
    OR repos.source_id = sqlc.narg(source_id)
  )
  AND (
    sqlc.narg(search) IS NULL
    OR (
      repos.full_name LIKE '%' || sqlc.narg(search) || '%'
      OR repos.description LIKE '%' || sqlc.narg(search) || '%'
    )
  )
ORDER BY repos.name DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);

-- name: GetRepoById :one
SELECT
  id, source_id, platform, ref_id, status, name, full_name, owner,
  description, html_url, clone_url, ssh_url, default_branch, visibility,
  is_private, is_fork, is_archived, origin, meta, archive_repo_size_bytes, last_seen_at,
  disabled_at, created_at, updated_at
FROM repos
WHERE id = ?1;

-- name: ListRepoRefs :many
SELECT
  id, repo_id, ref_name, ref_kind, current_hash, status,
  archive_ref_name, first_seen_at, last_seen_at, last_hash_updated_at,
  current_commit_authored_at, current_commit_committed_at, current_commit_author_name,
  current_commit_author_email, current_commit_message, deleted_at,
  created_at, updated_at
FROM repo_refs_current
WHERE repo_id = ?1
  AND ref_kind = ?2
  AND (?3 = 1 OR status <> 'deleted')
ORDER BY ref_name;

-- name: CountRepoRefChanges :one
SELECT COUNT(1)
FROM repo_ref_changes
WHERE repo_id = ?1
  AND (?2 IS NULL OR ref_name = ?2);

-- name: ListRepoRefChangesFiltered :many
SELECT
  id, repo_id, task_run_id, ref_name, ref_kind, action,
  old_hash, new_hash, new_commit_authored_at, new_commit_committed_at,
  new_commit_author_name, new_commit_author_email, new_commit_message,
  archive_ref_name, created_at
FROM repo_ref_changes
WHERE repo_id = ?1
  AND (?2 IS NULL OR ref_name = ?2)
ORDER BY created_at DESC
LIMIT ?3 OFFSET ?4;
