package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AgentEngine/pkg/engine/api"
)

type testSkillIndex struct {
	meta api.SkillMeta
}

func (t testSkillIndex) Get(name string) (api.SkillMeta, bool) {
	if name != t.meta.Name {
		return api.SkillMeta{}, false
	}
	return t.meta, true
}

func TestRunSkillScriptTool_SetsAgentWorkspaceAlias(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "skill")
	scriptsDir := filepath.Join(skillDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	workspaceDir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "print_env.sh")
	script := "#!/bin/sh\n" +
		"echo \"PROJECT_ROOT=$PROJECT_ROOT\"\n" +
		"echo \"WORKSPACE_ROOT=$WORKSPACE_ROOT\"\n" +
		"echo \"AGENT_WORKSPACE=$AGENT_WORKSPACE\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	tool := NewRunSkillScriptTool(tmp, testSkillIndex{
		meta: api.SkillMeta{Name: "test-skill", Path: skillDir},
	})

	res, err := tool.Execute(context.Background(), api.Args{
		"_active_skill": "test-skill",
		"script":        "print_env.sh",
	})
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if res.Status != "success" {
		t.Fatalf("unexpected status: %s content=%q", res.Status, res.Content)
	}
	if !strings.Contains(res.Content, "PROJECT_ROOT="+tmp) {
		t.Fatalf("missing PROJECT_ROOT: %q", res.Content)
	}
	if !strings.Contains(res.Content, "WORKSPACE_ROOT="+workspaceDir) {
		t.Fatalf("missing WORKSPACE_ROOT: %q", res.Content)
	}
	if !strings.Contains(res.Content, "AGENT_WORKSPACE="+workspaceDir) {
		t.Fatalf("missing AGENT_WORKSPACE alias: %q", res.Content)
	}
}
