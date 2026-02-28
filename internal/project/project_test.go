package project

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetect_HashIsStable(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	info1, err := Detect(ctx, dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	info2, err := Detect(ctx, dir)
	if err != nil {
		t.Fatalf("Detect second call: %v", err)
	}

	if info1.Hash != info2.Hash {
		t.Errorf("Hash is not stable: first=%q second=%q", info1.Hash, info2.Hash)
	}
}

func TestDetect_DifferentPathsDifferentHashes(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	ctx := context.Background()

	info1, err := Detect(ctx, dir1)
	if err != nil {
		t.Fatalf("Detect dir1: %v", err)
	}
	info2, err := Detect(ctx, dir2)
	if err != nil {
		t.Fatalf("Detect dir2: %v", err)
	}

	if info1.Hash == info2.Hash {
		t.Errorf("expected different hashes for different paths, both got %q", info1.Hash)
	}
}

func TestDetect_NameFromDirectoryBasename(t *testing.T) {
	parent := t.TempDir()
	namedDir := filepath.Join(parent, "myproject")
	if err := os.MkdirAll(namedDir, 0755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	info, err := Detect(ctx, namedDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if info.Name != "myproject" {
		t.Errorf("Name = %q, want %q", info.Name, "myproject")
	}
}

func TestDetect_HashHas16HexChars(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	info, err := Detect(ctx, dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	if len(info.Hash) != 16 {
		t.Errorf("Hash length = %d, want 16 hex chars; hash = %q", len(info.Hash), info.Hash)
	}
	for _, c := range info.Hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Hash contains non-hex character %q; hash = %q", c, info.Hash)
			break
		}
	}
}

func TestDetect_EmptyDirParam_UsesWorkingDirectory(t *testing.T) {
	ctx := context.Background()
	info, err := Detect(ctx, "")
	if err != nil {
		t.Fatalf("Detect(\"\") unexpected error: %v", err)
	}
	if info.Name == "" {
		t.Error("Name should not be empty when dir is empty string")
	}
	if info.Hash == "" {
		t.Error("Hash should not be empty when dir is empty string")
	}
}
