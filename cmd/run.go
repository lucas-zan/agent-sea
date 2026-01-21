package cmd

import (
	"context"
	"fmt"
	"strings"

	"AgentEngine/cmd/ui"
	"AgentEngine/pkg/engine/api"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <skill-name> [--arg key=value ...]",
	Short: "Execute a skill by name (non-interactive wrapper around chat)",
	Args:  cobra.ExactArgs(1),
	Run:   runSkill,
}

func init() {
	runCmd.Flags().StringArrayP("arg", "a", []string{}, "Skill arguments (key=value)")
	rootCmd.AddCommand(runCmd)
}

func runSkill(cmd *cobra.Command, args []string) {
	skillName := args[0]

	workspaceRoot, err := resolveWorkspaceRoot()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	eng, err := newAPIEngine(workspaceRoot)
	if err != nil {
		fmt.Printf("Error initializing engine: %v\n", err)
		return
	}

	argFlags, _ := cmd.Flags().GetStringArray("arg")
	skillArgs := parseArgs(argFlags)

	ctx := context.Background()
	sessionID, err := eng.StartSession(ctx, api.StartOptions{
		ApprovalMode: resolveApprovalMode(),
		EmitThinking: emitThinkingFlag,
		ActiveSkill:  skillName,
	})
	if err != nil {
		fmt.Printf("Error starting session: %v\n", err)
		return
	}

	approver := ui.NewCLIApprover()
	approval := &approvalState{}
	userMessage := buildRunInput(skillArgs)

	fmt.Printf("Session=%s Skill=%s\n", sessionID, skillName)
	if err := runTurnWithApprovals(ctx, eng, sessionID, userMessage, approver, approval); err != nil {
		fmt.Printf("\n❌ Error: %v\n", err)
	}
}

func parseArgs(flags []string) map[string]string {
	args := make(map[string]string)
	for _, flag := range flags {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) == 2 {
			args[parts[0]] = parts[1]
		}
	}
	return args
}

func buildRunInput(args map[string]string) string {
	if len(args) == 0 {
		return "Execute this skill. Ask follow-up questions if required."
	}

	var b strings.Builder
	b.WriteString("Execute this skill with the following inputs:\n")
	for k, v := range args {
		b.WriteString("- ")
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

