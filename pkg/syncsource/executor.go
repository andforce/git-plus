package syncsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

const defaultGitHubPerPage = 100

type Option func(*Executor)

type Executor struct {
	dataDir          string
	db               *sql.DB
	perPage          int
	now              func() time.Time
	sleep            func(context.Context, time.Duration) error
	openDB           func(context.Context, string) (*sql.DB, error)
	githubClient     githubClient
	repoArchiver     repositoryArchiver
	commitInfoLoader repositoryCommitInfoLoader
}

type progressUpdate struct {
	summary string
	meta    map[string]any
}

func NewExecutor(dataDir string, options ...Option) *Executor {
	executor := &Executor{
		dataDir:          dataDir,
		perPage:          defaultGitHubPerPage,
		now:              time.Now,
		sleep:            sleepWithContext,
		openDB:           appdb.Open,
		githubClient:     newGitHubAPIClient(defaultGitHubAPIBaseURL, nil),
		repoArchiver:     &goGitRepositoryArchiver{},
		commitInfoLoader: &goGitCommitInfoLoader{},
	}

	for _, option := range options {
		option(executor)
	}

	return executor
}

func WithHTTPClient(client *http.Client) Option {
	return func(executor *Executor) {
		baseURL := defaultGitHubAPIBaseURL
		if existingClient, ok := executor.githubClient.(*gitHubAPIClient); ok {
			baseURL = existingClient.baseURL
		}
		executor.githubClient = newGitHubAPIClient(baseURL, client)
	}
}

func WithGitHubAPIBaseURL(baseURL string) Option {
	return func(executor *Executor) {
		httpClient := (*http.Client)(nil)
		if existingClient, ok := executor.githubClient.(*gitHubAPIClient); ok {
			httpClient = existingClient.httpClient
		}
		executor.githubClient = newGitHubAPIClient(baseURL, httpClient)
	}
}

func WithNow(now func() time.Time) Option {
	return func(executor *Executor) {
		if now != nil {
			executor.now = now
		}
	}
}

func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(executor *Executor) {
		if sleep != nil {
			executor.sleep = sleep
		}
	}
}

func WithPerPage(perPage int) Option {
	return func(executor *Executor) {
		if perPage > 0 {
			executor.perPage = perPage
		}
	}
}

func WithGitHubClient(client githubClient) Option {
	return func(executor *Executor) {
		if client != nil {
			executor.githubClient = client
		}
	}
}

func WithDBOpener(openDB func(context.Context, string) (*sql.DB, error)) Option {
	return func(executor *Executor) {
		if openDB != nil {
			executor.openDB = openDB
		}
	}
}

func WithDatabase(db *sql.DB) Option {
	return func(executor *Executor) {
		if db != nil {
			executor.db = db
		}
	}
}

func WithRepositoryArchiver(repoArchiver repositoryArchiver) Option {
	return func(executor *Executor) {
		if repoArchiver != nil {
			executor.repoArchiver = repoArchiver
		}
	}
}

func WithCommitInfoLoader(loader repositoryCommitInfoLoader) Option {
	return func(executor *Executor) {
		if loader != nil {
			executor.commitInfoLoader = loader
		}
	}
}

