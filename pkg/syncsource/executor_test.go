package syncsource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	appdb "github.com/ImSingee/git-plus/db"
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

	if err := executor.Sync(context.Background(), source, reporter); err != nil {
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
		"resolved_total": 2,
		"inserted":       2,
		"updated":        0,
		"reactivated":    0,
		"auto_excluded":  0,
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

type recordingReporter struct {
	progresses []progressRecord
}

type progressRecord struct {
	summary string
	meta    map[string]any
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
