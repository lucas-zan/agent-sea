package runtime

import (
	"context"
	"os"
	"strings"

	"AgentEngine/pkg/engine/store"
	"AgentEngine/pkg/logger"
)

func (r *TurnRunner) maybeRouteSkill(ctx context.Context, userMessage string) {
	if r.session == nil || r.cfg.SkillIndex == nil || r.cfg.PlanStore == nil {
		return
	}

	if !autoSkillEnabled(r.session.Metadata) {
		return
	}

	if r.session.Metadata == nil {
		r.session.Metadata = make(map[string]string)
	}

	// Unlock request can be combined with a task, so we treat it as a flag and continue routing.
	unlocked := false
	if isUnlockSkillMessage(userMessage) {
		r.session.Metadata["skill_locked"] = "false"
		r.session.Metadata["skill_source"] = "none"
		r.session.Metadata["skill_last_reason"] = "user_unlock"
		unlocked = true
	}

	skills := r.cfg.SkillIndex.List()
	planHint := r.readPlanHint(ctx)

	// Explicit user override always wins, even if locked.
	if name, ok := parseUserSkillOverride(skills, userMessage); ok {
		r.session.ActiveSkill = name
		r.session.Metadata["skill_locked"] = "true"
		r.session.Metadata["skill_source"] = "user"
		r.session.Metadata["skill_last_reason"] = "explicit_user_override"
		logger.Info("SkillRouter", "Skill locked by user", map[string]interface{}{
			"skill": name,
		})
		return
	}

	locked := strings.EqualFold(r.session.Metadata["skill_locked"], "true")
	if locked && !unlocked {
		return
	}

	decision, ok := routeSkill(skills, routeSkillInput{
		UserMessage: userMessage,
		PlanHint:    planHint,
	})
	if !ok {
		return
	}

	// Apply auto decision (non-locking).
	if decision.Source == "auto" {
		if decision.Skill != "" && decision.Skill != r.session.ActiveSkill {
			prev := r.session.ActiveSkill
			r.session.ActiveSkill = decision.Skill
			r.session.Metadata["skill_source"] = "auto"
			r.session.Metadata["skill_last_reason"] = decision.Reason
			r.session.Metadata["skill_locked"] = "false"
			logger.Info("SkillRouter", "Auto-selected skill", map[string]interface{}{
				"from":     prev,
				"to":       decision.Skill,
				"score":    decision.Score,
				"planHint": truncateForLog(planHint, 120),
			})
		}
	}
}

func (r *TurnRunner) readPlanHint(ctx context.Context) string {
	if r.session == nil || r.cfg.PlanStore == nil {
		return ""
	}
	planID := "plan_" + r.session.SessionID
	plan, err := r.cfg.PlanStore.Get(ctx, planID)
	if err != nil {
		if err == store.ErrNotFound {
			return ""
		}
		return ""
	}
	return planHintFromPlan(plan)
}

func truncateForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func autoSkillEnabled(metadata map[string]string) bool {
	// Global env override.
	if v := strings.TrimSpace(strings.ToLower(strings.TrimSpace(os.Getenv("AUTO_SKILL")))); v != "" {
		if v == "0" || v == "false" || v == "off" {
			return false
		}
	}
	if metadata == nil {
		return true
	}
	if v, ok := metadata["auto_skill"]; ok {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "0" || v == "false" || v == "off" {
			return false
		}
	}
	return true
}

func isUnlockSkillMessage(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	if m == "" {
		return false
	}
	phrases := []string{
		"unlock skill",
		"auto skill",
		"automatic skill",
		"自动选择技能",
		"取消锁定技能",
		"解锁技能",
		"恢复自动技能",
	}
	for _, p := range phrases {
		if strings.Contains(m, strings.ToLower(p)) {
			return true
		}
	}
	return false
}
