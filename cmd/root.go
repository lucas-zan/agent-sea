package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"AgentEngine/pkg/engine/skill"
	"AgentEngine/pkg/logger"

	"github.com/spf13/cobra"
)

// Global flags
var (
	modelFlag       string
	agentFlag       string
	autoApproveFlag bool
	enableToolsFlag bool
)

var rootCmd = &cobra.Command{
	Use:   "agent",
	Short: "Agent Engine - A universal agent framework with DeepAgents CLI compatibility",
	Long: `Agent Engine is a universal agent framework that combines:
- DeepAgents CLI style: Built-in tools, HITL approval, memory system
- Agent loop execution: LLM tool calling with human approval
- Skill-based workflows: standard SKILL.md definitions (Agent Skills specification)

Global Flags:
  --model         LLM model to use (auto-detects provider)
  --agent         Agent configuration name (default: "default")
  --auto-approve  Skip HITL approval prompts
  --enable-tools  Enable built-in tools (ls, shell, etc.)

Smart Invocation:
  If the binary is renamed or symlinked, it auto-detects the command:
  - ./chat          → starts chat mode
  - ./skill-creator → starts chat with that skill active
  - ./agent         → normal CLI mode`,
}

func init() {
	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&modelFlag, "model", "", "LLM model to use (e.g., gpt-4o, claude-sonnet-4-5-20250929)")
	rootCmd.PersistentFlags().StringVar(&agentFlag, "agent", "default", "Agent configuration name")
	rootCmd.PersistentFlags().BoolVar(&autoApproveFlag, "auto-approve", false, "Skip HITL approval prompts")
	rootCmd.PersistentFlags().BoolVar(&enableToolsFlag, "enable-tools", true, "Enable built-in tools (ls, read, write, shell, etc.)")
}

// Execute runs the root command with smart detection
func Execute() {
	// Try to load .env file manually (to support native mode without external deps)
	loadDotEnv()

	// Initialize Logger
	logPath := fmt.Sprintf("workspace/logs/%s.log", time.Now().Format("20060102"))
	logLevelStr := os.Getenv("LOG_LEVEL")
	level := logger.INFO
	switch strings.ToUpper(logLevelStr) {
	case "DEBUG":
		level = logger.DEBUG
	case "WARN":
		level = logger.WARN
	case "ERROR":
		level = logger.ERROR
	}
	if err := logger.Init(logPath, level, "agent-engine"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize logger: %v\n", err)
	}

	logger.Info("System", "Agent Engine Starting", map[string]interface{}{
		"version": "1.0.0",
		"os":      runtime.GOOS,
	})

	// Get the program name (how we were invoked)
	progName := filepath.Base(os.Args[0])

	// Strip common extensions
	progName = strings.TrimSuffix(progName, ".exe")

	// Smart routing based on program name
	switch progName {
	case "agent", "agent-engine", "ae":
		// Default to chat when no subcommand is provided.
		if len(os.Args) == 1 {
			runSmartChat()
			return
		}
		// Normal CLI mode - just run cobra
		if err := rootCmd.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

	case "chat":
		// Direct chat mode
		runSmartChat()

	default:
		// Try to match a skill name
		if tryRunSkillByName(progName) {
			return
		}
		// Fallback to normal CLI
		if err := rootCmd.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

// runSmartChat starts chat mode directly.
func runSmartChat() {
	// Inject "chat" as the command
	os.Args = append([]string{os.Args[0], "chat"}, os.Args[1:]...)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// tryRunSkillByName checks if progName matches a skill and runs it
func tryRunSkillByName(progName string) bool {
	wd, err := os.Getwd()
	if err != nil {
		return false
	}
	if realWD, err := filepath.EvalSymlinks(wd); err == nil {
		wd = realWD
	}

	idx, err := skill.NewDirSkillIndex(defaultSkillRoots(wd)...)
	if err != nil {
		return false
	}
	for _, meta := range idx.List() {
		if meta.Name == progName {
			runSkillDirectly(meta.Name)
			return true
		}
	}
	return false
}

// runSkillDirectly starts a chat session with the skill pre-activated.
func runSkillDirectly(skillName string) {
	fmt.Printf("🚀 Starting skill: %s\n\n", skillName)

	// Start `agent chat` with the skill preset (keeps the entrypoint at api.Engine).
	os.Args = []string{os.Args[0], "chat", "--skill", skillName}
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// loadDotEnv reads .env file and sets environment variables
func loadDotEnv() {
	file, err := os.Open(".env")
	if err != nil {
		return // Ignore if file doesn't exist
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first =
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip quotes if present
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			val = val[1 : len(val)-1]
		}

		// Set if not already set (don't override shell env)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
