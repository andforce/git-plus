package reposervice

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	repov1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1"
)

func TestListTreeDefaultsToDefaultBranchAndReturnsReadme(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	_, mainHash, devHash, tagHash := createArchiveFixture(t, dataDir, "source-a", "1", true)
	seedActiveDownloadRefs(t, sqliteDB, repoID, mainHash, devHash, tagHash, true)

	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.ListTree(context.Background(), connect.NewRequest(&repov1.ListTreeRequest{
		RepoId: &repoID,
	}))
	if err != nil {
		t.Fatalf("list tree: %v", err)
	}

	if got := response.Msg.GetRefName(); got != "refs/heads/main" {
		t.Fatalf("unexpected ref name: %q", got)
	}
	if got := response.Msg.GetCommitHash(); got != mainHash {
		t.Fatalf("unexpected commit hash: %q", got)
	}
	if len(response.Msg.GetEntries()) != 1 {
		t.Fatalf("expected one tree entry, got %d", len(response.Msg.GetEntries()))
	}
	entry := response.Msg.GetEntries()[0]
	if got := entry.GetPath(); got != "README.md" {
		t.Fatalf("unexpected entry path: %q", got)
	}
	if got := entry.GetKind(); got != "file" {
		t.Fatalf("unexpected entry kind: %q", got)
	}
	if response.Msg.GetReadme() == nil {
		t.Fatal("expected README to be returned")
	}
	if got := response.Msg.GetReadme().GetContent(); got != "hello\n" {
		t.Fatalf("unexpected README content: %q", got)
	}

	searchResponse, err := server.SearchFiles(context.Background(), connect.NewRequest(&repov1.SearchFilesRequest{
		RepoId:   &repoID,
		RefName:  stringPtr("main"),
		Query:    stringPtr("read"),
		PageSize: int32Ptr(10),
	}))
	if err != nil {
		t.Fatalf("search files: %v", err)
	}
	if len(searchResponse.Msg.GetEntries()) != 1 {
		t.Fatalf("expected one file search result, got %d", len(searchResponse.Msg.GetEntries()))
	}
	if got := searchResponse.Msg.GetEntries()[0].GetPath(); got != "README.md" {
		t.Fatalf("unexpected search result path: %q", got)
	}

	codeSearchResponse, err := server.SearchCode(context.Background(), connect.NewRequest(&repov1.SearchCodeRequest{
		RepoId:   &repoID,
		RefName:  stringPtr("main"),
		Query:    stringPtr("hello"),
		PageSize: int32Ptr(10),
	}))
	if err != nil {
		t.Fatalf("search code: %v", err)
	}
	if len(codeSearchResponse.Msg.GetMatches()) != 1 {
		t.Fatalf("expected one code search result, got %d", len(codeSearchResponse.Msg.GetMatches()))
	}
	match := codeSearchResponse.Msg.GetMatches()[0]
	if got := match.GetPath(); got != "README.md" {
		t.Fatalf("unexpected code search path: %q", got)
	}
	if got := match.GetLineNo(); got != 1 {
		t.Fatalf("unexpected code search line: %d", got)
	}
	if got := match.GetLine(); got != "hello" {
		t.Fatalf("unexpected code search content: %q", got)
	}
}

func TestGetBlobReturnsTextContent(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	_, mainHash, devHash, tagHash := createArchiveFixture(t, dataDir, "source-a", "1", true)
	seedActiveDownloadRefs(t, sqliteDB, repoID, mainHash, devHash, tagHash, true)

	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.GetBlob(context.Background(), connect.NewRequest(&repov1.GetBlobRequest{
		RepoId:  &repoID,
		RefName: stringPtr("main"),
		Path:    stringPtr("README.md"),
	}))
	if err != nil {
		t.Fatalf("get blob: %v", err)
	}

	if got := response.Msg.GetCommitHash(); got != mainHash {
		t.Fatalf("unexpected commit hash: %q", got)
	}
	if got := response.Msg.GetName(); got != "README.md" {
		t.Fatalf("unexpected blob name: %q", got)
	}
	if got := response.Msg.GetContent(); got != "hello\n" {
		t.Fatalf("unexpected blob content: %q", got)
	}
	if response.Msg.GetIsBinary() {
		t.Fatal("expected README to be detected as text")
	}

	commitResponse, err := server.GetBlob(context.Background(), connect.NewRequest(&repov1.GetBlobRequest{
		RepoId:  &repoID,
		RefName: stringPtr(mainHash[:12]),
		Path:    stringPtr("README.md"),
	}))
	if err != nil {
		t.Fatalf("get blob by commit hash: %v", err)
	}
	if got := commitResponse.Msg.GetCommitHash(); got != mainHash {
		t.Fatalf("unexpected commit hash for commit ref: %q", got)
	}
	if got := commitResponse.Msg.GetContent(); got != "hello\n" {
		t.Fatalf("unexpected commit blob content: %q", got)
	}
}