func (executor *Executor) Sync(ctx context.Context, request SyncRequest, reporter ProgressReporter) error {
	if executor == nil {
		return fmt.Errorf("sync executor is required")
	}

	source := request.Source
	if err := reportProgress(reporter, "Loaded source configuration", map[string]any{
		"phase":            "load_source",
		"platform":         source.Platform,
		"include_defaults": source.IncludeDefaults,
		"include_starred":  source.IncludeStarred,
		"include_watching": source.IncludeWatching,
	}); err != nil {
		return err
	}

	resolvedRepos, candidateTotal, err := executor.resolveRepositories(ctx, source, reporter)
	if err != nil {
		return err
	}

	filteredRepos, filterStats := filterResolvedRepos(resolvedRepos, source.OnlyIncludeRepos, source.ExcludeRepos)
	if err := reportProgress(reporter, fmt.Sprintf("Filtered repositories (%d remaining)", len(filteredRepos)), map[string]any{
		"phase":                    "filter_repos",
		"candidate_total":          candidateTotal,
		"after_only_include_total": filterStats.AfterOnlyIncludeTotal,
		"after_exclude_total":      filterStats.AfterExcludeTotal,
	}); err != nil {
		return err
	}

	db := executor.db
	closeDB := func() {}
	if db == nil {
		openedDB, err := executor.openDB(ctx, executor.dataDir)
		if err != nil {
			return fmt.Errorf("open sqlite database: %w", err)
		}
		db = openedDB
		closeDB = func() {
			_ = openedDB.Close()
		}
	}
	defer closeDB()

	result, err := executor.syncSnapshot(ctx, db, source.ID, filteredRepos, reporter)
	if err != nil {
		return err
	}

	archiveResult, err := executor.syncActiveRepos(ctx, db, request, reporter)
	if err != nil {
		return err
	}

	return reportProgress(reporter, fmt.Sprintf("Archived %d repositories", archiveResult.Succeeded), map[string]any{
		"phase":             "done",
		"resolved_total":    result.ResolvedTotal,
		"inserted":          result.Inserted,
		"updated":           result.Updated,
		"reactivated":       result.Reactivated,
		"auto_excluded":     result.AutoExcluded,
		"archived_total":    archiveResult.Succeeded,
		"failed_total":      archiveResult.Failed,
		"change_count":      archiveResult.ChangeCount,
		"created_ref_count": archiveResult.CreatedRefCount,
		"updated_ref_count": archiveResult.UpdatedRefCount,
		"deleted_ref_count": archiveResult.DeletedRefCount,
	})
}

func (executor *Executor) resolveRepositories(ctx context.Context, source appconfig.SourceConfig, reporter ProgressReporter) ([]ResolvedRepo, int, error) {
	if strings.TrimSpace(source.Platform) != "github" {
		return nil, 0, fmt.Errorf("unsupported source platform %q", source.Platform)
	}

	reposByRefID := make(map[string]*ResolvedRepo)
	dedupedTotal := 0

	fetchers := []struct {
		enabled    bool
		phase      string
		originKind string
		fetch      func(context.Context, appconfig.SourceConfig, int, int) (githubPage, error)
	}{
		{
			enabled:    source.IncludeDefaults,
			phase:      "fetch_default",
			originKind: "default",
			fetch:      executor.githubClient.ListDefaultRepositories,
		},
		{
			enabled:    source.IncludeStarred,
			phase:      "fetch_starred",
			originKind: "starred",
			fetch:      executor.githubClient.ListStarredRepositories,
		},
		{
			enabled:    source.IncludeWatching,
			phase:      "fetch_watching",
			originKind: "watching",
			fetch:      executor.githubClient.ListWatchingRepositories,
		},
	}

	for _, fetcher := range fetchers {
		if !fetcher.enabled {
			continue
		}

		pageNumber := 1
		for {
			page, err := fetcher.fetch(ctx, source, pageNumber, executor.perPage)
			if err != nil {
				return nil, 0, fmt.Errorf("%s: %w", fetcher.phase, err)
			}

			for _, repo := range page.Repos {
				repo.SourceID = source.ID
				repo.Platform = source.Platform

				existingRepo, exists := reposByRefID[repo.RefID]
				if exists {
					existingRepo.AddOriginKind(fetcher.originKind)
					dedupedTotal++
					continue
				}

				repo.AddOriginKind(fetcher.originKind)
				reposByRefID[repo.RefID] = cloneResolvedRepo(repo)
			}

			if err := reportProgress(reporter, fmt.Sprintf("Fetching %s repositories (page %d, discovered %d)", fetcher.originKind, pageNumber, len(reposByRefID)), map[string]any{
				"phase":            fetcher.phase,
				"origin_kind":      fetcher.originKind,
				"page":             pageNumber,
				"per_page":         executor.perPage,
				"page_repo_count":  len(page.Repos),
				"discovered_total": len(reposByRefID),
				"deduped_total":    dedupedTotal,
				"has_next_page":    page.HasNextPage,
			}); err != nil {
				return nil, 0, err
			}

			if !page.HasNextPage {
				break
			}

			pageNumber++
		}
	}

	resolvedRepos := make([]ResolvedRepo, 0, len(reposByRefID))
	for _, repo := range reposByRefID {
		resolvedRepos = append(resolvedRepos, *repo)
	}

	sort.Slice(resolvedRepos, func(i int, j int) bool {
		return resolvedRepos[i].FullName < resolvedRepos[j].FullName
	})

	return resolvedRepos, len(reposByRefID), nil
}

