package syncsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	"github.com/ImSingee/git-plus/pkg/archivegit"
	git "github.com/go-git/go-git/v5"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

const (
	taskLogEventRepoSyncFailed = "repo_sync_failed"
	minRetryDelay              = 10 * time.Second
	maxRetryDelay              = 120 * time.Second
)

type repositoryArchiver interface {
	SyncRepository(ctx context.Context, request repositoryArchiveRequest) (repositoryArchiveResult, error)
}

type repositoryArchiveRequest struct {
	Path        string
	RemoteURL   string
	Username    string
	Token       string
	CurrentRefs []archivegit.CurrentRef
}

type repositoryArchiveResult struct {
	CurrentRefs           []archivegit.RemoteRef
	Changes               []archivegit.Change
	ArchiveContentChanged bool
}

type repositoryCommitInfoLoader interface {
	Load(
		ctx context.Context,
		repoPath string,
		currentRows []dbsqlc.RepoRefsCurrent,
		archiveResult repositoryArchiveResult,
	) (repoRefCommitMetadata, error)
}

type repoRefCommitMetadata struct {
	currentByRefName map[string]archivegit.CommitInfo
	newByRefName     map[string]archivegit.CommitInfo
}

type repoSyncOutcome struct {
	repo        dbsqlc.Repo
	path        string
	attempts    int
	retried     int
	changeCount int
	createdRefs int
	updatedRefs int
	deletedRefs int
	err         error
}

type goGitRepositoryArchiver struct{}

type goGitCommitInfoLoader struct{}

func (archiver *goGitRepositoryArchiver) SyncRepository(ctx context.Context, request repositoryArchiveRequest) (repositoryArchiveResult, error) {
	if strings.TrimSpace(request.Path) == "" {
		return repositoryArchiveResult{}, fmt.Errorf("archive path is required")
	}
	if strings.TrimSpace(request.RemoteURL) == "" {
		return repositoryArchiveResult{}, fmt.Errorf("remote URL is required")
	}

	repo, err := archivegit.OpenArchive(request.Path, request.RemoteURL)
	if err != nil {
		return repositoryArchiveResult{}, err
	}

	remoteRefs, err := archivegit.ListRemoteRefs(ctx, repo, gitAuthMethod(request.Username, request.Token))
	if err != nil {
		return repositoryArchiveResult{}, err
	}

	changes := archivegit.DiffRefs(request.CurrentRefs, remoteRefs)
	archiveContentChanged, err := archivegit.FetchArchiveRefs(ctx, repo, gitAuthMethod(request.Username, request.Token), changes)
	if err != nil {
		return repositoryArchiveResult{}, err
	}

	return repositoryArchiveResult{
		CurrentRefs:           remoteRefs,
		Changes:               changes,
		ArchiveContentChanged: archiveContentChanged,
	}, nil
}

func (loader *goGitCommitInfoLoader) Load(
	_ context.Context,
	repoPath string,
	currentRows []dbsqlc.RepoRefsCurrent,
	archiveResult repositoryArchiveResult,
) (repoRefCommitMetadata, error) {
	metadata := repoRefCommitMetadata{
		currentByRefName: make(map[string]archivegit.CommitInfo),
		newByRefName:     make(map[string]archivegit.CommitInfo),
	}

	if strings.TrimSpace(repoPath) == "" {
		return metadata, nil
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		if os.IsNotExist(err) || err == git.ErrRepositoryNotExists {
			return metadata, nil
		}
		return repoRefCommitMetadata{}, fmt.Errorf("open archive repository %q: %w", repoPath, err)
	}

	currentByName := make(map[string]dbsqlc.RepoRefsCurrent, len(currentRows))
	for _, row := range currentRows {
		currentByName[row.RefName] = row
	}

	changeByName := make(map[string]archivegit.Change, len(archiveResult.Changes))
	for _, change := range archiveResult.Changes {
		changeByName[change.RefName] = change
	}

	resolvedByHash := make(map[string]*archivegit.CommitInfo)
	resolveByHash := func(hash string) (*archivegit.CommitInfo, error) {
		trimmedHash := strings.TrimSpace(hash)
		if trimmedHash == "" {
			return nil, nil
		}
		if cached, ok := resolvedByHash[trimmedHash]; ok {
			return cached, nil
		}

		info, err := archivegit.ResolveCommitInfo(repo, trimmedHash)
		if err != nil {
			return nil, err
		}
		resolvedByHash[trimmedHash] = info
		return info, nil
	}

	for _, remoteRef := range archiveResult.CurrentRefs {
		change, hasChange := changeByName[remoteRef.Name]
		needsCurrentCommit := false
		if hasChange && (change.Action == archivegit.ChangeActionCreate || change.Action == archivegit.ChangeActionUpdate) {
			needsCurrentCommit = true
		} else if existingRow, ok := currentByName[remoteRef.Name]; !ok || !existingRow.CurrentCommitAuthoredAt.Valid {
			needsCurrentCommit = true
		}

		if !needsCurrentCommit {
			continue
		}

		info, err := resolveByHash(remoteRef.Hash)
		if err != nil {
			return repoRefCommitMetadata{}, fmt.Errorf("resolve commit info for ref %q: %w", remoteRef.Name, err)
		}
		if info == nil {
			continue
		}

		metadata.currentByRefName[remoteRef.Name] = *info
		if hasChange && (change.Action == archivegit.ChangeActionCreate || change.Action == archivegit.ChangeActionUpdate) {
			metadata.newByRefName[remoteRef.Name] = *info
		}
	}

	return metadata, nil
}

