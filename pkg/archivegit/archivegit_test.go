package archivegit

import (
	"slices"
	"strings"
	"testing"
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