func (executor *Executor) syncSnapshot(ctx context.Context, db *sql.DB, sourceID string, repos []ResolvedRepo, reporter ProgressReporter) (SnapshotResult, error) {
	now := executor.now().UTC().Format(time.RFC3339Nano)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return SnapshotResult{}, fmt.Errorf("begin repo snapshot transaction: %w", err)
	}

	result, updates, err := syncSnapshotTx(ctx, tx, sourceID, repos, now)
	if err != nil {
		_ = tx.Rollback()
		return SnapshotResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return SnapshotResult{}, fmt.Errorf("commit repo snapshot transaction: %w", err)
	}

	for _, update := range updates {
		if err := reportProgress(reporter, update.summary, update.meta); err != nil {
			return SnapshotResult{}, err
		}
	}

	return result, nil
}

func syncSnapshotTx(ctx context.Context, tx *sql.Tx, sourceID string, repos []ResolvedRepo, now string) (SnapshotResult, []progressUpdate, error) {
	queries := dbsqlc.New(tx)
	existingRepos, err := queries.ListReposForSource(ctx, sourceID)
	if err != nil {
		return SnapshotResult{}, nil, fmt.Errorf("list repos for source %q: %w", sourceID, err)
	}
	existingByRefID := make(map[string]dbsqlc.Repo, len(existingRepos))
	for _, existingRepo := range existingRepos {
		existingByRefID[existingRepo.RefID] = existingRepo
	}

	inserted := 0
	updated := 0
	reactivated := 0
	updates := make([]progressUpdate, 0, len(repos)+1)
	activeRefIDs := make(map[string]struct{}, len(repos))

	for index, repo := range repos {
		activeRefIDs[repo.RefID] = struct{}{}
		existingRepo, exists := existingByRefID[repo.RefID]

		if exists {
			params, err := buildUpdateRepoParams(repo, now)
			if err != nil {
				return SnapshotResult{}, nil, err
			}
			if err := queries.UpdateRepo(ctx, params); err != nil {
				return SnapshotResult{}, nil, fmt.Errorf("update repo %q/%q: %w", repo.SourceID, repo.RefID, err)
			}
			if existingRepo.Status == StatusAutoExcluded {
				reactivated++
			} else {
				updated++
			}
		} else {
			params, err := buildCreateRepoParams(repo, now)
			if err != nil {
				return SnapshotResult{}, nil, err
			}
			if err := queries.CreateRepo(ctx, params); err != nil {
				return SnapshotResult{}, nil, fmt.Errorf("insert repo %q/%q: %w", repo.SourceID, repo.RefID, err)
			}
			inserted++
		}

		if shouldReportPersistProgress(index+1, len(repos)) {
			updates = append(updates, progressUpdate{
				summary: fmt.Sprintf("Persisting repositories (%d/%d)", index+1, len(repos)),
				meta: map[string]any{
					"phase":        "persist_upserts",
					"target_total": len(repos),
					"processed":    index + 1,
					"inserted":     inserted,
					"updated":      updated,
					"reactivated":  reactivated,
				},
			})
		}
	}

	autoExcluded := 0
	for _, existingRepo := range existingRepos {
		if _, keepActive := activeRefIDs[existingRepo.RefID]; keepActive {
			continue
		}
		if existingRepo.Status == StatusAutoExcluded {
			continue
		}

		if err := queries.MarkRepoAutoExcluded(ctx, dbsqlc.MarkRepoAutoExcludedParams{
			SourceID:   sourceID,
			RefID:      existingRepo.RefID,
			Status:     StatusAutoExcluded,
			DisabledAt: nullableString(now),
			UpdatedAt:  now,
		}); err != nil {
			return SnapshotResult{}, nil, fmt.Errorf("mark auto excluded repo %q/%q: %w", sourceID, existingRepo.RefID, err)
		}
		autoExcluded++
	}

	updates = append(updates, progressUpdate{
		summary: fmt.Sprintf("Marked %d repositories as auto excluded", autoExcluded),
		meta: map[string]any{
			"phase":          "persist_auto_excluded",
			"existing_total": len(existingRepos),
			"kept_active":    len(repos),
			"auto_excluded":  autoExcluded,
		},
	})

	return SnapshotResult{
		ResolvedTotal: len(repos),
		Inserted:      inserted,
		Updated:       updated,
		Reactivated:   reactivated,
		AutoExcluded:  autoExcluded,
	}, updates, nil
}

