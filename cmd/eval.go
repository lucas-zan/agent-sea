package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"AgentEngine/cmd/ui"
	"AgentEngine/pkg/engine/api"

	"github.com/spf13/cobra"
)

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Run evaluation scenarios and write a report",
	Run:   runEval,
}

var (
	evalProjectFlag string
	evalOutlineFlag string
	evalOutDirFlag  string
)

func init() {
	evalCmd.Flags().StringVar(&evalProjectFlag, "project", "诡世禁忌", "Novel project name under workspace/novel/")
	evalCmd.Flags().StringVar(&evalOutlineFlag, "outline", "", "Path to outline markdown (default: workspace/novel/<project>/outline.md)")
	evalCmd.Flags().StringVar(&evalOutDirFlag, "out", "workspace/eval", "Directory to write evaluation outputs")
	rootCmd.AddCommand(evalCmd)
}

type evalReport struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Scenario  string    `json:"scenario"`
	SessionID string    `json:"session_id"`

	Project string `json:"project,omitempty"`
	Outline string `json:"outline,omitempty"`

	Checks []evalCheck `json:"checks"`
	Files  []fileDiff  `json:"files,omitempty"`
	Events eventStats  `json:"events,omitempty"`
}

type evalCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type fileDiff struct {
	Path      string `json:"path"`
	BeforeSHA string `json:"before_sha,omitempty"`
	AfterSHA  string `json:"after_sha,omitempty"`
	Changed   bool   `json:"changed"`
	Bytes     int    `json:"bytes,omitempty"`
}

type eventStats struct {
	ToolCalls     int `json:"tool_calls"`
	Approvals     int `json:"approvals"`
	PlanSnapshots int `json:"plan_snapshots"`
	Errors        int `json:"errors"`
	Deltas        int `json:"deltas"`
	Done          int `json:"done"`
}

func runEval(cmd *cobra.Command, args []string) {
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

	outline := evalOutlineFlag
	if outline == "" {
		outline = filepath.Join("workspace", "novel", evalProjectFlag, "outline.md")
	}

	ctx := context.Background()
	sessionID, err := eng.StartSession(ctx, api.StartOptions{
		ApprovalMode: resolveApprovalMode(),
		ActiveSkill:  "world-build",
	})
	if err != nil {
		fmt.Printf("Error starting session: %v\n", err)
		return
	}

	report := evalReport{
		ID:        "eval-" + sessionID,
		StartedAt: time.Now(),
		Scenario:  "novel-world-build",
		SessionID: sessionID,
		Project:   evalProjectFlag,
		Outline:   outline,
	}

	projectDir := filepath.Join("workspace", "novel", evalProjectFlag, "world")
	targetFiles := []string{
		filepath.Join(projectDir, "summary.md"),
		filepath.Join(projectDir, "power_system.md"),
		filepath.Join(projectDir, "geography.md"),
		filepath.Join(projectDir, "factions.md"),
		filepath.Join(projectDir, "history.md"),
		filepath.Join(projectDir, "culture.md"),
		filepath.Join(projectDir, "rules.md"),
	}
	before := snapshotFiles(workspaceRoot, targetFiles)

	approver := ui.NewCLIApprover()
	approval := &approvalState{}

	prompt := fmt.Sprintf(
		"Project: %s\nAspect: all\n\nBased on the outline at %s, build or refine the world setting. Read any existing world files first, ensure internal consistency, and update world files under workspace/novel/%s/world/.\n",
		evalProjectFlag,
		outline,
		evalProjectFlag,
	)

	err = runTurnWithApprovals(ctx, eng, sessionID, prompt, approver, approval)
	report.EndedAt = time.Now()

	if err != nil {
		report.Checks = append(report.Checks, evalCheck{Name: "scenario_completed", Passed: false, Detail: err.Error()})
	} else {
		report.Checks = append(report.Checks, evalCheck{Name: "scenario_completed", Passed: true})
	}

	after := snapshotFiles(workspaceRoot, targetFiles)
	report.Files = diffSnapshots(before, after)

	summaryPath := filepath.Join("workspace", "novel", evalProjectFlag, "world", "summary.md")
	if b, ok := after[summaryPath]; ok && b.Bytes > 0 {
		report.Checks = append(report.Checks, evalCheck{Name: "world_summary_exists", Passed: true})
	} else {
		report.Checks = append(report.Checks, evalCheck{Name: "world_summary_exists", Passed: false, Detail: summaryPath})
	}

	report.Events = summarizeEvents(filepath.Join(workspaceRoot, "workspace", "events", sessionID+".jsonl"))

	if err := writeEvalReport(workspaceRoot, evalOutDirFlag, &report); err != nil {
		fmt.Printf("Failed to write report: %v\n", err)
		return
	}

	fmt.Printf("✅ Eval complete. Session=%s Report=%s\n", report.SessionID, filepath.ToSlash(filepath.Join(evalOutDirFlag, report.ID+".json")))
}

type fileSnap struct {
	SHA   string
	Bytes int
}

func snapshotFiles(workspaceRoot string, paths []string) map[string]fileSnap {
	out := make(map[string]fileSnap, len(paths))
	for _, rel := range paths {
		abs := filepath.Join(workspaceRoot, rel)
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		out[rel] = fileSnap{SHA: hex.EncodeToString(sum[:]), Bytes: len(data)}
	}
	return out
}

func diffSnapshots(before, after map[string]fileSnap) []fileDiff {
	seen := make(map[string]bool, len(before)+len(after))
	var out []fileDiff

	for path := range before {
		seen[path] = true
	}
	for path := range after {
		seen[path] = true
	}

	for path := range seen {
		b, bok := before[path]
		a, aok := after[path]
		fd := fileDiff{Path: path}
		if bok {
			fd.BeforeSHA = b.SHA
		}
		if aok {
			fd.AfterSHA = a.SHA
			fd.Bytes = a.Bytes
		}
		fd.Changed = !bok || !aok || b.SHA != a.SHA
		out = append(out, fd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func writeEvalReport(workspaceRoot, outDir string, report *evalReport) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}
	abs := filepath.Join(workspaceRoot, outDir)
	if err := os.MkdirAll(abs, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(abs, report.ID+".json"), data, 0644)
}

func summarizeEvents(path string) eventStats {
	raw, err := os.ReadFile(path)
	if err != nil {
		return eventStats{}
	}
	lines := strings.Split(string(raw), "\n")
	var stats eventStats
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		switch e.Type {
		case string(api.EventDelta):
			stats.Deltas++
		case string(api.EventToolCall):
			stats.ToolCalls++
		case string(api.EventApproval):
			stats.Approvals++
		case string(api.EventPlan):
			stats.PlanSnapshots++
		case string(api.EventError):
			stats.Errors++
		case string(api.EventDone):
			stats.Done++
		}
	}
	return stats
}

