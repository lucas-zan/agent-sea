package runtime

import (
	"testing"

	"AgentEngine/pkg/engine/api"
)

func TestParsePlanSkillTag(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantSkill string
		wantText  string
		wantOK    bool
	}{
		{
			name:      "basic",
			in:        "[skill:chapter-write] 写第3章正文",
			wantSkill: "chapter-write",
			wantText:  "写第3章正文",
			wantOK:    true,
		},
		{
			name:      "spaces",
			in:        "  [skill: chapter-plan]   规划10章  ",
			wantSkill: "chapter-plan",
			wantText:  "规划10章",
			wantOK:    true,
		},
		{
			name:   "no-tag",
			in:     "写第3章正文",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSkill, gotText, ok := parsePlanSkillTag(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("ok mismatch: got=%v want=%v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if gotSkill != tt.wantSkill {
				t.Fatalf("skill mismatch: got=%q want=%q", gotSkill, tt.wantSkill)
			}
			if gotText != tt.wantText {
				t.Fatalf("text mismatch: got=%q want=%q", gotText, tt.wantText)
			}
		})
	}
}

func TestRouteSkill_ExplicitUserOverrideLocks(t *testing.T) {
	skills := []api.SkillMeta{
		{Name: "chapter-plan", Description: `Planning`},
		{Name: "chapter-write", Description: `Write chapters`},
	}

	got, ok := routeSkill(skills, routeSkillInput{
		UserMessage: "skill: chapter-write 先写第3章",
		PlanHint:    "[skill:chapter-plan] 规划10章",
	})
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got.Skill != "chapter-write" || got.Source != "user" || !got.Locked {
		t.Fatalf("unexpected decision: %+v", got)
	}
}

func TestRouteSkill_PlanTagWinsInAutoMode(t *testing.T) {
	skills := []api.SkillMeta{
		{Name: "chapter-plan", Description: `Triggers on "规划10章"`},
		{Name: "chapter-write", Description: `Triggers on "写第X章"`},
	}

	got, ok := routeSkill(skills, routeSkillInput{
		UserMessage: "可以，继续",
		PlanHint:    "[skill:chapter-write] 写第3章",
	})
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got.Skill != "chapter-write" || got.Source != "auto" || got.Locked {
		t.Fatalf("unexpected decision: %+v", got)
	}
}

func TestRouteSkill_AutoByTriggerPicksChapterWrite(t *testing.T) {
	skills := []api.SkillMeta{
		{Name: "chapter-plan", Description: `Triggers on "规划10章", "chapter plan"`},
		{Name: "chapter-write", Description: `Triggers on "写第X章", "创作章节", "write chapter".`},
		{Name: "world-build", Description: `World building`},
	}

	got, ok := routeSkill(skills, routeSkillInput{
		UserMessage: "可以，先写第3章",
	})
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got.Skill != "chapter-write" {
		t.Fatalf("expected chapter-write, got=%q (%+v)", got.Skill, got)
	}
}

func TestRouteSkill_AutoLowConfidenceDoesNothing(t *testing.T) {
	skills := []api.SkillMeta{
		{Name: "alpha", Description: `General`},
		{Name: "beta", Description: `General`},
	}

	_, ok := routeSkill(skills, routeSkillInput{
		UserMessage: "随便聊聊",
	})
	if ok {
		t.Fatalf("expected ok=false")
	}
}

func TestPlanHintFromPlan_RunningFirst(t *testing.T) {
	plan := &api.PlanPayload{
		PlanID: "plan_x",
		Items: []api.PlanItem{
			{ID: 1, Text: "a", Status: api.PlanPending},
			{ID: 2, Text: "[skill:chapter-plan] plan", Status: api.PlanRunning},
			{ID: 3, Text: "c", Status: api.PlanPending},
		},
	}
	if got := planHintFromPlan(plan); got != "[skill:chapter-plan] plan" {
		t.Fatalf("unexpected hint: %q", got)
	}
}

func TestPlanHintFromPlan_PendingWhenNoRunning(t *testing.T) {
	plan := &api.PlanPayload{
		PlanID: "plan_x",
		Items: []api.PlanItem{
			{ID: 1, Text: "a", Status: api.PlanDone},
			{ID: 2, Text: "b", Status: api.PlanPending},
			{ID: 3, Text: "c", Status: api.PlanPending},
		},
	}
	if got := planHintFromPlan(plan); got != "b" {
		t.Fatalf("unexpected hint: %q", got)
	}
}
