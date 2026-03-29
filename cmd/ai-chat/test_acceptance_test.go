package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAcceptanceTargetRepoFindsGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("creating fake git dir: %v", err)
	}
	nested := filepath.Join(repo, "cmd", "ai-chat")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("creating nested dir: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting cwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restoring cwd: %v", chdirErr)
		}
	}()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("changing cwd: %v", err)
	}

	root, err := resolveAcceptanceTargetRepo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != repo {
		t.Fatalf("expected repo root %q, got %q", repo, root)
	}
}

func TestResolveAcceptanceTargetRepoFailsOutsideRepo(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting cwd: %v", err)
	}
	defer func() {
		if chdirErr := os.Chdir(wd); chdirErr != nil {
			t.Fatalf("restoring cwd: %v", chdirErr)
		}
	}()
	outside := t.TempDir()
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("changing cwd: %v", err)
	}

	if _, err := resolveAcceptanceTargetRepo(); err == nil {
		t.Fatal("expected error when no git root exists")
	}
}
