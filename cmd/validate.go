package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"AgentEngine/pkg/engine/skill"

	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate [skill-path]",
	Short: "Validate skill(s) against the Agent Skills specification",
	Long: `Validate skill(s) against the Agent Skills specification.

If a path is provided, validates the skill at that path.
If no path is provided, validates all skills under ./skills.
`,
	Run: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) {
	target := "./skills"
	if len(args) > 0 {
		target = args[0]
	}

	info, err := os.Stat(target)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var skillFiles []string
	if info.IsDir() {
		// If this dir contains SKILL.md, validate as a single skill dir; otherwise, walk.
		if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err == nil {
			skillFiles = append(skillFiles, filepath.Join(target, "SKILL.md"))
		} else {
			_ = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				if d.IsDir() {
					return nil
				}
				if d.Name() == "SKILL.md" {
					skillFiles = append(skillFiles, path)
				}
				return nil
			})
		}
	} else {
		// File path: accept SKILL.md directly.
		skillFiles = append(skillFiles, target)
	}

	if len(skillFiles) == 0 {
		fmt.Println("No SKILL.md files found.")
		return
	}

	sort.Strings(skillFiles)

	errorsCount := 0
	for _, p := range skillFiles {
		if err := skill.ValidateSkillFile(p); err != nil {
			fmt.Printf("❌ %s\n", skill.ExplainValidationError(p, err))
			errorsCount++
		}
	}

	if errorsCount == 0 {
		fmt.Printf("✅ All %d skill(s) are valid.\n", len(skillFiles))
		return
	}

	fmt.Printf("❌ %d/%d skill(s) have validation errors.\n", errorsCount, len(skillFiles))
	os.Exit(1)
}

