package skill

import (
	"fmt"
	"os"
	"path/filepath"
)

// ValidateSkillFile validates a SKILL.md file against the strict Agent Skills frontmatter constraints.
func ValidateSkillFile(skillFile string) error {
	raw, err := os.ReadFile(skillFile)
	if err != nil {
		return err
	}
	_, _, _, err = parseSkillMarkdown(skillFile, string(raw))
	return err
}

// ValidateSkillDir validates a skill directory that contains SKILL.md.
func ValidateSkillDir(skillDir string) error {
	return ValidateSkillFile(filepath.Join(skillDir, "SKILL.md"))
}

// ExplainValidationError formats a validation error for CLI display.
func ExplainValidationError(skillPath string, err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: %v", skillPath, err)
}