func (executor *Executor) syncActiveRepos(ctx context.Context, db *sql.DB, request SyncRequest, reporter ProgressReporter) (ArchiveResult, error) {
	queries := dbsqlc.New(db)
	activeRepos, err := queries.ListActiveReposForSource(ctx, request.Source.ID)
	if err != nil {
		return ArchiveResult{}, fmt.Errorf("list active repos for source %q: %w", request.Source.ID, err)
	}

	if err := reportProgress(reporter, fmt.Sprintf("Loaded %d active repositories", len(activeRepos)), map[string]any{
		"phase":        "load_active_repos",
		"target_total": len(activeRepos),
	}); err != nil {
		return ArchiveResult{}, err
	}

	if len(activeRepos) == 0 {
		return ArchiveResult{}, nil
	}

	workerCount := request.Concurrency
	if workerCount <= 0 {
		workerCount = 1
	}

	jobs := make(chan dbsqlc.Repo)
	results := make(chan repoSyncOutcome, len(activeRepos))

	var workers sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for repo := range jobs {
				outcome := executor.syncRepoWithRetry(ctx, db, request, repo)
				results <- outcome
			}
		}()
	}

	go func() {
		for _, repo := range activeRepos {
			select {
			case <-ctx.Done():
				close(jobs)
				workers.Wait()
				close(results)
				return
			case jobs <- repo:
			}
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	result := ArchiveResult{
		TargetTotal: len(activeRepos),
	}

	for outcome := range results {
		if ctx.Err() != nil {
			return ArchiveResult{}, ctx.Err()
		}

		result.Processed++
		result.Retried += outcome.retried
		if outcome.err != nil {
			result.Failed++
			if err := executor.recordRepoSyncFailure(ctx, db, request, outcome); err != nil {
				return ArchiveResult{}, err
			}
		} else {
			result.Succeeded++
			result.ChangeCount += outcome.changeCount
			result.CreatedRefCount += outcome.createdRefs
			result.UpdatedRefCount += outcome.updatedRefs
			result.DeletedRefCount += outcome.deletedRefs
		}

		summary := fmt.Sprintf("Syncing active repositories (%d/%d)", result.Processed, result.TargetTotal)
		if strings.TrimSpace(outcome.repo.FullName) != "" {
			summary = fmt.Sprintf("%s: %s", summary, outcome.repo.FullName)
		}

		if err := reportProgress(reporter, summary, map[string]any{
			"phase":             "sync_active_repos",
			"target_total":      result.TargetTotal,
			"processed":         result.Processed,
			"succeeded":         result.Succeeded,
			"failed":            result.Failed,
			"retried":           result.Retried,
			"current_repo_id":   outcome.repo.ID,
			"current_ref_id":    outcome.repo.RefID,
			"current_full_name": outcome.repo.FullName,
		}); err != nil {
			return ArchiveResult{}, err
		}
	}

	return result, nil
}

func (executor *Executor) syncRepoWithRetry(ctx context.Context, db *sql.DB, request SyncRequest, repo dbsqlc.Repo) repoSyncOutcome {
	maxRetryTimes := request.MaxRetryTimes
	if maxRetryTimes < 0 {
		maxRetryTimes = 0
	}

	totalAttempts := 1 + maxRetryTimes
	repoPath := filepath.Join(executor.dataDir, "repos", request.Source.ID, repo.RefID)

	var finalOutcome repoSyncOutcome
	for attempt := 1; attempt <= totalAttempts; attempt++ {
		outcome, err := executor.syncRepoOnce(ctx, db, request, repo, repoPath)
		if err == nil {
			outcome.attempts = attempt
			outcome.retried = attempt - 1
			return outcome
		}

		finalOutcome = repoSyncOutcome{
			repo:     repo,
			path:     repoPath,
			attempts: attempt,
			retried:  attempt - 1,
			err:      err,
		}

		if attempt == totalAttempts {
			return finalOutcome
		}

		if sleepErr := executor.sleep(ctx, retryDelay(attempt)); sleepErr != nil {
			finalOutcome.err = sleepErr
			return finalOutcome
		}
	}

	return finalOutcome
}

func (executor *Executor) syncRepoOnce(ctx context.Context, db *sql.DB, request SyncRequest, repo dbsqlc.Repo, repoPath string) (repoSyncOutcome, error) {
	queries := dbsqlc.New(db)
	currentRows, err := queries.ListRepoRefsCurrentByRepoID(ctx, repo.ID)
	if err != nil {
		return repoSyncOutcome{}, fmt.Errorf("list repo refs for repo %d: %w", repo.ID, err)
	}

	archiveResult, err := executor.repoArchiver.SyncRepository(ctx, repositoryArchiveRequest{
		Path:        repoPath,
		RemoteURL:   repo.CloneUrl.String,
		Username:    request.Source.Username,
		Token:       request.Source.Token,
		CurrentRefs: toArchiveCurrentRefs(currentRows),
	})
	if err != nil {
		return repoSyncOutcome{}, fmt.Errorf("archive repo %q: %w", repo.FullName, err)
	}

	now := executor.now().UTC().Format(time.RFC3339Nano)
	archiveRepoSizeBytes := sql.NullInt64{}
	if shouldRefreshArchiveRepoSize(repo.ArchiveRepoSizeBytes, archiveResult.ArchiveContentChanged) {
		sizeBytes, err := calculateDirectorySizeBytes(repoPath)
		if err != nil {
			return repoSyncOutcome{}, fmt.Errorf("calculate archive size for %q: %w", repo.FullName, err)
		}
		archiveRepoSizeBytes = sql.NullInt64{Int64: sizeBytes, Valid: true}
	}

	commitMetadata, err := executor.commitInfoLoader.Load(ctx, repoPath, currentRows, archiveResult)
	if err != nil {
		return repoSyncOutcome{}, fmt.Errorf("load commit info for %q: %w", repo.FullName, err)
	}

	if err := persistRepoRefState(ctx, db, repo.ID, request.RunID, currentRows, archiveResult, commitMetadata, archiveRepoSizeBytes, now); err != nil {
		return repoSyncOutcome{}, fmt.Errorf("persist repo ref state for %q: %w", repo.FullName, err)
	}

	outcome := repoSyncOutcome{
		repo: repo,
		path: repoPath,
	}
	for _, change := range archiveResult.Changes {
		switch change.Action {
		case archivegit.ChangeActionCreate:
			outcome.changeCount++
			outcome.createdRefs++
		case archivegit.ChangeActionUpdate:
			outcome.changeCount++
			outcome.updatedRefs++
		case archivegit.ChangeActionDelete:
			outcome.changeCount++
			outcome.deletedRefs++
		}
	}

	return outcome, nil
}

func persistRepoRefState(
	ctx context.Context,
	db *sql.DB,
	repoID int64,
	taskRunID string,
	currentRows []dbsqlc.RepoRefsCurrent,
	archiveResult repositoryArchiveResult,
	commitMetadata repoRefCommitMetadata,
	archiveRepoSizeBytes sql.NullInt64,
	now string,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin repo ref transaction: %w", err)
	}

	queries := dbsqlc.New(tx)
	currentByName := make(map[string]dbsqlc.RepoRefsCurrent, len(currentRows))
	for _, currentRow := range currentRows {
		currentByName[currentRow.RefName] = currentRow
	}

	changeByName := make(map[string]archivegit.Change, len(archiveResult.Changes))
	for _, change := range archiveResult.Changes {
		changeByName[change.RefName] = change
	}

	for _, remoteRef := range archiveResult.CurrentRefs {
		change, hasChange := changeByName[remoteRef.Name]
		archiveRefName := sql.NullString{}
		if hasChange && (change.Action == archivegit.ChangeActionCreate || change.Action == archivegit.ChangeActionUpdate) {
			archiveRefName = nullableString(change.ArchiveRefName)
		} else if existingRow, ok := currentByName[remoteRef.Name]; ok {
			archiveRefName = existingRow.ArchiveRefName
		}

		currentCommit := commitFields{}
		if existingRow, ok := currentByName[remoteRef.Name]; ok {
			currentCommit = commitFieldsFromExistingRow(existingRow)
		}
		if info, ok := commitMetadata.currentByRefName[remoteRef.Name]; ok {
			currentCommit = commitFieldsFromInfo(info)
		}

		if err := queries.UpsertRepoRefCurrent(ctx, dbsqlc.UpsertRepoRefCurrentParams{
			RepoID:                   repoID,
			RefName:                  remoteRef.Name,
			RefKind:                  remoteRef.Kind,
			CurrentHash:              remoteRef.Hash,
			Status:                   archivegit.RefStatusActive,
			ArchiveRefName:           archiveRefName,
			FirstSeenAt:              now,
			LastSeenAt:               now,
			LastHashUpdatedAt:        now,
			CurrentCommitAuthoredAt:  currentCommit.AuthoredAt,
			CurrentCommitCommittedAt: currentCommit.CommittedAt,
			CurrentCommitAuthorName:  currentCommit.AuthorName,
			CurrentCommitAuthorEmail: currentCommit.AuthorEmail,
			CurrentCommitMessage:     currentCommit.Message,
			DeletedAt:                sql.NullString{},
			CreatedAt:                now,
			UpdatedAt:                now,
		}); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("upsert current ref %q: %w", remoteRef.Name, err)
		}

		if !hasChange || (change.Action != archivegit.ChangeActionCreate && change.Action != archivegit.ChangeActionUpdate) {
			continue
		}

		newCommit := commitFields{}
		if info, ok := commitMetadata.newByRefName[change.RefName]; ok {
			newCommit = commitFieldsFromInfo(info)
		}

		if err := queries.CreateRepoRefChange(ctx, dbsqlc.CreateRepoRefChangeParams{
			RepoID:               repoID,
			TaskRunID:            taskRunID,
			RefName:              change.RefName,
			RefKind:              change.RefKind,
			Action:               change.Action,
			OldHash:              nullableString(change.OldHash),
			NewHash:              nullableString(change.NewHash),
			NewCommitAuthoredAt:  newCommit.AuthoredAt,
			NewCommitCommittedAt: newCommit.CommittedAt,
			NewCommitAuthorName:  newCommit.AuthorName,
			NewCommitAuthorEmail: newCommit.AuthorEmail,
			NewCommitMessage:     newCommit.Message,
			ArchiveRefName:       nullableString(change.ArchiveRefName),
			CreatedAt:            now,
		}); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert ref change %q: %w", change.RefName, err)
		}
	}

	for _, change := range archiveResult.Changes {
		if change.Action != archivegit.ChangeActionDelete {
			continue
		}

		if err := queries.MarkRepoRefCurrentDeleted(ctx, dbsqlc.MarkRepoRefCurrentDeletedParams{
			RepoID:     repoID,
			RefName:    change.RefName,
			LastSeenAt: now,
			UpdatedAt:  now,
		}); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("mark deleted ref %q: %w", change.RefName, err)
		}

		if err := queries.CreateRepoRefChange(ctx, dbsqlc.CreateRepoRefChangeParams{
			RepoID:               repoID,
			TaskRunID:            taskRunID,
			RefName:              change.RefName,
			RefKind:              change.RefKind,
			Action:               change.Action,
			OldHash:              nullableString(change.OldHash),
			NewHash:              sql.NullString{},
			NewCommitAuthoredAt:  sql.NullString{},
			NewCommitCommittedAt: sql.NullString{},
			NewCommitAuthorName:  sql.NullString{},
			NewCommitAuthorEmail: sql.NullString{},
			NewCommitMessage:     sql.NullString{},
			ArchiveRefName:       sql.NullString{},
			CreatedAt:            now,
		}); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert delete ref change %q: %w", change.RefName, err)
		}
	}

	if archiveRepoSizeBytes.Valid {
		if err := queries.UpdateRepoArchiveSize(ctx, dbsqlc.UpdateRepoArchiveSizeParams{
			ID:                   repoID,
			ArchiveRepoSizeBytes: archiveRepoSizeBytes,
			UpdatedAt:            now,
		}); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update archive size for repo %d: %w", repoID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit repo ref transaction: %w", err)
	}

	return nil
}

