package syncsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	"github.com/ImSingee/git-plus/pkg/archivegit"
	appconfig "github.com/ImSingee/git-plus/pkg/config"
)

func TestExecutorSyncFetchesFiltersAndPersistsRepositories(t *testing.T) {
	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		page := query.Get("page")
		perPage := query.Get("per_page")

		var payload any
		switch {
		case r.URL.Path == "/user/repos" && page == "1" && perPage == "2":
			payload = []map[string]any{
				githubRepositoryPayload(1, "acme", "core", "Core repo"),
				githubRepositoryPayload(2, "acme", "tool-backup", "Backup repo"),
			}
		case r.URL.Path == "/user/repos" && page == "2" && perPage == "2":
			payload = []map[string]any{
				githubRepositoryPayload(3, "other", "skip", "Skip repo"),
			}
		case r.URL.Path == "/user/starred" && page == "1" && perPage == "2":
			payload = []map[string]any{
				githubRepositoryPayload(1, "acme", "core", "Core repo"),
				githubRepositoryPayload(4, "acme", "excluded", "Excluded repo"),
			}
		case r.URL.Path == "/user/starred" && page == "2" && perPage == "2":
			payload = []map[string]any{}
		case r.URL.Path == "/user/subscriptions" && page == "1" && perPage == "2":
			payload = []map[string]any{
				githubRepositoryPayload(5, "misc", "watch", "Watched repo"),
			}
		default:
			t.Fatalf("unexpected github request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Fatalf("encode payload: %v", err)
		}
	}))
	defer server.Close()

	reporter := &recordingReporter{}
	executor := NewExecutor(
		dataDir,
		WithGitHubAPIBaseURL(server.URL),
		WithHTTPClient(server.Client()),
		WithPerPage(2),
		WithRepositoryArchiver(stubRepositoryArchiver{}),
		WithNow(func() time.Time {
			return time.Date(2026, time.April, 4, 8, 0, 0, 0, time.UTC)
		}),
	)

	source := appconfig.SourceConfig{
		ID:               "github-main",
		Platform:         "github",
		Token:            "plain-token",
		IncludeDefaults:  true,
		IncludeStarred:   true,
		IncludeWatching:  true,
		OnlyIncludeRepos: []string{"acme/*", "*-backup"},
		ExcludeRepos:     []string{"acme/excluded"},
	}

	if err := executor.Sync(context.Background(), SyncRequest{Source: source, Concurrency: 1}, reporter); err != nil {
		t.Fatalf("sync source: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqliteDB.Close()

	repos := loadRepoRows(t, sqliteDB, source.ID)
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	coreRepo := findRepoByRefID(t, repos, "1")
	if coreRepo.Status != StatusActive {
		t.Fatalf("expected core repo to be active, got %q", coreRepo.Status)
	}
	if coreRepo.FullName != "acme/core" {
		t.Fatalf("unexpected full_name: %q", coreRepo.FullName)
	}
	assertOriginKinds(t, coreRepo.Origin, []string{"default", "starred"})

	backupRepo := findRepoByRefID(t, repos, "2")
	if backupRepo.Status != StatusActive {
		t.Fatalf("expected backup repo to be active, got %q", backupRepo.Status)
	}
	assertOriginKinds(t, backupRepo.Origin, []string{"default"})

	assertProgressMeta(t, reporter.progresses, "fetch_default", map[string]any{
		"page":             1,
		"page_repo_count":  2,
		"discovered_total": 2,
		"has_next_page":    true,
	})
	assertProgressMeta(t, reporter.progresses, "fetch_default", map[string]any{
		"page":             2,
		"page_repo_count":  1,
		"discovered_total": 3,
		"has_next_page":    false,
	})
	assertProgressMeta(t, reporter.progresses, "filter_repos", map[string]any{
		"candidate_total":          5,
		"after_only_include_total": 3,
		"after_exclude_total":      2,
	})
	assertProgressMeta(t, reporter.progresses, "done", map[string]any{
		"resolved_total":    2,
		"inserted":          2,
		"updated":           0,
		"reactivated":       0,
		"auto_excluded":     0,
		"archived_total":    2,
		"failed_total":      0,
		"change_count":      0,
		"created_ref_count": 0,
		"updated_ref_count": 0,
		"deleted_ref_count": 0,
	})
}

func TestSyncSnapshotTxMarksAutoExcludedAndReactivatesRepos(t *testing.T) {
	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqliteDB.Close()

	firstRepos := []ResolvedRepo{
		testResolvedRepo("source-a", "1", "acme/core", []string{"default"}, "Core repo"),
		testResolvedRepo("source-a", "2", "acme/backup", []string{"starred"}, "Backup repo"),
	}
	runSnapshotSync(t, sqliteDB, "source-a", firstRepos, "2026-04-04T08:00:00Z")

	secondReporter := &recordingReporter{}
	secondRepos := []ResolvedRepo{
		testResolvedRepo("source-a", "1", "acme/core", []string{"default", "starred"}, "Core repo updated"),
		testResolvedRepo("source-a", "3", "acme/new", []string{"watching"}, "New repo"),
	}
	secondResult := runSnapshotSyncWithReporter(t, sqliteDB, "source-a", secondRepos, "2026-04-04T09:00:00Z", secondReporter)
	if secondResult.Inserted != 1 || secondResult.Updated != 1 || secondResult.Reactivated != 0 || secondResult.AutoExcluded != 1 {
		t.Fatalf("unexpected second sync result: %#v", secondResult)
	}

	reposAfterSecondSync := loadRepoRows(t, sqliteDB, "source-a")
	if findRepoByRefID(t, reposAfterSecondSync, "2").Status != StatusAutoExcluded {
		t.Fatalf("expected repo 2 to be auto excluded")
	}
	if findRepoByRefID(t, reposAfterSecondSync, "1").Description != "Core repo updated" {
		t.Fatalf("expected repo 1 description to be updated")
	}

	thirdRepos := []ResolvedRepo{
		testResolvedRepo("source-a", "2", "acme/backup", []string{"default"}, "Backup repo restored"),
	}
	thirdResult := runSnapshotSync(t, sqliteDB, "source-a", thirdRepos, "2026-04-04T10:00:00Z")
	if thirdResult.Reactivated != 1 {
		t.Fatalf("expected repo 2 to be reactivated, got %#v", thirdResult)
	}

	reposAfterThirdSync := loadRepoRows(t, sqliteDB, "source-a")
	reactivatedRepo := findRepoByRefID(t, reposAfterThirdSync, "2")
	if reactivatedRepo.Status != StatusActive {
		t.Fatalf("expected repo 2 to be active after reactivation, got %q", reactivatedRepo.Status)
	}
	if reactivatedRepo.DisabledAt.Valid {
		t.Fatalf("expected repo 2 disabled_at to be cleared after reactivation")
	}

	assertProgressMeta(t, secondReporter.progresses, "persist_auto_excluded", map[string]any{
		"existing_total": 2,
		"kept_active":    2,
		"auto_excluded":  1,
	})
}

func TestExecutorSyncSnapshotReportsProgressAfterTransactionCommit(t *testing.T) {
	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open primary database: %v", err)
	}
	defer sqliteDB.Close()

	reporterDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open reporter database: %v", err)
	}
	defer reporterDB.Close()

	executor := NewExecutor(
		dataDir,
		WithNow(func() time.Time {
			return time.Date(2026, time.April, 4, 8, 0, 0, 0, time.UTC)
		}),
	)

	repos := []ResolvedRepo{
		testResolvedRepo("source-a", "1", "acme/core", []string{"default"}, "Core repo"),
	}

	reporter := &dbWritingReporter{db: reporterDB}
	result, err := executor.syncSnapshot(context.Background(), sqliteDB, "source-a", repos, reporter)
	if err != nil {
		t.Fatalf("sync snapshot: %v", err)
	}
	if result.Inserted != 1 {
		t.Fatalf("expected one inserted repo, got %#v", result)
	}

	var progressWrites int
	if err := reporterDB.QueryRow("SELECT COUNT(1) FROM app_meta WHERE key LIKE 'progress-%'").Scan(&progressWrites); err != nil {
		t.Fatalf("count progress writes: %v", err)
	}
	if progressWrites == 0 {
		t.Fatal("expected reporter writes to succeed")
	}
}

