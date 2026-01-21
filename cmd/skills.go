package cmd

import (
	"fmt"
	"sort"
	"strings"

	"AgentEngine/pkg/engine/skill"

	"github.com/spf13/cobra"
)

var skillsListCmd = &cobra.Command{
	Use:   "skills",
	Short: "List all available skills",
	Run:   runSkillsList,
}

var skillInfoCmd = &cobra.Command{
	Use:   "info <skill-name>",
	Short: "Show detailed information about a skill",
	Args:  cobra.ExactArgs(1),
	Run:   runSkillsInfo,
}

func init() {
	rootCmd.AddCommand(skillsListCmd)
	rootCmd.AddCommand(skillInfoCmd)
}

func runSkillsList(cmd *cobra.Command, args []string) {
	workspaceRoot, err := resolveWorkspaceRoot()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	idx, err := skill.NewDirSkillIndex(defaultSkillRoots(workspaceRoot)...)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	list := idx.List()
	if len(list) == 0 {
		fmt.Println("No skills found.")
		return
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	fmt.Println("\n📋 Available Skills:")
	for _, s := range list {
		desc := strings.TrimSpace(s.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("  - %s: %s\n", s.Name, truncateSkillStr(desc, 80))
	}
}

func runSkillsInfo(cmd *cobra.Command, args []string) {
	workspaceRoot, err := resolveWorkspaceRoot()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	idx, err := skill.NewDirSkillIndex(defaultSkillRoots(workspaceRoot)...)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	name := args[0]
	sk, err := idx.Load(name)
	if err != nil {
		fmt.Printf("❌ Skill '%s' not found\n", name)
		return
	}

	fmt.Printf("\n📋 Skill: %s\n\n", sk.Name)
	fmt.Printf("Description: %s\n", sk.Description)
	fmt.Printf("Path: %s\n", sk.Path)
	if sk.License != "" {
		fmt.Printf("License: %s\n", sk.License)
	}
	if sk.Compatibility != "" {
		fmt.Printf("Compatibility: %s\n", sk.Compatibility)
	}
	if len(sk.AllowedTools) > 0 {
		fmt.Printf("Allowed tools: %s\n", strings.Join(sk.AllowedTools, " "))
	}

	if len(sk.Metadata) > 0 {
		fmt.Println("\nMetadata:")
		keys := make([]string, 0, len(sk.Metadata))
		for k := range sk.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  - %s: %s\n", k, sk.Metadata[k])
		}
	}

	if len(sk.Scripts) > 0 {
		fmt.Println("\nScripts:")
		for _, s := range sk.Scripts {
			fmt.Printf("  - %s\n", s)
		}
	}

	if len(sk.References) > 0 {
		fmt.Println("\nReferences:")
		for _, r := range sk.References {
			fmt.Printf("  - %s\n", r)
		}
	}

	if len(sk.Assets) > 0 {
		fmt.Println("\nAssets:")
		for _, a := range sk.Assets {
			fmt.Printf("  - %s\n", a)
		}
	}

	if sk.Content != "" {
		fmt.Println("\n--- Content Preview ---")
		lines := strings.Split(sk.Content, "\n")
		if len(lines) > 20 {
			for _, line := range lines[:20] {
				fmt.Println(line)
			}
			fmt.Printf("\n... (%d more lines)\n", len(lines)-20)
		} else {
			fmt.Println(sk.Content)
		}
	}
}

func truncateSkillStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