func TestGetBlameReturnsLineAuthors(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	_, mainHash, devHash, tagHash := createArchiveFixture(t, dataDir, "source-a", "1", true)
	seedActiveDownloadRefs(t, sqliteDB, repoID, mainHash, devHash, tagHash, true)

	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.GetBlame(context.Background(), connect.NewRequest(&repov1.GetBlameRequest{
		RepoId:  &repoID,
		RefName: stringPtr("main"),
		Path:    stringPtr("README.md"),
	}))
	if err != nil {
		t.Fatalf("get blame: %v", err)
	}

	if got := response.Msg.GetCommitHash(); got != mainHash {
		t.Fatalf("unexpected commit hash: %q", got)
	}
	if len(response.Msg.GetLines()) != 1 {
		t.Fatalf("expected one blamed line, got %d", len(response.Msg.GetLines()))
	}
	line := response.Msg.GetLines()[0]
	if got := line.GetCommitHash(); got != mainHash {
		t.Fatalf("unexpected blame commit hash: %q", got)
	}
	if got := line.GetAuthorName(); got != "Test" {
		t.Fatalf("unexpected blame author: %q", got)
	}
	if got := line.GetContent(); got != "hello" {
		t.Fatalf("unexpected blame content: %q", got)
	}
}

func TestListCommitsReturnsRefHistory(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	_, mainHash, devHash, tagHash := createArchiveFixture(t, dataDir, "source-a", "1", true)
	seedActiveDownloadRefs(t, sqliteDB, repoID, mainHash, devHash, tagHash, true)

	pageSize := int32(10)
	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.ListCommits(context.Background(), connect.NewRequest(&repov1.ListCommitsRequest{
		RepoId:   &repoID,
		RefName:  stringPtr("refs/heads/main"),
		PageSize: &pageSize,
	}))
	if err != nil {
		t.Fatalf("list commits: %v", err)
	}

	if len(response.Msg.GetCommits()) != 1 {
		t.Fatalf("expected one commit, got %d", len(response.Msg.GetCommits()))
	}
	commit := response.Msg.GetCommits()[0]
	if got := commit.GetHash(); got != mainHash {
		t.Fatalf("unexpected commit hash: %q", got)
	}
	if got := commit.GetMessage(); got != "init\n" {
		t.Fatalf("unexpected commit message: %q", got)
	}
	if got := commit.GetAuthorName(); got != "Test" {
		t.Fatalf("unexpected commit author: %q", got)
	}
}

func TestGetCommitReturnsFileDiffs(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	secondHash := createTwoCommitArchiveFixture(t, dataDir, "source-a", "1")
	seedSingleActiveRef(t, sqliteDB, repoID, "refs/heads/main", "head", secondHash)

	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.GetCommit(context.Background(), connect.NewRequest(&repov1.GetCommitRequest{
		RepoId:     &repoID,
		CommitHash: stringPtr(secondHash),
	}))
	if err != nil {
		t.Fatalf("get commit: %v", err)
	}

	if got := response.Msg.GetCommit().GetHash(); got != secondHash {
		t.Fatalf("unexpected commit hash: %q", got)
	}
	if response.Msg.GetAdditions() == 0 {
		t.Fatal("expected additions to be counted")
	}
	if len(response.Msg.GetFiles()) != 2 {
		t.Fatalf("expected two changed files, got %d", len(response.Msg.GetFiles()))
	}

	filesByPath := make(map[string]*repov1.CommitFileDiff)
	for _, file := range response.Msg.GetFiles() {
		filesByPath[file.GetNewPath()] = file
	}

	readmeDiff := filesByPath["README.md"]
	if readmeDiff == nil {
		t.Fatalf("expected README diff, got %#v", filesByPath)
	}
	if got := readmeDiff.GetStatus(); got != "modified" {
		t.Fatalf("unexpected README status: %q", got)
	}
	if !strings.Contains(readmeDiff.GetPatch(), "+world") {
		t.Fatalf("expected README patch to include added line, got:\n%s", readmeDiff.GetPatch())
	}

	sourceDiff := filesByPath["src/app.go"]
	if sourceDiff == nil {
		t.Fatalf("expected src/app.go diff, got %#v", filesByPath)
	}
	if got := sourceDiff.GetStatus(); got != "added" {
		t.Fatalf("unexpected src/app.go status: %q", got)
	}
}

