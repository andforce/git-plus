package repodownload

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegisterAndExpireSession(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
	currentTime := now

	rootPath := t.TempDir()
	zipPath := filepath.Join(rootPath, "repo.zip")
	if err := os.WriteFile(zipPath, []byte("zip"), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	manager := NewManager(
		WithTTL(2*time.Minute),
		WithNow(func() time.Time { return currentTime }),
		WithIDGenerator(func() string { return "download-1" }),
	)
	t.Cleanup(manager.Close)

	session := manager.Register(1, zipPath, "repo-snapshot.zip", rootPath)
	if session.ID != "download-1" {
		t.Fatalf("unexpected session id: %q", session.ID)
	}

	if _, err := manager.Get(1, session.ID); err != nil {
		t.Fatalf("get active session: %v", err)
	}

	currentTime = now.Add(3 * time.Minute)
	if _, err := manager.Get(1, session.ID); err != ErrNotFound {
		t.Fatalf("expected expired session to be removed, got %v", err)
	}

	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Fatalf("expected expired session root to be removed, got %v", err)
	}
}

func TestGetRejectsRepoMismatch(t *testing.T) {
	manager := NewManager(
		WithIDGenerator(func() string { return "download-1" }),
	)
	t.Cleanup(manager.Close)

	rootPath := t.TempDir()
	zipPath := filepath.Join(rootPath, "repo.zip")
	if err := os.WriteFile(zipPath, []byte("zip"), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	session := manager.Register(1, zipPath, "repo-snapshot.zip", rootPath)
	if _, err := manager.Get(2, session.ID); err != ErrRepoMismatch {
		t.Fatalf("expected repo mismatch error, got %v", err)
	}
}
