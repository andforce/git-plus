package reposervice

import (
	"context"
	"database/sql"
	"testing"

	"connectrpc.com/connect"
	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	repov1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1"
)

func TestListRefsReturnsLastHashUpdatedAtAndCurrentCommit(t *testing.T) {
	sqliteDB := openRepoServiceTestDB(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	queries := dbsqlc.New(sqliteDB)
	if err := queries.UpsertRepoRefCurrent(context.Background(), dbsqlc.UpsertRepoRefCurrentParams{
		RepoID:                   repoID,
		RefName:                  "refs/heads/main",
		RefKind:                  "head",
		CurrentHash:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:                   "active",
		ArchiveRefName:           testNullString("refs/archive/heads/main/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		FirstSeenAt:              "2026-04-04T08:00:00Z",
		LastSeenAt:               "2026-04-04T10:00:00Z",
		LastHashUpdatedAt:        "2026-04-04T09:30:00Z",
		CurrentCommitAuthoredAt:  testNullString("2026-04-04T09:00:00Z"),
		CurrentCommitCommittedAt: testNullString("2026-04-04T09:15:00Z"),
		CurrentCommitAuthorName:  testNullString("Alice"),
		CurrentCommitAuthorEmail: testNullString("alice@example.com"),
		CurrentCommitMessage:     testNullString("main update"),
		DeletedAt:                sql.NullString{},
		CreatedAt:                "2026-04-04T08:00:00Z",
		UpdatedAt:                "2026-04-04T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed current ref: %v", err)
	}

	server := newServiceServer(t.TempDir(), WithDatabase(sqliteDB))
	response, err := server.ListRefs(context.Background(), connect.NewRequest(&repov1.ListRefsRequest{
		RepoId:  &repoID,
		RefKind: stringPtr("head"),
	}))
	if err != nil {
		t.Fatalf("list refs: %v", err)
	}
	if len(response.Msg.GetRefs()) != 1 {
		t.Fatalf("expected one ref, got %d", len(response.Msg.GetRefs()))
	}

	ref := response.Msg.GetRefs()[0]
	if got := ref.GetLastHashUpdatedAt().AsTime().UTC().Format("2006-01-02T15:04:05Z"); got != "2026-04-04T09:30:00Z" {
		t.Fatalf("unexpected last_hash_updated_at: %s", got)
	}
	if ref.GetCurrentCommit() == nil {
		t.Fatal("expected current_commit to be present")
	}
	if got := ref.GetCurrentCommit().GetAuthorName(); got != "Alice" {
		t.Fatalf("unexpected current commit author: %q", got)
	}
	if got := ref.GetCurrentCommit().GetCommittedAt().AsTime().UTC().Format("2006-01-02T15:04:05Z"); got != "2026-04-04T09:15:00Z" {
		t.Fatalf("unexpected current commit committed_at: %s", got)
	}
}

func TestListRefChangesReturnsNewCommitAndKeepsDeleteEmpty(t *testing.T) {
	sqliteDB := openRepoServiceTestDB(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	queries := dbsqlc.New(sqliteDB)
	if err := queries.CreateTaskRun(context.Background(), dbsqlc.CreateTaskRunParams{
		TaskID:               "task-1",
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
		t.Fatalf("seed task run: %v", err)
	}
	if err := queries.CreateRepoRefChange(context.Background(), dbsqlc.CreateRepoRefChangeParams{
		RepoID:               repoID,
		TaskRunID:            "task-1",
		RefName:              "refs/heads/main",
		RefKind:              "head",
		Action:               "update",
		OldHash:              testNullString("1111111111111111111111111111111111111111"),
		NewHash:              testNullString("2222222222222222222222222222222222222222"),
		NewCommitAuthoredAt:  testNullString("2026-04-04T09:00:00Z"),
		NewCommitCommittedAt: testNullString("2026-04-04T09:05:00Z"),
		NewCommitAuthorName:  testNullString("Alice"),
		NewCommitAuthorEmail: testNullString("alice@example.com"),
		NewCommitMessage:     testNullString("main update"),
		ArchiveRefName:       testNullString("refs/archive/heads/main/2222222222222222222222222222222222222222"),
		CreatedAt:            "2026-04-04T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed update change: %v", err)
	}
	if err := queries.CreateRepoRefChange(context.Background(), dbsqlc.CreateRepoRefChangeParams{
		RepoID:               repoID,
		TaskRunID:            "task-1",
		RefName:              "refs/heads/legacy",
		RefKind:              "head",
		Action:               "delete",
		OldHash:              testNullString("3333333333333333333333333333333333333333"),
		NewHash:              sql.NullString{},
		NewCommitAuthoredAt:  sql.NullString{},
		NewCommitCommittedAt: sql.NullString{},
		NewCommitAuthorName:  sql.NullString{},
		NewCommitAuthorEmail: sql.NullString{},
		NewCommitMessage:     sql.NullString{},
		ArchiveRefName:       sql.NullString{},
		CreatedAt:            "2026-04-04T11:00:00Z",
	}); err != nil {
		t.Fatalf("seed delete change: %v", err)
	}

	server := newServiceServer(t.TempDir(), WithDatabase(sqliteDB))
	response, err := server.ListRefChanges(context.Background(), connect.NewRequest(&repov1.ListRefChangesRequest{
		RepoId: &repoID,
	}))
	if err != nil {
		t.Fatalf("list ref changes: %v", err)
	}
	if len(response.Msg.GetChanges()) != 2 {
		t.Fatalf("expected two changes, got %d", len(response.Msg.GetChanges()))
	}

	changesByAction := make(map[string]*repov1.RepoRefChange, len(response.Msg.GetChanges()))
	for _, change := range response.Msg.GetChanges() {
		changesByAction[change.GetAction()] = change
	}

	updateChange := changesByAction["update"]
	if updateChange == nil || updateChange.GetNewCommit() == nil {
		t.Fatalf("expected update change to include new_commit, got %#v", updateChange)
	}
	if got := updateChange.GetNewCommit().GetAuthorEmail(); got != "alice@example.com" {
		t.Fatalf("unexpected new commit author email: %q", got)
	}

	deleteChange := changesByAction["delete"]
	if deleteChange == nil {
		t.Fatalf("expected delete change to be present, got %#v", changesByAction)
	}
	if deleteChange.GetNewCommit() != nil {
		t.Fatalf("expected delete change new_commit to be nil, got %#v", deleteChange.GetNewCommit())
	}
}

func openRepoServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	return sqliteDB
}

func seedRepoServiceRepoData(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO repos (
			id, source_id, platform, ref_id, status, name, full_name, owner,
			description, html_url, clone_url, ssh_url, default_branch, visibility,
			is_private, is_fork, is_archived, origin, meta, last_seen_at, disabled_at,
			created_at, updated_at
		) VALUES (
			1, 'source-a', 'github', '1', 'active', 'core', 'acme/core', 'acme',
			NULL, NULL, NULL, NULL, NULL, NULL,
			0, 0, 0, '{}', '{}', '2026-04-04T10:00:00Z', NULL,
			'2026-04-04T08:00:00Z', '2026-04-04T10:00:00Z'
		)
	`); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	return 1
}

func testNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}
