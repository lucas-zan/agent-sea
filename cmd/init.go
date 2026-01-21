package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type InitPersonaResult struct {
	ProjectPersonaPath      string
	WorkspacePersonaPath    string
	ProjectPersonaCreated   bool
	WorkspacePersonaCreated bool
}

// InitPersonaFiles creates persona.md templates for the current project/workspace.
//
// It is safe-by-default: it never overwrites existing files.
// - Project persona:  <project>/.sea/persona.md (shareable)
// - Workspace persona: <project>/workspace/persona.md (local overrides; typically gitignored)
func InitPersonaFiles(workspaceRoot string, agentName string) (InitPersonaResult, error) {
	projectRoot := filepath.Dir(workspaceRoot)
	projectPersonaPath := filepath.Join(projectRoot, ".sea", "persona.md")
	workspacePersonaPath := filepath.Join(workspaceRoot, "persona.md")

	res := InitPersonaResult{
		ProjectPersonaPath:   projectPersonaPath,
		WorkspacePersonaPath: workspacePersonaPath,
	}

	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		return res, fmt.Errorf("create workspace dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(projectPersonaPath), 0o755); err != nil {
		return res, fmt.Errorf("create project persona dir: %w", err)
	}

	if created, err := writeFileIfMissing(projectPersonaPath, defaultProjectPersona(agentName)); err != nil {
		return res, err
	} else {
		res.ProjectPersonaCreated = created
	}

	if created, err := writeFileIfMissing(workspacePersonaPath, defaultWorkspacePersona()); err != nil {
		return res, err
	} else {
		res.WorkspacePersonaCreated = created
	}

	return res, nil
}

func writeFileIfMissing(path string, content string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func defaultProjectPersona(agentName string) string {
	if strings.TrimSpace(agentName) == "" {
		agentName = "default"
	}
	return fmt.Sprintf(
		"# Project Persona (%s)\n\n"+
			"## Role\n"+
			"You are a Go engineering assistant for this repository.\n\n"+
			"## Mandatory Workflow\n"+
			"- Follow TDD: write tests from expected behavior first, then implement.\n"+
			"- Before marking work done: run the tests you created and ensure they pass.\n\n"+
			"## Repository Notes\n"+
			"- Source code: cmd/, pkg/\n"+
			"- Built-in skills: skills/\n"+
			"- Runtime artifacts: workspace/ (local state; avoid committing)\n\n"+
			"## Output Style\n"+
			"- Be concise and actionable.\n"+
			"- Prefer commands and file paths (e.g. go test ./..., pkg/engine/...).\n",
		agentName,
	)
}

func defaultWorkspacePersona() string {
	return "# Workspace Persona (Local Overrides)\n\n" +
		"This file is loaded last and can override the project/user persona.\n\n" +
		"## Preferences\n" +
		"- Language: zh-CN\n" +
		"- Ask clarifying questions when requirements are ambiguous.\n"
}
