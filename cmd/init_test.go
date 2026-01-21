package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitPersonaFiles_CreatesProjectAndWorkspacePersona(t *testing.T) {
	projectRoot := t.TempDir()
	workspaceRoot := filepath.Join(projectRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	res, err := InitPersonaFiles(workspaceRoot, "default")
	if err != nil {
		t.Fatalf("InitPersonaFiles: %v", err)
	}
	if !res.ProjectPersonaCreated || !res.WorkspacePersonaCreated {
		t.Fatalf("expected both personas created, got %+v", res)
	}

	projectPersonaPath := filepath.Join(projectRoot, ".agent-engine", "persona.md")
	workspacePersonaPath := filepath.Join(workspaceRoot, "persona.md")

	if _, err := os.Stat(projectPersonaPath); err != nil {
		t.Fatalf("expected project persona exists: %v", err)
	}
	if _, err := os.Stat(workspacePersonaPath); err != nil {
		t.Fatalf("expected workspace persona exists: %v", err)
	}
}

func TestInitPersonaFiles_DoesNotOverwriteExistingFiles(t *testing.T) {
	projectRoot := t.TempDir()
	workspaceRoot := filepath.Join(projectRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	projectPersonaPath := filepath.Join(projectRoot, ".agent-engine", "persona.md")
	if err := os.MkdirAll(filepath.Dir(projectPersonaPath), 0o755); err != nil {
		t.Fatalf("mkdir project persona dir: %v", err)
	}
	workspacePersonaPath := filepath.Join(workspaceRoot, "persona.md")

	wantProject := "project-persona-custom"
	wantWorkspace := "workspace-persona-custom"

	if err := os.WriteFile(projectPersonaPath, []byte(wantProject), 0o644); err != nil {
		t.Fatalf("write project persona: %v", err)
	}
	if err := os.WriteFile(workspacePersonaPath, []byte(wantWorkspace), 0o644); err != nil {
		t.Fatalf("write workspace persona: %v", err)
	}

	res, err := InitPersonaFiles(workspaceRoot, "default")
	if err != nil {
		t.Fatalf("InitPersonaFiles: %v", err)
	}
	if res.ProjectPersonaCreated || res.WorkspacePersonaCreated {
		t.Fatalf("expected no creations, got %+v", res)
	}

	gotProject, err := os.ReadFile(projectPersonaPath)
	if err != nil {
		t.Fatalf("read project persona: %v", err)
	}
	gotWorkspace, err := os.ReadFile(workspacePersonaPath)
	if err != nil {
		t.Fatalf("read workspace persona: %v", err)
	}

	if string(gotProject) != wantProject {
		t.Fatalf("project persona overwritten: got %q want %q", string(gotProject), wantProject)
	}
	if string(gotWorkspace) != wantWorkspace {
		t.Fatalf("workspace persona overwritten: got %q want %q", string(gotWorkspace), wantWorkspace)
	}
}
