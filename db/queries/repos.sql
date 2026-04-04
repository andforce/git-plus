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
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
) VALUES (
  ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18, ?19, ?20, ?21, ?22
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
  last_seen_at = ?19,
  disabled_at = ?20,
  updated_at = ?21
WHERE source_id = ?1 AND ref_id = ?2;

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

-- name: ListReposFiltered :many
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
  last_seen_at,
  disabled_at,
  created_at,
  updated_at
FROM repos
WHERE (@source_id IS NULL OR source_id = @source_id)
  AND (@search IS NULL OR (full_name LIKE '%' || @search || '%' OR description LIKE '%' || @search || '%'))
  AND @sort IS NOT NULL
ORDER BY
  CASE WHEN @sort = 'created_at_desc' THEN created_at END DESC,
  CASE WHEN @sort = 'created_at_asc'  THEN created_at END ASC,
  CASE WHEN @sort = 'name_asc'   THEN name  END ASC,
  CASE WHEN @sort = 'name_desc'  THEN name  END DESC
LIMIT @limit OFFSET @offset;