func shouldReportPersistProgress(processed int, total int) bool {
	if processed <= 0 || total <= 0 {
		return false
	}

	if processed == 1 || processed == total {
		return true
	}

	return (processed-1)%100 == 0
}

func buildCreateRepoParams(repo ResolvedRepo, now string) (dbsqlc.CreateRepoParams, error) {
	originJSON, err := buildOriginJSON(repo)
	if err != nil {
		return dbsqlc.CreateRepoParams{}, err
	}

	return dbsqlc.CreateRepoParams{
		SourceID:      repo.SourceID,
		Platform:      repo.Platform,
		RefID:         repo.RefID,
		Status:        StatusActive,
		Name:          repo.Name,
		FullName:      repo.FullName,
		Owner:         repo.Owner,
		Description:   nullableString(repo.Description),
		HtmlUrl:       nullableString(repo.HTMLURL),
		CloneUrl:      nullableString(repo.CloneURL),
		SshUrl:        nullableString(repo.SSHURL),
		DefaultBranch: nullableString(repo.DefaultBranch),
		Visibility:    nullableString(repo.Visibility),
		IsPrivate:     int64(boolToInt(repo.IsPrivate)),
		IsFork:        int64(boolToInt(repo.IsFork)),
		IsArchived:    int64(boolToInt(repo.IsArchived)),
		Origin:        originJSON,
		Meta:          repo.MetaJSON,
		LastSeenAt:    now,
		DisabledAt:    sql.NullString{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func buildUpdateRepoParams(repo ResolvedRepo, now string) (dbsqlc.UpdateRepoParams, error) {
	originJSON, err := buildOriginJSON(repo)
	if err != nil {
		return dbsqlc.UpdateRepoParams{}, err
	}

	return dbsqlc.UpdateRepoParams{
		SourceID:      repo.SourceID,
		RefID:         repo.RefID,
		Platform:      repo.Platform,
		Status:        StatusActive,
		Name:          repo.Name,
		FullName:      repo.FullName,
		Owner:         repo.Owner,
		Description:   nullableString(repo.Description),
		HtmlUrl:       nullableString(repo.HTMLURL),
		CloneUrl:      nullableString(repo.CloneURL),
		SshUrl:        nullableString(repo.SSHURL),
		DefaultBranch: nullableString(repo.DefaultBranch),
		Visibility:    nullableString(repo.Visibility),
		IsPrivate:     int64(boolToInt(repo.IsPrivate)),
		IsFork:        int64(boolToInt(repo.IsFork)),
		IsArchived:    int64(boolToInt(repo.IsArchived)),
		Origin:        originJSON,
		Meta:          repo.MetaJSON,
		LastSeenAt:    now,
		DisabledAt:    sql.NullString{},
		UpdatedAt:     now,
	}, nil
}

func buildOriginJSON(repo ResolvedRepo) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"kinds": repo.OriginKinds(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal repo origin for %q/%q: %w", repo.SourceID, repo.RefID, err)
	}

	return string(payload), nil
}

func cloneResolvedRepo(repo ResolvedRepo) *ResolvedRepo {
	cloned := repo
	if len(repo.originKinds) > 0 {
		cloned.originKinds = make(map[string]struct{}, len(repo.originKinds))
		for kind := range repo.originKinds {
			cloned.originKinds[kind] = struct{}{}
		}
	}

	return &cloned
}

func nullableString(value string) sql.NullString {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return sql.NullString{}
	}

	return sql.NullString{
		String: trimmed,
		Valid:  true,
	}
}

func nullableTime(value time.Time) sql.NullString {
	if value.IsZero() {
		return sql.NullString{}
	}

	return sql.NullString{
		String: value.UTC().Format(time.RFC3339Nano),
		Valid:  true,
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func reportProgress(reporter ProgressReporter, summary string, meta map[string]any) error {
	if reporter == nil {
		return nil
	}

	cleanMeta := make(map[string]any, len(meta))
	for key, value := range meta {
		if value == nil {
			continue
		}
		cleanMeta[key] = value
	}

	return reporter.SetProgress(summary, cleanMeta)
}
