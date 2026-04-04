package reposervice

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	appdb "github.com/ImSingee/git-plus/db"
	dbsqlc "github.com/ImSingee/git-plus/db/sqlc"
	"github.com/ImSingee/git-plus/pkg/repodownload"
	repov1 "github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1"
	"github.com/ImSingee/git-plus/pkg/rpc/gitplus/repo/v1/repov1connect"
)

func TestStreamRepositoryDownloadProducesReadyArchive(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	_, mainHash, devHash, tagHash := createArchiveFixture(t, dataDir, "source-a", "1", true)
	seedActiveDownloadRefs(t, sqliteDB, repoID, mainHash, devHash, tagHash, true)

	manager := repodownload.NewManager()
	t.Cleanup(manager.Close)

	server := httptest.NewServer(NewHandler(
		dataDir,
		WithDatabase(sqliteDB),
		WithDownloadManager(manager),
	))
	defer server.Close()

	client := repov1connect.NewRepoServiceClient(server.Client(), server.URL+"/api")
	stream, err := client.StreamRepositoryDownload(
		context.Background(),
		connect.NewRequest(&repov1.StreamRepositoryDownloadRequest{RepoId: &repoID}),
	)
	if err != nil {
		t.Fatalf("stream repository download: %v", err)
	}

	events := make([]*repov1.StreamRepositoryDownloadResponse, 0)
	for stream.Receive() {
		events = append(events, stream.Msg())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream receive: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected multiple stream events, got %d", len(events))
	}

	lastEvent := events[len(events)-1]
	if got := lastEvent.GetState(); got != repov1.DownloadState_DOWNLOAD_STATE_READY {
		t.Fatalf("expected ready event, got %v", got)
	}
	if lastEvent.GetDownloadId() == "" {
		t.Fatal("expected ready event to include download id")
	}

	response, err := server.Client().Get(
		server.URL + "/api/repos/1/downloads/" + lastEvent.GetDownloadId() + "/archive",
	)
	if err != nil {
		t.Fatalf("download archive: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.Contains(got, "application/zip") {
		t.Fatalf("unexpected content type: %q", got)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read archive response: %v", err)
	}

	repoPath := unzipDownloadedRepository(t, body)
	if got := runGitOutput(t, repoPath, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
		t.Fatalf("unexpected HEAD: %q", got)
	}

	branches := strings.Fields(runGitOutput(t, repoPath, "branch", "--format=%(refname:short)"))
	if !containsString(branches, "main") || !containsString(branches, "dev") {
		t.Fatalf("expected local branches main and dev, got %#v", branches)
	}

	tags := strings.Fields(runGitOutput(t, repoPath, "tag", "--list"))
	if !containsString(tags, "v1") {
		t.Fatalf("expected tag v1, got %#v", tags)
	}
}

func TestStreamRepositoryDownloadSupportsTagOnlyRepositories(t *testing.T) {
	dataDir, sqliteDB := openRepoServiceDownloadTestEnv(t)
	defer sqliteDB.Close()

	repoID := seedRepoServiceRepoData(t, sqliteDB)
	_, _, _, tagHash := createArchiveFixture(t, dataDir, "source-a", "1", false)
	seedActiveDownloadRefs(t, sqliteDB, repoID, "", "", tagHash, false)

	manager := repodownload.NewManager()
	t.Cleanup(manager.Close)

	server := httptest.NewServer(NewHandler(
		dataDir,
		WithDatabase(sqliteDB),
		WithDownloadManager(manager),
	))
	defer server.Close()

	client := repov1connect.NewRepoServiceClient(server.Client(), server.URL+"/api")
	stream, err := client.StreamRepositoryDownload(
		context.Background(),
		connect.NewRequest(&repov1.StreamRepositoryDownloadRequest{RepoId: &repoID}),
	)
	if err != nil {
		t.Fatalf("stream repository download: %v", err)
	}

	var lastEvent *repov1.StreamRepositoryDownloadResponse
	for stream.Receive() {
		lastEvent = stream.Msg()
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream receive: %v", err)
	}
	if lastEvent == nil {
		t.Fatal("expected at least one stream event")
	}
	if got := lastEvent.GetState(); got != repov1.DownloadState_DOWNLOAD_STATE_READY {
		t.Fatalf("expected ready event, got %v", got)
	}
}

func TestDownloadEndpointRejectsRepoMismatch(t *testing.T) {
	dataDir := t.TempDir()
	manager := repodownload.NewManager()
	t.Cleanup(manager.Close)

	downloadRoot := filepath.Join(dataDir, "download")
	if err := os.MkdirAll(downloadRoot, 0o755); err != nil {
		t.Fatalf("create download root: %v", err)
	}
	zipPath := filepath.Join(downloadRoot, "repo.zip")
	if err := os.WriteFile(zipPath, []byte("zip"), 0o644); err != nil {
		t.Fatalf("write zip file: %v", err)
	}

	session := manager.Register(1, zipPath, "repo-snapshot.zip", downloadRoot)
	server := httptest.NewServer(NewHandler(
		dataDir,
		WithDownloadManager(manager),
	))
	defer server.Close()

	response, err := server.Client().Get(
		server.URL + "/api/repos/2/downloads/" + session.ID + "/archive",
	)
	if err != nil {
		t.Fatalf("request mismatched download: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for repo mismatch, got %d", response.StatusCode)
	}
}

func openRepoServiceDownloadTestEnv(t *testing.T) (string, *sql.DB) {
	t.Helper()

	dataDir := t.TempDir()
	if err := appdb.Migrate(context.Background(), dataDir); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	sqliteDB, err := appdb.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	return dataDir, sqliteDB
}

func seedActiveDownloadRefs(t *testing.T, db *sql.DB, repoID int64, mainHash string, devHash string, tagHash string, includeBranch bool) {
	t.Helper()

	queries := dbsqlc.New(db)
	if _, err := db.Exec(
		`UPDATE repos SET default_branch = ?, archive_repo_size_bytes = ? WHERE id = ?`,
		"main",
		50*1024*1024,
		repoID,
	); err != nil {
		t.Fatalf("update repo default branch: %v", err)
	}

	if includeBranch {
		for _, ref := range []struct {
			name string
			kind string
			hash string
		}{
			{name: "refs/heads/main", kind: "head", hash: mainHash},
			{name: "refs/heads/dev", kind: "head", hash: devHash},
		} {
			if err := queries.UpsertRepoRefCurrent(context.Background(), dbsqlc.UpsertRepoRefCurrentParams{
				RepoID:            repoID,
				RefName:           ref.name,
				RefKind:           ref.kind,
				CurrentHash:       ref.hash,
				Status:            "active",
				ArchiveRefName:    sql.NullString{},
				FirstSeenAt:       "2026-04-04T08:00:00Z",
				LastSeenAt:        "2026-04-04T10:00:00Z",
				LastHashUpdatedAt: "2026-04-04T10:00:00Z",
				DeletedAt:         sql.NullString{},
				CreatedAt:         "2026-04-04T08:00:00Z",
				UpdatedAt:         "2026-04-04T10:00:00Z",
			}); err != nil {
				t.Fatalf("seed active ref %q: %v", ref.name, err)
			}
		}
	}

	if err := queries.UpsertRepoRefCurrent(context.Background(), dbsqlc.UpsertRepoRefCurrentParams{
		RepoID:            repoID,
		RefName:           "refs/tags/v1",
		RefKind:           "tag",
		CurrentHash:       tagHash,
		Status:            "active",
		ArchiveRefName:    sql.NullString{},
		FirstSeenAt:       "2026-04-04T08:00:00Z",
		LastSeenAt:        "2026-04-04T10:00:00Z",
		LastHashUpdatedAt: "2026-04-04T10:00:00Z",
		DeletedAt:         sql.NullString{},
		CreatedAt:         "2026-04-04T08:00:00Z",
		UpdatedAt:         "2026-04-04T10:00:00Z",
	}); err != nil {
		t.Fatalf("seed active tag: %v", err)
	}
}

func createArchiveFixture(t *testing.T, dataDir string, sourceID string, refID string, includeBranches bool) (string, string, string, string) {
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
	runGitCommand(t, workPath, "branch", "dev")
	runGitCommand(t, workPath, "tag", "v1")

	mainHash := runGitOutput(t, workPath, "rev-parse", "main")
	devHash := runGitOutput(t, workPath, "rev-parse", "dev")
	tagHash := runGitOutput(t, workPath, "rev-parse", "v1")

	barePath := filepath.Join(t.TempDir(), "archive.git")
	runGitCommand(t, filepath.Dir(barePath), "clone", "--bare", workPath, barePath)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "refs/archive/heads/main/"+mainHash, mainHash)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "refs/archive/heads/dev/"+devHash, devHash)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "refs/archive/tags/v1/"+tagHash, tagHash)
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/heads/main")
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/heads/dev")
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/tags/v1")
	runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "symbolic-ref", "HEAD", "refs/heads/missing")

	if !includeBranches {
		runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/archive/heads/main/"+mainHash)
		runGitCommand(t, filepath.Dir(barePath), "--git-dir", barePath, "update-ref", "-d", "refs/archive/heads/dev/"+devHash)
	}

	targetPath := filepath.Join(dataDir, "repos", sourceID, refID)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatalf("create repo archive parent: %v", err)
	}
	if err := os.Rename(barePath, targetPath); err != nil {
		t.Fatalf("move archive fixture: %v", err)
	}

	return targetPath, mainHash, devHash, tagHash
}

func unzipDownloadedRepository(t *testing.T, archive []byte) string {
	t.Helper()

	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("open zip archive: %v", err)
	}

	rootDir := t.TempDir()
	for _, file := range reader.File {
		targetPath := filepath.Join(rootDir, file.Name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				t.Fatalf("create archive dir: %v", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			t.Fatalf("create archive file parent: %v", err)
		}
		if file.Mode()&os.ModeSymlink != 0 {
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open symlink archive entry: %v", err)
			}
			linkTarget, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read symlink archive entry: %v", err)
			}
			if err := os.Symlink(string(linkTarget), targetPath); err != nil {
				t.Fatalf("write symlink archive entry: %v", err)
			}
			continue
		}

		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open archive file: %v", err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read archive file: %v", err)
		}
		if err := os.WriteFile(targetPath, content, file.Mode()); err != nil {
			t.Fatalf("write archive file: %v", err)
		}
	}

	return filepath.Join(rootDir, "core")
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