type commitFields struct {
	AuthoredAt  sql.NullString
	CommittedAt sql.NullString
	AuthorName  sql.NullString
	AuthorEmail sql.NullString
	Message     sql.NullString
}

func commitFieldsFromExistingRow(row dbsqlc.RepoRefsCurrent) commitFields {
	return commitFields{
		AuthoredAt:  row.CurrentCommitAuthoredAt,
		CommittedAt: row.CurrentCommitCommittedAt,
		AuthorName:  row.CurrentCommitAuthorName,
		AuthorEmail: row.CurrentCommitAuthorEmail,
		Message:     row.CurrentCommitMessage,
	}
}

func commitFieldsFromInfo(info archivegit.CommitInfo) commitFields {
	return commitFields{
		AuthoredAt:  nullableTime(info.AuthoredAt),
		CommittedAt: nullableTime(info.CommittedAt),
		AuthorName:  nullableString(info.AuthorName),
		AuthorEmail: nullableString(info.AuthorEmail),
		Message:     nullableString(info.Message),
	}
}

func (executor *Executor) recordRepoSyncFailure(ctx context.Context, db *sql.DB, request SyncRequest, outcome repoSyncOutcome) error {
	metaJSON, err := json.Marshal(map[string]any{
		"repo_id":   outcome.repo.ID,
		"source_id": request.Source.ID,
		"ref_id":    outcome.repo.RefID,
		"full_name": outcome.repo.FullName,
		"attempts":  outcome.attempts,
		"path":      outcome.path,
	})
	if err != nil {
		return fmt.Errorf("marshal repo sync failure meta: %w", err)
	}

	if err := dbsqlc.New(db).CreateTaskRunLog(ctx, dbsqlc.CreateTaskRunLogParams{
		TaskID:       request.RunID,
		EventType:    taskLogEventRepoSyncFailed,
		Summary:      nullableString("Repository sync failed"),
		MetaJson:     nullableString(string(metaJSON)),
		ErrorMessage: nullableString(outcome.err.Error()),
		CreatedAt:    executor.now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return fmt.Errorf("create repo sync failure log: %w", err)
	}

	return nil
}

func toArchiveCurrentRefs(rows []dbsqlc.RepoRefsCurrent) []archivegit.CurrentRef {
	refs := make([]archivegit.CurrentRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, archivegit.CurrentRef{
			Name:           row.RefName,
			Kind:           row.RefKind,
			Hash:           row.CurrentHash,
			Status:         row.Status,
			ArchiveRefName: row.ArchiveRefName.String,
		})
	}

	return refs
}

func gitAuthMethod(username string, token string) *githttp.BasicAuth {
	normalizedUsername := strings.TrimSpace(username)
	if normalizedUsername == "" {
		normalizedUsername = "git"
	}

	return &githttp.BasicAuth{
		Username: normalizedUsername,
		Password: token,
	}
}

func retryDelay(retryIndex int) time.Duration {
	if retryIndex <= 0 {
		return minRetryDelay
	}

	delay := minRetryDelay << (retryIndex - 1)
	if delay > maxRetryDelay {
		return maxRetryDelay
	}

	return delay
}

func shouldRefreshArchiveRepoSize(currentSize sql.NullInt64, archiveContentChanged bool) bool {
	return !currentSize.Valid || archiveContentChanged
}

func calculateDirectorySizeBytes(root string) (int64, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return 0, fmt.Errorf("root path is required")
	}

	var total int64
	if err := filepath.Walk(trimmedRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil || !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		return nil
	}); err != nil {
		return 0, err
	}

	return total, nil
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