func TestCompareRefsReturnsCommitsAndDiff(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	mainHash, featureHash := createCompareArchiveFixture(t, dataDir, "source-a", "1")
	seedSingleActiveRef(t, sqliteDB, repoID, "refs/heads/main", "head", mainHash)
	seedSingleActiveRef(t, sqliteDB, repoID, "refs/heads/feature", "head", featureHash)

	pageSize := int32(10)
	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.CompareRefs(context.Background(), connect.NewRequest(&repov1.CompareRefsRequest{
		RepoId:      &repoID,
		BaseRefName: stringPtr("main"),
		HeadRefName: stringPtr("feature"),
		PageSize:    &pageSize,
	}))
	if err != nil {
		t.Fatalf("compare refs: %v", err)
	}

	if got := response.Msg.GetBaseRefName(); got != "refs/heads/main" {
		t.Fatalf("unexpected base ref: %q", got)
	}
	if got := response.Msg.GetHeadRefName(); got != "refs/heads/feature" {
		t.Fatalf("unexpected head ref: %q", got)
	}
	if got := response.Msg.GetBaseCommitHash(); got != mainHash {
		t.Fatalf("unexpected base hash: %q", got)
	}
	if got := response.Msg.GetHeadCommitHash(); got != featureHash {
		t.Fatalf("unexpected head hash: %q", got)
	}
	if got := response.Msg.GetMergeBaseHash(); got != mainHash {
		t.Fatalf("unexpected merge base: %q", got)
	}
	if got := response.Msg.GetAheadCount(); got != 1 {
		t.Fatalf("unexpected ahead count: %d", got)
	}
	if got := response.Msg.GetBehindCount(); got != 0 {
		t.Fatalf("unexpected behind count: %d", got)
	}
	if len(response.Msg.GetCommits()) != 1 {
		t.Fatalf("expected one compare commit, got %d", len(response.Msg.GetCommits()))
	}
	if got := response.Msg.GetCommits()[0].GetHash(); got != featureHash {
		t.Fatalf("unexpected compare commit hash: %q", got)
	}
	if response.Msg.GetAdditions() == 0 {
		t.Fatal("expected additions to be counted")
	}

	filesByPath := make(map[string]*repov1.CommitFileDiff)
	for _, file := range response.Msg.GetFiles() {
		filesByPath[file.GetNewPath()] = file
	}
	if filesByPath["README.md"] == nil {
		t.Fatalf("expected README diff, got %#v", filesByPath)
	}
	sourceDiff := filesByPath["src/app.go"]
	if sourceDiff == nil {
		t.Fatalf("expected src/app.go diff, got %#v", filesByPath)
	}
	if got := sourceDiff.GetStatus(); got != "added" {
		t.Fatalf("unexpected src/app.go status: %q", got)
	}
}

func TestListCommitsFiltersByPath(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	secondHash := createTwoCommitArchiveFixture(t, dataDir, "source-a", "1")
	seedSingleActiveRef(t, sqliteDB, repoID, "refs/heads/main", "head", secondHash)

	pageSize := int32(10)
	server := newServiceServer(dataDir, WithDatabase(sqliteDB))
	response, err := server.ListCommits(context.Background(), connect.NewRequest(&repov1.ListCommitsRequest{
		RepoId:   &repoID,
		RefName:  stringPtr("main"),
		PageSize: &pageSize,
		Path:     stringPtr("src/app.go"),
	}))
	if err != nil {
		t.Fatalf("list path commits: %v", err)
	}

	if len(response.Msg.GetCommits()) != 1 {
		t.Fatalf("expected one commit for src/app.go, got %d", len(response.Msg.GetCommits()))
	}
	if got := response.Msg.GetCommits()[0].GetHash(); got != secondHash {
		t.Fatalf("unexpected path commit hash: %q", got)
	}
}

