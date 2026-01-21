package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePathInWorkspace_BlocksDotDotEscape(t *testing.T) {
	root := t.TempDir()
	_, err := resolvePathInWorkspace(root, "../outside.txt")
	if err == nil {
		t.Fatalf("expected error for path escape, got nil")
	}
}

func TestResolvePathInWorkspace_SymlinkSafety(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior varies on Windows")
	}

	root := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	_, err := resolvePathInWorkspace(root, filepath.Join("link", "secret.txt"))
	if err == nil {
		t.Fatalf("expected error for symlink escape, got nil")
	}
}

func TestResolvePathInWorkspace_AllowsSymlinkInsideWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior varies on Windows")
	}

	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "alias")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	got, err := resolvePathInWorkspace(root, filepath.Join("alias", "file.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := filepath.Join(target, "file.txt")
	gotReal, _ := filepath.EvalSymlinks(got)
	wantReal, _ := filepath.EvalSymlinks(want)
	if filepath.Clean(gotReal) != filepath.Clean(wantReal) {
		t.Fatalf("expected %q, got %q", wantReal, gotReal)
	}
}
