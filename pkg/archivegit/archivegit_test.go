package archivegit

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestArchiveRefName(t *testing.T) {
	tests := []struct {
		name    string
		refName string
		hash    string
		want    string
		wantErr bool
	}{
		{
			name:    "head ref",
			refName: "refs/heads/main",
			hash:    strings.Repeat("a", 40),
			want:    "refs/archive/heads/main/" + strings.Repeat("a", 40),
		},
		{
			name:    "nested tag ref",
			refName: "refs/tags/releases/v1.0.0",
			hash:    strings.Repeat("b", 40),
			want:    "refs/archive/tags/releases/v1.0.0/" + strings.Repeat("b", 40),
		},
		{
			name:    "unsupported ref",
			refName: "refs/notes/build",
			hash:    strings.Repeat("c", 40),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ArchiveRefName(tt.refName, tt.hash)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("archive ref name: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ArchiveRefName(%q, %q) = %q, want %q", tt.refName, tt.hash, got, tt.want)
			}
		})
	}
}

func TestDiffRefsTracksCreateUpdateDeleteAndDeletedRowsAsAbsent(t *testing.T) {
	changes := DiffRefs(
		[]CurrentRef{
			{
				Name:   "refs/heads/main",
				Kind:   RefKindHead,
				Hash:   strings.Repeat("1", 40),
				Status: RefStatusActive,
			},
			{
				Name:   "refs/heads/stale",
				Kind:   RefKindHead,
				Hash:   strings.Repeat("2", 40),
				Status: RefStatusActive,
			},
			{
				Name:   "refs/tags/v0.9.0",
				Kind:   RefKindTag,
				Hash:   strings.Repeat("3", 40),
				Status: RefStatusDeleted,
			},
		},
		[]RemoteRef{
			{
				Name: "refs/heads/main",
				Kind: RefKindHead,
				Hash: strings.Repeat("4", 40),
			},
			{
				Name: "refs/tags/v0.9.0",
				Kind: RefKindTag,
				Hash: strings.Repeat("3", 40),
			},
		},
	)

	var got []string
	for _, change := range changes {
		got = append(got, change.RefName+":"+change.Action)
	}

	if !slices.Equal(got, []string{
		"refs/heads/main:update",
		"refs/heads/stale:delete",
		"refs/tags/v0.9.0:create",
	}) {
		t.Fatalf("unexpected changes: %v", got)
	}
}

func TestListRemoteRefsTreatsEmptyRemoteAsNoRefs(t *testing.T) {
	tempDir := t.TempDir()
	remotePath := filepath.Join(tempDir, "remote.git")
	if _, err := git.PlainInit(remotePath, true); err != nil {
		t.Fatalf("init empty remote: %v", err)
	}

	archivePath := filepath.Join(tempDir, "archive.git")
	repo, err := OpenArchive(archivePath, remotePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}

	refs, err := ListRemoteRefs(context.Background(), repo, nil)
	if err != nil {
		t.Fatalf("list remote refs: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no refs for empty remote, got %#v", refs)
	}
}

func TestResolveCommitInfoForCommitAndAnnotatedTag(t *testing.T) {
	repo, commitHash := initCommitMetadataRepo(t)

	info, err := ResolveCommitInfo(repo, commitHash.String())
	if err != nil {
		t.Fatalf("resolve commit info for commit hash: %v", err)
	}
	assertCommitInfo(t, info)

	tagRef, err := repo.Reference(plumbing.NewTagReferenceName("v1.0.0"), true)
	if err != nil {
		t.Fatalf("load tag ref: %v", err)
	}

	tagInfo, err := ResolveCommitInfo(repo, tagRef.Hash().String())
	if err != nil {
		t.Fatalf("resolve commit info for annotated tag: %v", err)
	}
	assertCommitInfo(t, tagInfo)
}

func TestResolveCommitInfoReturnsNilForUnknownObject(t *testing.T) {
	repo, _ := initCommitMetadataRepo(t)

	info, err := ResolveCommitInfo(repo, strings.Repeat("f", 40))
	if err != nil {
		t.Fatalf("resolve commit info for missing hash: %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil commit info for missing hash, got %#v", info)
	}
}

func initCommitMetadataRepo(t *testing.T) (*git.Repository, plumbing.Hash) {
	t.Helper()

	repoPath := filepath.Join(t.TempDir(), "repo")
	repo, err := git.PlainInit(repoPath, false)
	if err != nil {
		t.Fatalf("init repo: %v", err)
	}

	authoredAt := time.Date(2026, time.April, 4, 8, 0, 0, 0, time.UTC)
	committedAt := time.Date(2026, time.April, 4, 9, 30, 0, 0, time.UTC)

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}
	if _, err := worktree.Add("README.md"); err != nil {
		t.Fatalf("stage file: %v", err)
	}

	commitHash, err := worktree.Commit("initial commit\n\nbody", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Alice",
			Email: "alice@example.com",
			When:  authoredAt,
		},
		Committer: &object.Signature{
			Name:  "Bob",
			Email: "bob@example.com",
			When:  committedAt,
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	if _, err := repo.CreateTag("v1.0.0", commitHash, &git.CreateTagOptions{
		Message: "release",
		Tagger: &object.Signature{
			Name:  "Release Bot",
			Email: "release@example.com",
			When:  committedAt.Add(30 * time.Minute),
		},
	}); err != nil {
		t.Fatalf("create annotated tag: %v", err)
	}

	return repo, commitHash
}

func assertCommitInfo(t *testing.T, info *CommitInfo) {
	t.Helper()

	if info == nil {
		t.Fatal("expected commit info, got nil")
	}
	if !info.AuthoredAt.Equal(time.Date(2026, time.April, 4, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected authored at: %v", info.AuthoredAt)
	}
	if !info.CommittedAt.Equal(time.Date(2026, time.April, 4, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("unexpected committed at: %v", info.CommittedAt)
	}
	if info.AuthorName != "Alice" {
		t.Fatalf("unexpected author name: %q", info.AuthorName)
	}
	if info.AuthorEmail != "alice@example.com" {
		t.Fatalf("unexpected author email: %q", info.AuthorEmail)
	}
	if info.Message != "initial commit\n\nbody" {
		t.Fatalf("unexpected message: %q", info.Message)
	}
}