func TestShouldReportPersistProgress(t *testing.T) {
	tests := []struct {
		name      string
		processed int
		total     int
		want      bool
	}{
		{name: "invalid processed", processed: 0, total: 10, want: false},
		{name: "first item", processed: 1, total: 805, want: true},
		{name: "not every item", processed: 21, total: 805, want: false},
		{name: "hundred plus one", processed: 101, total: 805, want: true},
		{name: "next hundred plus one", processed: 201, total: 805, want: true},
		{name: "not exact hundred", processed: 200, total: 805, want: false},
		{name: "last item", processed: 805, total: 805, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReportPersistProgress(tt.processed, tt.total); got != tt.want {
				t.Fatalf(
					"shouldReportPersistProgress(%d, %d) = %v, want %v",
					tt.processed,
					tt.total,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestExecutorSyncWithSharedDatabaseDoesNotCloseIt(t *testing.T) {
	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqliteDB.Close()

	executor := NewExecutor(
		dataDir,
		WithDatabase(sqliteDB),
		WithGitHubClient(stubGitHubClient{
			defaultPage: githubPage{
				Repos: []ResolvedRepo{
					testResolvedRepo("source-a", "1", "acme/core", []string{"default"}, "Core repo"),
				},
			},
		}),
		WithRepositoryArchiver(stubRepositoryArchiver{}),
		WithNow(func() time.Time {
			return time.Date(2026, time.April, 4, 8, 0, 0, 0, time.UTC)
		}),
	)

	source := appconfig.SourceConfig{
		ID:              "source-a",
		Platform:        "github",
		Token:           "plain-token",
		IncludeDefaults: true,
	}

	if err := executor.Sync(context.Background(), SyncRequest{Source: source, Concurrency: 1}, &recordingReporter{}); err != nil {
		t.Fatalf("sync source with shared database: %v", err)
	}

	if err := sqliteDB.PingContext(context.Background()); err != nil {
		t.Fatalf("shared database should remain open: %v", err)
	}

	repos := loadRepoRows(t, sqliteDB, source.ID)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
}

func TestExecutorSyncActiveReposRetriesPersistsRefsAndLogsFailures(t *testing.T) {
	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer sqliteDB.Close()

	runSnapshotSync(t, sqliteDB, "source-a", []ResolvedRepo{
		testResolvedRepo("source-a", "1", "acme/core", []string{"default"}, "Core repo"),
		testResolvedRepo("source-a", "2", "acme/fail", []string{"default"}, "Fail repo"),
		testResolvedRepo("source-a", "3", "acme/skip", []string{"default"}, "Skip repo"),
	}, "2026-04-04T08:00:00Z")
	runSnapshotSync(t, sqliteDB, "source-a", []ResolvedRepo{
		testResolvedRepo("source-a", "1", "acme/core", []string{"default"}, "Core repo"),
		testResolvedRepo("source-a", "2", "acme/fail", []string{"default"}, "Fail repo"),
	}, "2026-04-04T09:00:00Z")

	queries := dbsqlc.New(sqliteDB)
	repos, err := queries.ListReposForSource(context.Background(), "source-a")
	if err != nil {
		t.Fatalf("list repos for source: %v", err)
	}

	repoByRefID := make(map[string]dbsqlc.Repo, len(repos))
	for _, repo := range repos {
		repoByRefID[repo.RefID] = repo
	}

	coreRepo := repoByRefID["1"]
	skipRepo := repoByRefID["3"]
	if skipRepo.Status != StatusAutoExcluded {
		t.Fatalf("expected repo 3 to be auto excluded, got %q", skipRepo.Status)
	}

	if err := queries.UpsertRepoRefCurrent(context.Background(), dbsqlc.UpsertRepoRefCurrentParams{
		RepoID:         coreRepo.ID,
		RefName:        "refs/heads/main",
		RefKind:        "head",
		CurrentHash:    strings.Repeat("1", 40),
		Status:         "active",
		ArchiveRefName: nullableString("refs/archive/heads/main/" + strings.Repeat("1", 40)),
		FirstSeenAt:    "2026-04-04T08:00:00Z",
		LastSeenAt:     "2026-04-04T08:00:00Z",
		DeletedAt:      sql.NullString{},
		CreatedAt:      "2026-04-04T08:00:00Z",
		UpdatedAt:      "2026-04-04T08:00:00Z",
	}); err != nil {
		t.Fatalf("seed main ref: %v", err)
	}
	if err := queries.UpsertRepoRefCurrent(context.Background(), dbsqlc.UpsertRepoRefCurrentParams{
		RepoID:         coreRepo.ID,
		RefName:        "refs/heads/legacy",
		RefKind:        "head",
		CurrentHash:    strings.Repeat("2", 40),
		Status:         "active",
		ArchiveRefName: nullableString("refs/archive/heads/legacy/" + strings.Repeat("2", 40)),
		FirstSeenAt:    "2026-04-04T08:00:00Z",
		LastSeenAt:     "2026-04-04T08:00:00Z",
		DeletedAt:      sql.NullString{},
		CreatedAt:      "2026-04-04T08:00:00Z",
		UpdatedAt:      "2026-04-04T08:00:00Z",
	}); err != nil {
		t.Fatalf("seed legacy ref: %v", err)
	}
	if err := queries.CreateTaskRun(context.Background(), dbsqlc.CreateTaskRunParams{
		TaskID:               "task-sync-1",
		ParentTaskID:         sql.NullString{},
		JobID:                "sync-source:source-a",
		JobType:              "sync-source",
		Name:                 "Sync source source-a",
		ArgsJson:             sql.NullString{},
		Status:               "running",
		CreatedAt:            "2026-04-04T10:00:00Z",
		StartedAt:            "2026-04-04T10:00:00Z",
		FinishedAt:           sql.NullString{},
		ErrorMessage:         sql.NullString{},
		LastProgressSummary:  sql.NullString{},
		LastProgressMetaJson: sql.NullString{},
		UpdatedAt:            "2026-04-04T10:00:00Z",
	}); err != nil {
		t.Fatalf("create task run: %v", err)
	}

	attempts := map[string]int{}
	var recordedSleeps []time.Duration
	reporter := &recordingReporter{}
	executor := NewExecutor(
		dataDir,
		WithDatabase(sqliteDB),
		WithRepositoryArchiver(stubRepositoryArchiver{
			sync: func(_ context.Context, request repositoryArchiveRequest) (repositoryArchiveResult, error) {
				refID := filepath.Base(request.Path)
				attempts[refID]++

				switch refID {
				case "1":
					if attempts[refID] == 1 {
						return repositoryArchiveResult{}, fmt.Errorf("temporary network error")
					}

					return repositoryArchiveResult{
						CurrentRefs: []archivegit.RemoteRef{
							{
								Name: "refs/heads/main",
								Kind: archivegit.RefKindHead,
								Hash: strings.Repeat("3", 40),
							},
							{
								Name: "refs/tags/v1.0.0",
								Kind: archivegit.RefKindTag,
								Hash: strings.Repeat("4", 40),
							},
						},
						Changes: []archivegit.Change{
							{
								RefName: "refs/heads/legacy",
								RefKind: archivegit.RefKindHead,
								OldHash: strings.Repeat("2", 40),
								Action:  archivegit.ChangeActionDelete,
							},
							{
								RefName:        "refs/heads/main",
								RefKind:        archivegit.RefKindHead,
								OldHash:        strings.Repeat("1", 40),
								NewHash:        strings.Repeat("3", 40),
								Action:         archivegit.ChangeActionUpdate,
								ArchiveRefName: "refs/archive/heads/main/" + strings.Repeat("3", 40),
							},
							{
								RefName:        "refs/tags/v1.0.0",
								RefKind:        archivegit.RefKindTag,
								NewHash:        strings.Repeat("4", 40),
								Action:         archivegit.ChangeActionCreate,
								ArchiveRefName: "refs/archive/tags/v1.0.0/" + strings.Repeat("4", 40),
							},
						},
					}, nil
				case "2":
					return repositoryArchiveResult{}, fmt.Errorf("permanent auth error")
				default:
					t.Fatalf("unexpected archive request for repo path %q", request.Path)
					return repositoryArchiveResult{}, nil
				}
			},
		}),
		WithSleep(func(_ context.Context, delay time.Duration) error {
			recordedSleeps = append(recordedSleeps, delay)
			return nil
		}),
		WithNow(func() time.Time {
			return time.Date(2026, time.April, 4, 10, 0, 0, 0, time.UTC)
		}),
	)

	result, err := executor.syncActiveRepos(context.Background(), sqliteDB, SyncRequest{
		RunID:         "task-sync-1",
		Source:        appconfig.SourceConfig{ID: "source-a", Username: "octocat", Token: "plain-token"},
		Concurrency:   2,
		MaxRetryTimes: 2,
	}, reporter)
	if err != nil {
		t.Fatalf("sync active repos: %v", err)
	}

	if result.TargetTotal != 2 || result.Processed != 2 || result.Succeeded != 1 || result.Failed != 1 {
		t.Fatalf("unexpected archive result: %#v", result)
	}
	if result.Retried != 3 {
		t.Fatalf("expected total retries to be 3, got %#v", result)
	}
	if result.ChangeCount != 3 || result.CreatedRefCount != 1 || result.UpdatedRefCount != 1 || result.DeletedRefCount != 1 {
		t.Fatalf("unexpected change counters: %#v", result)
	}

	expectedSleeps := []time.Duration{10 * time.Second, 10 * time.Second, 20 * time.Second}
	if !slices.Equal(recordedSleeps, expectedSleeps) {
		t.Fatalf("unexpected retry delays: got %v want %v", recordedSleeps, expectedSleeps)
	}

	currentRefs, err := queries.ListRepoRefsCurrentByRepoID(context.Background(), coreRepo.ID)
	if err != nil {
		t.Fatalf("list current refs: %v", err)
	}
	currentByName := make(map[string]dbsqlc.RepoRefsCurrent, len(currentRefs))
	for _, currentRef := range currentRefs {
		currentByName[currentRef.RefName] = currentRef
	}

	if currentByName["refs/heads/main"].CurrentHash != strings.Repeat("3", 40) {
		t.Fatalf("unexpected main hash: %#v", currentByName["refs/heads/main"])
	}
	if currentByName["refs/heads/legacy"].Status != archivegit.RefStatusDeleted {
		t.Fatalf("expected legacy branch to be deleted, got %#v", currentByName["refs/heads/legacy"])
	}
	if currentByName["refs/tags/v1.0.0"].Status != archivegit.RefStatusActive {
		t.Fatalf("expected tag to be active, got %#v", currentByName["refs/tags/v1.0.0"])
	}

	rows, err := sqliteDB.Query(`
		SELECT ref_name, action
		FROM repo_ref_changes
		WHERE repo_id = ?
		ORDER BY ref_name, action
	`, coreRepo.ID)
	if err != nil {
		t.Fatalf("query repo ref changes: %v", err)
	}
	defer rows.Close()

	var changePairs []string
	for rows.Next() {
		var refName string
		var action string
		if err := rows.Scan(&refName, &action); err != nil {
			t.Fatalf("scan repo ref change: %v", err)
		}
		changePairs = append(changePairs, refName+":"+action)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate repo ref changes: %v", err)
	}
	if !slices.Equal(changePairs, []string{
		"refs/heads/legacy:delete",
		"refs/heads/main:update",
		"refs/tags/v1.0.0:create",
	}) {
		t.Fatalf("unexpected repo ref changes: %v", changePairs)
	}

	logRows, err := sqliteDB.Query(`
		SELECT event_type, meta_json, error_message
		FROM task_run_logs
		WHERE task_id = ?
		ORDER BY id
	`, "task-sync-1")
	if err != nil {
		t.Fatalf("query task logs: %v", err)
	}
	defer logRows.Close()

	var failureLogCount int
	for logRows.Next() {
		var eventType string
		var metaJSON sql.NullString
		var errorMessage sql.NullString
		if err := logRows.Scan(&eventType, &metaJSON, &errorMessage); err != nil {
			t.Fatalf("scan task log: %v", err)
		}
		if eventType != taskLogEventRepoSyncFailed {
			continue
		}
		failureLogCount++

		var meta map[string]any
		if err := json.Unmarshal([]byte(metaJSON.String), &meta); err != nil {
			t.Fatalf("decode task log meta: %v", err)
		}
		if meta["ref_id"] != "2" {
			t.Fatalf("unexpected failure log meta: %#v", meta)
		}
		if meta["attempts"] != float64(3) {
			t.Fatalf("expected attempts=3 in failure log, got %#v", meta)
		}
		if !strings.Contains(errorMessage.String, "permanent auth error") {
			t.Fatalf("unexpected failure log error: %q", errorMessage.String)
		}
	}
	if err := logRows.Err(); err != nil {
		t.Fatalf("iterate task logs: %v", err)
	}
	if failureLogCount != 1 {
		t.Fatalf("expected one failure task log, got %d", failureLogCount)
	}

	assertProgressMeta(t, reporter.progresses, "load_active_repos", map[string]any{
		"target_total": 2,
	})
	assertProgressMeta(t, reporter.progresses, "sync_active_repos", map[string]any{
		"target_total": 2,
		"processed":    2,
		"succeeded":    1,
		"failed":       1,
		"retried":      3,
	})
}

type recordingReporter struct {
	progresses []progressRecord
}

type stubGitHubClient struct {
	defaultPage  githubPage
	starredPage  githubPage
	watchingPage githubPage
}

func (client stubGitHubClient) ListDefaultRepositories(_ context.Context, _ appconfig.SourceConfig, _ int, _ int) (githubPage, error) {
	return client.defaultPage, nil
}

func (client stubGitHubClient) ListStarredRepositories(_ context.Context, _ appconfig.SourceConfig, _ int, _ int) (githubPage, error) {
	return client.starredPage, nil
}

func (client stubGitHubClient) ListWatchingRepositories(_ context.Context, _ appconfig.SourceConfig, _ int, _ int) (githubPage, error) {
	return client.watchingPage, nil
}

type progressRecord struct {
	summary string
	meta    map[string]any
}

type stubRepositoryArchiver struct {
	sync func(context.Context, repositoryArchiveRequest) (repositoryArchiveResult, error)
}

func (archiver stubRepositoryArchiver) SyncRepository(ctx context.Context, request repositoryArchiveRequest) (repositoryArchiveResult, error) {
	if archiver.sync != nil {
		return archiver.sync(ctx, request)
	}

	return repositoryArchiveResult{}, nil
}

func (reporter *recordingReporter) SetProgress(summary string, meta map[string]any) error {
	clonedMeta := make(map[string]any, len(meta))
	for key, value := range meta {
		clonedMeta[key] = value
	}

	reporter.progresses = append(reporter.progresses, progressRecord{
		summary: summary,
		meta:    clonedMeta,
	})

	return nil
}

type repoRow struct {
	RefID       string
	Status      string
	FullName    string
	Description string
	Origin      string
	DisabledAt  sql.NullString
}

type dbWritingReporter struct {
	db    *sql.DB
	count int
}

func (reporter *dbWritingReporter) SetProgress(summary string, meta map[string]any) error {
	reporter.count++
	_, err := reporter.db.Exec(
		"INSERT OR REPLACE INTO app_meta (key, value) VALUES (?, ?)",
		progressKey(reporter.count),
		summary,
	)
	return err
}

func loadRepoRows(t *testing.T, db *sql.DB, sourceID string) []repoRow {
	t.Helper()

	rows, err := db.Query(`
		SELECT ref_id, status, full_name, COALESCE(description, ''), origin, disabled_at
		FROM repos
		WHERE source_id = ?
		ORDER BY ref_id
	`, sourceID)
	if err != nil {
		t.Fatalf("query repos: %v", err)
	}
	defer rows.Close()

	var repos []repoRow
	for rows.Next() {
		var repo repoRow
		if err := rows.Scan(&repo.RefID, &repo.Status, &repo.FullName, &repo.Description, &repo.Origin, &repo.DisabledAt); err != nil {
			t.Fatalf("scan repo: %v", err)
		}
		repos = append(repos, repo)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate repos: %v", err)
	}

	return repos
}

func findRepoByRefID(t *testing.T, repos []repoRow, refID string) repoRow {
	t.Helper()

	for _, repo := range repos {
		if repo.RefID == refID {
			return repo
		}
	}

	t.Fatalf("repo %q not found", refID)
	return repoRow{}
}

func assertOriginKinds(t *testing.T, originJSON string, expected []string) {
	t.Helper()

	var origin struct {
		Kinds []string `json:"kinds"`
	}
	if err := json.Unmarshal([]byte(originJSON), &origin); err != nil {
		t.Fatalf("decode origin json: %v", err)
	}

	if !slices.Equal(origin.Kinds, expected) {
		t.Fatalf("expected origin kinds %v, got %v", expected, origin.Kinds)
	}
}

func assertProgressMeta(t *testing.T, progresses []progressRecord, phase string, expected map[string]any) {
	t.Helper()

	for _, progress := range progresses {
		if progress.meta["phase"] != phase {
			continue
		}

		matched := true
		for key, value := range expected {
			if progress.meta[key] != value {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}

	t.Fatalf("progress phase %q with meta %#v not found; got %#v", phase, expected, progresses)
}

func githubRepositoryPayload(id int64, owner string, name string, description string) map[string]any {
	fullName := path.Join(owner, name)
	return map[string]any{
		"id":             id,
		"name":           name,
		"full_name":      fullName,
		"description":    description,
		"html_url":       "https://github.com/" + fullName,
		"clone_url":      "https://github.com/" + fullName + ".git",
		"ssh_url":        "git@github.com:" + fullName + ".git",
		"default_branch": "main",
		"visibility":     "public",
		"private":        false,
		"fork":           false,
		"archived":       false,
		"owner": map[string]any{
			"login": owner,
		},
	}
}

func progressKey(count int) string {
	return fmt.Sprintf("progress-%d", count)
}

func testResolvedRepo(sourceID string, refID string, fullName string, originKinds []string, description string) ResolvedRepo {
	name := fullName[strings.LastIndex(fullName, "/")+1:]
	repo := ResolvedRepo{
		SourceID:      sourceID,
		Platform:      "github",
		RefID:         refID,
		Name:          name,
		FullName:      fullName,
		Owner:         fullName[:strings.Index(fullName, "/")],
		Description:   description,
		HTMLURL:       "https://github.com/" + fullName,
		CloneURL:      "https://github.com/" + fullName + ".git",
		SSHURL:        "git@github.com:" + fullName + ".git",
		DefaultBranch: "main",
		Visibility:    "public",
		MetaJSON:      `{"id":"` + refID + `"}`,
	}
	for _, kind := range originKinds {
		repo.AddOriginKind(kind)
	}

	return repo
}

func runSnapshotSync(t *testing.T, db *sql.DB, sourceID string, repos []ResolvedRepo, now string) SnapshotResult {
	t.Helper()
	return runSnapshotSyncWithReporter(t, db, sourceID, repos, now, nil)
}

func runSnapshotSyncWithReporter(t *testing.T, db *sql.DB, sourceID string, repos []ResolvedRepo, now string, reporter ProgressReporter) SnapshotResult {
	t.Helper()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	result, updates, err := syncSnapshotTx(context.Background(), tx, sourceID, repos, now)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("sync snapshot tx: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}

	for _, update := range updates {
		if err := reportProgress(reporter, update.summary, update.meta); err != nil {
			t.Fatalf("report progress: %v", err)
		}
	}

	return result
}
