package middleware

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersonaMiddleware_loadPersona_DefaultWhenNoFiles(t *testing.T) {
	projectRoot := t.TempDir()
	workspaceRoot := filepath.Join(projectRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	m := NewPersonaMiddleware(workspaceRoot, projectRoot, "default")
	got := m.loadPersona()
	if strings.TrimSpace(got) != strings.TrimSpace(DefaultPersona) {
		t.Fatalf("got default mismatch")
	}
}

func TestPersonaMiddleware_loadPersona_LayersUserProjectWorkspaceInOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectRoot := t.TempDir()
	workspaceRoot := filepath.Join(projectRoot, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	userPersonaPath := filepath.Join(home, ".sea", "default", "persona.md")
	projectPersonaPath := filepath.Join(projectRoot, ".sea", "persona.md")
	workspacePersonaPath := filepath.Join(workspaceRoot, "persona.md")

	userPersona := strings.TrimSpace("# User Persona\n- user\n")
	projectPersona := strings.TrimSpace("# Project Persona\n- project\n")
	workspacePersona := strings.TrimSpace("# Workspace Persona\n- workspace\n")

	if err := os.MkdirAll(filepath.Dir(userPersonaPath), 0o755); err != nil {
		t.Fatalf("mkdir user persona dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPersonaPath), 0o755); err != nil {
		t.Fatalf("mkdir project persona dir: %v", err)
	}

	if err := os.WriteFile(userPersonaPath, []byte(userPersona), 0o644); err != nil {
		t.Fatalf("write user persona: %v", err)
	}
	if err := os.WriteFile(projectPersonaPath, []byte(projectPersona), 0o644); err != nil {
		t.Fatalf("write project persona: %v", err)
	}
	if err := os.WriteFile(workspacePersonaPath, []byte(workspacePersona), 0o644); err != nil {
		t.Fatalf("write workspace persona: %v", err)
	}

	m := NewPersonaMiddleware(workspaceRoot, projectRoot, "default")
	got := m.loadPersona()

	userIdx := strings.Index(got, userPersona)
	projectIdx := strings.Index(got, projectPersona)
	workspaceIdx := strings.Index(got, workspacePersona)

	if userIdx == -1 || projectIdx == -1 || workspaceIdx == -1 {
		t.Fatalf("expected all persona layers present")
	}
	if !(userIdx < projectIdx && projectIdx < workspaceIdx) {
		t.Fatalf("expected order user < project < workspace, got idx=%d/%d/%d", userIdx, projectIdx, workspaceIdx)
	}
}
