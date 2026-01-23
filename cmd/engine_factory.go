package cmd

import (
	"os"
	"path/filepath"
	"strconv"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/memory"
	mw "AgentEngine/pkg/engine/middleware"
	"AgentEngine/pkg/engine/policy"
	"AgentEngine/pkg/engine/runtime"
	"AgentEngine/pkg/engine/skill"
	"AgentEngine/pkg/engine/store"
	"AgentEngine/pkg/engine/systool"
	"AgentEngine/pkg/engine/tools"
)

func resolveWorkspaceRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if realWD, err := filepath.EvalSymlinks(wd); err == nil {
		wd = realWD
	}
	// Use workspace/ subdirectory as the working directory for file operations
	workspaceDir := filepath.Join(wd, "workspace")
	// Create if it doesn't exist
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		return "", err
	}
	return workspaceDir, nil
}

func defaultSkillRoots(workspaceRoot string) []string {
	var roots []string

	// workspaceRoot points to workspace/ subdirectory, go up one level for project root
	projectRoot := filepath.Dir(workspaceRoot)

	// Project skills (<project>/.sea/skills). Highest priority.
	roots = append(roots, filepath.Join(projectRoot, ".sea", "skills"))

	// Legacy project skills path (<project>/workspace/.sea/skills).
	roots = append(roots, filepath.Join(workspaceRoot, ".sea", "skills"))

	// Global skills (~/.sea/<agent>/skills).
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".sea", agentFlag, "skills"))
	}

	// Built-in skills shipped with the repo.
	roots = append(roots, filepath.Join(projectRoot, "skills"))

	// Codex skills (optional).
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		roots = append(roots, filepath.Join(codexHome, "skills"))
	} else if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".codex", "skills"))
	}

	return roots
}

func newAPIEngine(workspaceRoot string) (api.Engine, error) {
	sessionStore, err := store.NewFileSessionStore(workspaceRoot)
	if err != nil {
		return nil, err
	}
	planStore, err := store.NewFilePlanStore(workspaceRoot)
	if err != nil {
		return nil, err
	}
	eventLog, err := store.NewJSONLEventLog(workspaceRoot)
	if err != nil {
		return nil, err
	}

	skillIndex, err := skill.NewDirSkillIndex(defaultSkillRoots(workspaceRoot)...)
	if err != nil {
		return nil, err
	}

	mem := memory.NewStructuredManager(workspaceRoot)

	reg := tools.NewRegistry()
	reg.MustRegister(&systool.ListSkillsTool{SkillIndex: skillIndex})
	reg.MustRegister(&systool.ReadSkillTool{SkillIndex: skillIndex})
	reg.MustRegister(&systool.ActivateSkillTool{SkillIndex: skillIndex})
	reg.MustRegister(&systool.ReadTodosTool{PlanStore: planStore})
	reg.MustRegister(&systool.WriteTodosTool{PlanStore: planStore})
	reg.MustRegister(&systool.ReadMemoryTool{Manager: mem})
	reg.MustRegister(&systool.UpdateMemoryTool{Manager: mem})
	reg.MustRegister(&systool.UnderstandIntentTool{})

	if enableToolsFlag {
		for _, t := range tools.DefaultRegistry(workspaceRoot).All() {
			reg.MustRegister(t)
		}
		// run_skill_script needs skill index for path resolution.
		reg.MustRegister(tools.NewRunSkillScriptTool(workspaceRoot, skillIndex))
	}

	var llm runtime.LLM = &runtime.MockLLM{}
	if apiKey := os.Getenv("LLM_API_KEY"); apiKey != "" {
		baseURL := os.Getenv("LLM_BASE_URL")
		model := os.Getenv("LLM_MODEL")
		if modelFlag != "" {
			model = modelFlag
		}
		openai := runtime.NewOpenAILLM(baseURL, apiKey, model)
		llm = openai
	}

	// Read compression settings from environment
	autoCompressThreshold := 50 // Default
	if v := os.Getenv("AUTO_COMPRESS_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			autoCompressThreshold = n
		}
	}
	compressKeepTurns := 3 // Default
	if v := os.Getenv("COMPRESS_KEEP_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			compressKeepTurns = n
		}
	}

	// Filter historical tool messages (default: true for smaller context)
	filterHistoryTools := true
	if v := os.Getenv("FILTER_HISTORY_TOOLS"); v == "false" || v == "0" {
		filterHistoryTools = false
	}

	engine, err := runtime.NewEngine(runtime.EngineConfig{
		LLM:                   llm,
		Tools:                 reg,
		Policy:                policy.NewDefaultPolicy(),
		Middlewares:           []runtime.Middleware{mw.NewPersonaMiddleware(workspaceRoot, filepath.Dir(workspaceRoot), agentFlag), mw.NewBasePromptMiddleware(workspaceRoot), mw.NewSkillsMiddleware(skillIndex), mw.NewMemoryMiddleware(mem), mw.NewPlanningMiddleware(planStore)},
		WorkspaceRoot:         workspaceRoot,
		SkillIndex:            skillIndex,
		SessionStore:          sessionStore,
		PlanStore:             planStore,
		EventLog:              eventLog,
		AutoCompressThreshold: autoCompressThreshold,
		CompressKeepTurns:     compressKeepTurns,
		FilterHistoryTools:    filterHistoryTools,
	})
	if err != nil {
		return nil, err
	}
	return engine, nil
}
