package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new agent configuration",
	Args:  cobra.ExactArgs(1),
	Run:   runAgentsCreate,
}

var agentsListCmd = &cobra.Command{
	Use:   "agents",
	Short: "List all available agents",
	Run:   runAgentsList,
}

var resetCmd = &cobra.Command{
	Use:   "reset <name>",
	Short: "Reset an agent to default configuration",
	Args:  cobra.ExactArgs(1),
	Run:   runAgentsReset,
}

func init() {
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(agentsListCmd)
	rootCmd.AddCommand(resetCmd)
}

func runAgentsList(cmd *cobra.Command, args []string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	agentsDir := filepath.Join(homeDir, ".sea")

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("📁 No agents found.")
			fmt.Println("   Agents will be created in ~/.sea/ when you first use them.")
			return
		}
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	fmt.Println("\n🤖 Available Agents:")
	fmt.Println()

	found := false
	for _, entry := range entries {
		if entry.IsDir() {
			agentDir := filepath.Join(agentsDir, entry.Name())
			personaMD := filepath.Join(agentDir, "persona.md")

			status := "✓"
			if _, err := os.Stat(personaMD); os.IsNotExist(err) {
				status = "○ (no persona.md)"
			}

			fmt.Printf("  %s %s\n", status, entry.Name())
			fmt.Printf("    %s\n\n", agentDir)
			found = true
		}
	}

	if !found {
		fmt.Println("   No agents found.")
	}
}

func runAgentsCreate(cmd *cobra.Command, args []string) {
	name := args[0]

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	agentDir := filepath.Join(homeDir, ".sea", name)

	// Check if already exists
	if _, err := os.Stat(agentDir); err == nil {
		fmt.Printf("❌ Agent '%s' already exists at %s\n", name, agentDir)
		return
	}

	// Create directory
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		fmt.Printf("❌ Error creating directory: %v\n", err)
		return
	}

	// Create default persona.md
	personaMD := filepath.Join(agentDir, "persona.md")
	defaultContent := fmt.Sprintf(`# Agent: %s

## Personality
You are a helpful AI assistant.

## Preferences
- Be concise and clear
- Ask for clarification when needed
- Explain your reasoning

## Remember
This file stores your preferences and memories.
Update it as you learn about the user's needs.
`, name)

	if err := os.WriteFile(personaMD, []byte(defaultContent), 0644); err != nil {
		fmt.Printf("❌ Error creating persona.md: %v\n", err)
		return
	}

	fmt.Printf("✅ Created agent '%s' at %s\n", name, agentDir)
	fmt.Printf("   Edit %s to customize your agent\n", personaMD)
}

func runAgentsReset(cmd *cobra.Command, args []string) {
	name := args[0]

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("❌ Error: %v\n", err)
		return
	}

	agentDir := filepath.Join(homeDir, ".sea", name)

	// Check if exists
	if _, err := os.Stat(agentDir); os.IsNotExist(err) {
		fmt.Printf("❌ Agent '%s' does not exist\n", name)
		return
	}

	// Remove and recreate
	if err := os.RemoveAll(agentDir); err != nil {
		fmt.Printf("❌ Error removing directory: %v\n", err)
		return
	}

	// Recreate using create logic
	runAgentsCreate(cmd, args)
}