func createTwoCommitArchiveFixture(t *testing.T, dataDir string, sourceID string, refID string) string {
	t.Helper()

	workPath := filepath.Join(t.TempDir(), "source")
	runGitCommand(t, filepath.Dir(workPath), "init", "--initial-branch=main", workPath)
	runGitCommand(t, workPath, "config", "user.name", "Test")
	runGitCommand(t, workPath, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(workPath, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGitCommand(t, workPath, "add", "README.md")
	runGitCommand(t, workPath, "commit", "-m", "init")

	if err := os.MkdirAll(filepath.Join(workPath, "src"), 0o755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("update readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "src", "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatalf("write app.go: %v", err)
	}
	runGitCommand(t, workPath, "add", "README.md", "src/app.go")
	runGitCommand(t, workPath, "commit", "-m", "update readme")

	secondHash := runGitOutput(t, workPath, "rev-parse", "main")
	barePath := filepath.Join(t.TempDir(), "archive.git")
	runGitCommand(t, filepath.Dir(barePath), "clone", "--bare", workPath, barePath)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "refs/archive/heads/main/"+secondHash, secondHash)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/heads/main")
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "symbolic-ref", "HEAD", "refs/heads/missing")

	targetPath := filepath.Join(dataDir, "repos", sourceID, refID)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create repo archive parent: %v", err)
	}
	if err := os.Rename(barePath, targetPath); err != nil {
		t.Fatalf("move archive fixture: %v", err)
	}

	return secondHash
}

func createCompareArchiveFixture(t *testing.T, dataDir string, sourceID string, refID string) (string, string) {
	t.Helper()

	workPath := filepath.Join(t.TempDir(), "source")
	runGitCommand(t, filepath.Dir(workPath), "init", "--initial-branch=main", workPath)
	runGitCommand(t, workPath, "config", "user.name", "Test")
	runGitCommand(t, workPath, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(workPath, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	runGitCommand(t, workPath, "add", "README.md")
	runGitCommand(t, workPath, "commit", "-m", "init")
	mainHash := runGitOutput(t, workPath, "rev-parse", "main")

	runGitCommand(t, workPath, "switch", "-c", "feature")
	if err := os.MkdirAll(filepath.Join(workPath, "src"), 0o755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "README.md"), []byte("hello\nfeature\n"), 0o644); err != nil {
		t.Fatalf("update readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workPath, "src", "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatalf("write app.go: %v", err)
	}
	runGitCommand(t, workPath, "add", "README.md", "src/app.go")
	runGitCommand(t, workPath, "commit", "-m", "add feature")
	featureHash := runGitOutput(t, workPath, "rev-parse", "feature")

	barePath := filepath.Join(t.TempDir(), "archive.git")
	runGitCommand(t, filepath.Dir(barePath), "clone", "--bare", workPath, barePath)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "refs/archive/heads/main/"+mainHash, mainHash)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "refs/archive/heads/feature/"+featureHash, featureHash)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/heads/main")
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/heads/feature")
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "symbolic-ref", "HEAD", "refs/heads/missing")

	targetPath := filepath.Join(dataDir, "repos", sourceID, refID)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create repo archive parent: %v", err)
	}
	if err := os.Rename(barePath, targetPath); err != nil {
		t.Fatalf("move archive fixture: %v", err)
	}

	return mainHash, featureHash
}

func seedSingleActiveRef(t *testing.T, db *sql.DB, repoID int64, refName string, refKind string, hash string) {
	t.Helper()

	if _, err := db.Exec(
		`UPDATE repos SET default_branch = ? WHERE id = ?`,
		"main",
		repoID,
	); err != nil {
		t.Fatalf("update repo default branch: %v", err)
	}

	if err := dbsqlc.New(db).UpsertRepoRefCurrent(context.Background(), dbsqlc.UpsertRepoRefCurrentParams{
		RepoID:            repoID,
		RefName:           refName,
		RefKind:           refKind,
		CurrentHash:       hash,
		Status:            "active",
		ArchiveRefName:    sql.NullString{},
		FirstSeenAt:       "2026-04-04T08:00:00Z",
		LastSeenAt:        "2026-04-04T10:00:00Z",
		LastHashUpdatedAt: "2026-04-04T10:00:00Z",
		DeletedAt:         sql.NullString{},
		CreatedAt:         "2026-04-04T08:00:00Z",
		UpdatedAt:         "2026-04-04T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed active ref %q: %v", refName, err)
	}
}
