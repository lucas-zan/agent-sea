package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"AgentEngine/cmd/ui"
	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/runtime"

	"github.com/spf13/cobra"
)

var (
	listSessionsFlag bool
	activeSkillFlag  string
	approvalModeFlag string
	emitThinkingFlag bool
)

var chatCmd = &cobra.Command{
	Use:   "chat [session-id]",
	Short: "Start an interactive chat session",
	Run:   runChat,
}

func init() {
	chatCmd.Flags().BoolVarP(&listSessionsFlag, "list", "l", false, "List all sessions")
	chatCmd.Flags().StringVar(&activeSkillFlag, "skill", "", "Set initial active skill for a new session")
	chatCmd.Flags().StringVar(&approvalModeFlag, "approval-mode", "", "suggest | auto | full-auto (default: auto)")
	chatCmd.Flags().BoolVar(&emitThinkingFlag, "thinking", false, "Emit thinking events (UI/debug)")
	rootCmd.AddCommand(chatCmd)
}

func runChat(cmd *cobra.Command, args []string) {
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

	ctx := context.Background()

	if listSessionsFlag {
		listSessions(ctx, eng)
		return
	}

	sessionID := ""
	if len(args) > 0 {
		sessionID = args[0]
		if _, err := eng.GetSession(ctx, sessionID); err != nil {
			fmt.Printf("Session '%s' not found, creating a new session...\n", sessionID)
			sessionID = ""
		}
	}

	if sessionID == "" {
		opts := api.StartOptions{
			ApprovalMode: resolveApprovalMode(),
			EmitThinking: emitThinkingFlag,
			ActiveSkill:  activeSkillFlag,
		}
		id, err := eng.StartSession(ctx, opts)
		if err != nil {
			fmt.Printf("Error starting session: %v\n", err)
			return
		}
		sessionID = id
	}

	printChatBanner(sessionID)

	approver := ui.NewCLIApprover()
	approval := &approvalState{}

	// Initialize history manager
	historyMgr, err := NewHistoryManager(workspaceRoot)
	if err != nil {
		fmt.Printf("Warning: Failed to initialize history: %v\n", err)
	}

	// Load history
	var inputHistory []string
	if historyMgr != nil {
		if stored, err := historyMgr.Load(); err == nil {
			inputHistory = stored
		}
	}

	for {
		in, err := ui.ReadInputWithHistory("\n💬 You: ", inputHistory)
		if err != nil {
			fmt.Printf("Input error: %v\n", err)
			return
		}
		if in.Cancelled {
			return
		}

		text := strings.TrimSpace(in.Value)
		if text == "" {
			continue
		}

		// Update memory history and persist
		if len(inputHistory) == 0 || inputHistory[len(inputHistory)-1] != text {
			inputHistory = append(inputHistory, text)
			if historyMgr != nil {
				// Fire and forget persistence
				go func(t string) {
					_ = historyMgr.Append(t)
				}(text)
			}
		}

		switch strings.ToLower(text) {
		case "/quit", "/exit", "/q":
			fmt.Println("\nGoodbye.")
			return
		case "/help", "/?":
			fmt.Println("\nCommands:")
			fmt.Println("  /init      Create persona templates for this project/workspace")
			fmt.Println("  /compress  Compress conversation history (keep last 3 turns)")
			fmt.Println("  /help      Show help")
			fmt.Println("  /quit      Exit")
			continue
		case "/init":
			fmt.Println("\n🧩 Initializing persona templates...")
			res, err := InitPersonaFiles(workspaceRoot, agentFlag)
			if err != nil {
				fmt.Printf("❌ Init failed: %v\n", err)
				continue
			}
			if res.ProjectPersonaCreated {
				fmt.Printf("✅ Created: %s\n", res.ProjectPersonaPath)
			} else {
				fmt.Printf("↩︎ Exists:  %s\n", res.ProjectPersonaPath)
			}
			if res.WorkspacePersonaCreated {
				fmt.Printf("✅ Created: %s\n", res.WorkspacePersonaPath)
			} else {
				fmt.Printf("↩︎ Exists:  %s\n", res.WorkspacePersonaPath)
			}
			fmt.Println("Tip: restart the session to ensure the new persona is loaded.")
			continue
		case "/compress":
			fmt.Println("\n🔄 Compressing conversation history...")
			// Type assert to get the runtime engine
			if runtimeEng, ok := eng.(*runtime.Engine); ok {
				result, err := runtimeEng.CompressSession(ctx, sessionID, 3)
				if err != nil {
					fmt.Printf("❌ Compression failed: %v\n", err)
				} else {
					fmt.Printf("✅ Compression complete:\n")
					fmt.Printf("   Messages removed: %d\n", result.MessagesRemoved)
					fmt.Printf("   Messages kept: %d\n", result.MessagesKept)
					fmt.Printf("   Summary length: %d chars\n", result.SummaryLength)
				}
			} else {
				fmt.Println("❌ Compression not available with mock engine")
			}
			continue
		}

		if err := runTurnWithApprovals(ctx, eng, sessionID, text, approver, approval); err != nil {
			fmt.Printf("\n❌ Error: %v\n", err)
		}
	}
}

func resolveApprovalMode() api.ApprovalMode {
	if autoApproveFlag {
		return api.ModeFullAuto
	}
	switch strings.ToLower(strings.TrimSpace(approvalModeFlag)) {
	case "suggest":
		return api.ModeSuggest
	case "full-auto", "fullauto":
		return api.ModeFullAuto
	case "", "auto":
		return api.ModeAuto
	default:
		return api.ModeAuto
	}
}

func listSessions(ctx context.Context, eng api.Engine) {
	sessions, err := eng.ListSessions(ctx)
	if err != nil {
		fmt.Printf("Error listing sessions: %v\n", err)
		return
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return
	}

	sort.Slice(sessions, func(i, j int) bool { return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt) })

	fmt.Println("\n📂 Sessions:")
	for _, s := range sessions {
		skillInfo := ""
		if s.ActiveSkill != "" {
			skillInfo = " [" + s.ActiveSkill + "]"
		}
		fmt.Printf("  %s - %d messages%s - %s\n", s.SessionID, s.MessageCount, skillInfo, s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	fmt.Println("\nResume with: agent chat <session-id>")
}

func printChatBanner(sessionID string) {
	fmt.Println()
	fmt.Println("╔═══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    🤖 Agent Engine Chat                       ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Session: %-52s ║\n", sessionID)
	fmt.Println("╠═══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Commands:                                                    ║")
	fmt.Println("║    /help      Show all commands                               ║")
	fmt.Println("║    /compress  Compress history when context is too long       ║")
	fmt.Println("║    /init      Create project-specific persona templates       ║")
	fmt.Println("║    /quit      Exit session                                    ║")
	fmt.Println("╠═══════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Tips:                                                        ║")
	fmt.Println("║    • Ctrl+J to insert newline, Enter to send                  ║")
	fmt.Println("║    • Create persona.md for project-specific AI behavior       ║")
	fmt.Println("║    • Use /compress if responses slow down (context too long)  ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════════╝")
}
