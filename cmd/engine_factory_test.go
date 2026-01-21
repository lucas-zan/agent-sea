package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"AgentEngine/pkg/engine/skill"
)

func TestDefaultSkillRoots_OrderAndPrecedence(t *testing.T) {
	projectRoot := t.TempDir()
	workspaceRoot := filepath.Join(projectRoot, "workspace")

	home := t.TempDir()
	t.Setenv("HOME", home)

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	origAgentFlag := agentFlag
	agentFlag = "test-agent"
	t.Cleanup(func() { agentFlag = origAgentFlag })

	roots := defaultSkillRoots(workspaceRoot)

	want := []string{
		filepath.Join(projectRoot, ".agent-engine", "skills"),
		filepath.Join(workspaceRoot, ".agent-engine", "skills"),
		filepath.Join(home, ".agent-engine", "test-agent", "skills"),
		filepath.Join(projectRoot, "skills"),
		filepath.Join(codexHome, "skills"),
	}

	if len(roots) != len(want) {
		t.Fatalf("roots length mismatch: got=%d want=%d\nroots=%v", len(roots), len(want), roots)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("roots[%d] mismatch: got=%q want=%q\nroots=%v", i, roots[i], want[i], roots)
		}
	}

	// Verify precedence: earlier roots win on name collisions.
	writeSkill := func(root string, name string, body string) {
		t.Helper()
		skillDir := filepath.Join(root, name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdirAll(%q): %v", skillDir, err)
		}
		skillFile := filepath.Join(skillDir, "SKILL.md")
		content := "---\nname: " + name + "\ndescription: test\nlicense: MIT\ncompatibility: \"\"\nmetadata: {}\nallowed-tools: \"\"\n---\n\n" + body + "\n"
		if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", skillFile, err)
		}
	}

	// Ensure project skill overrides built-in skill.
	writeSkill(filepath.Join(projectRoot, "skills"), "demo-skill", "BUILTIN")
	writeSkill(filepath.Join(projectRoot, ".agent-engine", "skills"), "demo-skill", "PROJECT")

	idx, err := skill.NewDirSkillIndex(roots...)
	if err != nil {
		t.Fatalf("NewDirSkillIndex: %v", err)
	}
	loaded, err := idx.Load("demo-skill")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Content != "PROJECT" {
		t.Fatalf("skill precedence mismatch: got=%q want=%q", loaded.Content, "PROJECT")
	}
}
