package middleware

import (
	"context"
	"strings"
	"testing"

	"AgentEngine/pkg/engine/api"
)

type stubSkillIndex struct {
	sk  *api.Skill
	err error
}

func (s stubSkillIndex) List() []api.SkillMeta { return nil }

func (s stubSkillIndex) Load(name string) (*api.Skill, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.sk == nil {
		return nil, nil
	}
	return s.sk, nil
}

func TestSkillsMiddleware_AppendsExecutionRules(t *testing.T) {
	idx := stubSkillIndex{
		sk: &api.Skill{
			SkillMeta: api.SkillMeta{
				Name: "chapter-write",
			},
			Content: "SKILL BODY",
		},
	}
	mw := NewSkillsMiddleware(idx)

	state := &api.State{
		ActiveSkill:  "chapter-write",
		SystemPrompt: "BASE",
	}
	if err := mw.BeforeTurn(context.Background(), state); err != nil {
		t.Fatalf("BeforeTurn error: %v", err)
	}
	if !strings.Contains(state.SystemPrompt, "--- BEGIN SKILL: chapter-write ---") {
		t.Fatalf("missing skill block: %q", state.SystemPrompt)
	}
	if !strings.Contains(state.SystemPrompt, "--- SKILL EXECUTION RULES ---") {
		t.Fatalf("missing execution rules: %q", state.SystemPrompt)
	}
}
