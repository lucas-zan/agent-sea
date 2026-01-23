package runtime

import (
	"regexp"
	"sort"
	"strings"

	"AgentEngine/pkg/engine/api"
)

type routeSkillInput struct {
	UserMessage string
	PlanHint    string
}

type routeSkillDecision struct {
	Skill  string
	Source string // user | auto
	Locked bool
	Reason string
	Score  int
}

func routeSkill(skills []api.SkillMeta, in routeSkillInput) (routeSkillDecision, bool) {
	userMsg := strings.TrimSpace(in.UserMessage)
	planHint := strings.TrimSpace(in.PlanHint)

	if userSel, ok := parseUserSkillOverride(skills, userMsg); ok {
		return routeSkillDecision{
			Skill:  userSel,
			Source: "user",
			Locked: true,
			Reason: "explicit_user_override",
			Score:  100,
		}, true
	}

	if planSkill, planText, ok := parsePlanSkillTag(planHint); ok {
		if skillExists(skills, planSkill) {
			return routeSkillDecision{
				Skill:  planSkill,
				Source: "auto",
				Locked: false,
				Reason: "plan_skill_tag:" + strings.TrimSpace(planText),
				Score:  90,
			}, true
		}
	}

	ctx := normalizeForMatch(userMsg + " " + planHint)
	if ctx == "" {
		return routeSkillDecision{}, false
	}

	type scored struct {
		name  string
		score int
	}
	scoredList := make([]scored, 0, len(skills))
	for _, sk := range skills {
		scoredList = append(scoredList, scored{name: sk.Name, score: scoreSkill(sk, ctx)})
	}

	sort.Slice(scoredList, func(i, j int) bool {
		if scoredList[i].score == scoredList[j].score {
			return scoredList[i].name < scoredList[j].name
		}
		return scoredList[i].score > scoredList[j].score
	})

	if len(scoredList) == 0 {
		return routeSkillDecision{}, false
	}

	best := scoredList[0]
	if best.score < 8 {
		return routeSkillDecision{}, false
	}
	if len(scoredList) > 1 && best.score-scoredList[1].score < 2 {
		return routeSkillDecision{}, false
	}

	return routeSkillDecision{
		Skill:  best.name,
		Source: "auto",
		Locked: false,
		Reason: "scored_match",
		Score:  best.score,
	}, true
}

func skillExists(skills []api.SkillMeta, name string) bool {
	for _, sk := range skills {
		if sk.Name == name {
			return true
		}
	}
	return false
}

func parseUserSkillOverride(skills []api.SkillMeta, msg string) (string, bool) {
	if strings.TrimSpace(msg) == "" {
		return "", false
	}

	// Prefer explicit "skill: <name>" style.
	re := regexp.MustCompile(`(?i)\bskill\s*[:=]\s*([a-z0-9]+(?:-[a-z0-9]+)*)\b`)
	if m := re.FindStringSubmatch(msg); len(m) == 2 {
		name := m[1]
		if skillExists(skills, name) {
			return name, true
		}
	}

	// Chinese forms: "使用技能 <name>" / "用 <name> 技能"
	reZH := regexp.MustCompile(`(?i)(?:使用技能|用技能|使用 skill|用 skill)\s*[:：]?\s*([a-z0-9]+(?:-[a-z0-9]+)*)`)
	if m := reZH.FindStringSubmatch(msg); len(m) == 2 {
		name := strings.ToLower(strings.TrimSpace(m[1]))
		if skillExists(skills, name) {
			return name, true
		}
	}

	return "", false
}

func parsePlanSkillTag(text string) (skillName string, remainder string, ok bool) {
	s := strings.TrimSpace(text)
	if !strings.HasPrefix(s, "[") {
		return "", "", false
	}

	// Format: [skill:<name>] <text>
	re := regexp.MustCompile(`(?i)^\[\s*skill\s*:\s*([a-z0-9]+(?:-[a-z0-9]+)*)\s*\]\s*(.*)$`)
	m := re.FindStringSubmatch(s)
	if len(m) != 3 {
		return "", "", false
	}

	return strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2]), true
}

func planHintFromPlan(plan *api.PlanPayload) string {
	if plan == nil || len(plan.Items) == 0 {
		return ""
	}
	for _, it := range plan.Items {
		if it.Status == api.PlanRunning {
			return strings.TrimSpace(it.Text)
		}
	}
	for _, it := range plan.Items {
		if it.Status == api.PlanPending {
			return strings.TrimSpace(it.Text)
		}
	}
	return ""
}

func scoreSkill(sk api.SkillMeta, normalizedContext string) int {
	if sk.Name == "" {
		return 0
	}

	name := strings.ToLower(strings.TrimSpace(sk.Name))
	score := 0

	// Strong match on skill name.
	if strings.Contains(normalizedContext, name) {
		score += 12
	}

	// Skill name tokens.
	for _, tok := range strings.Split(name, "-") {
		tok = strings.TrimSpace(tok)
		if len(tok) < 3 {
			continue
		}
		if strings.Contains(normalizedContext, tok) {
			score += 2
		}
	}

	// Extract trigger phrases from quoted strings in description.
	for _, trig := range extractQuotedStrings(sk.Description) {
		if trig == "" {
			continue
		}
		if triggerMatches(trig, normalizedContext) {
			score += 15
			continue
		}
		trigNorm := normalizeForMatch(trig)
		if trigNorm != "" && strings.Contains(normalizedContext, trigNorm) {
			score += 15
			continue
		}
	}

	// Weak match on description keywords (ASCII only).
	for _, w := range asciiWords(sk.Description) {
		if len(w) < 4 {
			continue
		}
		if strings.Contains(normalizedContext, w) {
			score++
		}
	}

	return score
}

func triggerMatches(trigger string, normalizedContext string) bool {
	t := strings.TrimSpace(trigger)
	if t == "" {
		return false
	}

	// Support "第X章" placeholder.
	if strings.Contains(t, "第X章") || strings.Contains(t, "第x章") {
		reStr := regexp.QuoteMeta(t)
		reStr = strings.ReplaceAll(reStr, "第X章", "第[0-9]+章")
		reStr = strings.ReplaceAll(reStr, "第x章", "第[0-9]+章")
		reStr = strings.ReplaceAll(reStr, "X", "[0-9]+")
		reStr = strings.ReplaceAll(reStr, "x", "[0-9]+")
		re, err := regexp.Compile(reStr)
		if err == nil && re.FindStringIndex(normalizedContext) != nil {
			return true
		}
	}

	return false
}

func extractQuotedStrings(s string) []string {
	re := regexp.MustCompile("\"([^\"]+)\"")
	matches := re.FindAllStringSubmatch(s, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) != 2 {
			continue
		}
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func asciiWords(s string) []string {
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}

func normalizeForMatch(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	return strings.Join(strings.Fields(s), " ")
}
