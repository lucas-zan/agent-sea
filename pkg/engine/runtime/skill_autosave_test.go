package runtime

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"AgentEngine/pkg/engine/api"
	"AgentEngine/pkg/engine/policy"
	"AgentEngine/pkg/engine/store"
	"AgentEngine/pkg/engine/tools"
)

type staticLLM struct {
	out string
}

func (s staticLLM) Stream(ctx context.Context, req LLMRequest) (LLMStream, error) {
	return &staticStream{content: s.out}, nil
}

type staticStream struct {
	content string
	sent    bool
}

func (s *staticStream) Recv(ctx context.Context) (LLMChunk, error) {
	if s.sent {
		return LLMChunk{}, io.EOF
	}
	s.sent = true
	return LLMChunk{Delta: s.content, FinishReason: "stop"}, nil
}

func (s *staticStream) Close() error { return nil }

type stubSkillIndex struct {
	sk *api.Skill
}

func (s stubSkillIndex) List() []api.SkillMeta { return nil }
func (s stubSkillIndex) Load(name string) (*api.Skill, error) {
	if s.sk == nil || s.sk.Name != name {
		return nil, io.EOF
	}
	return s.sk, nil
}

func drainEvents(t *testing.T, stream api.EventStream) {
	t.Helper()
	ctx := context.Background()
	for {
		_, err := stream.Recv(ctx)
		if err != nil {
			return
		}
	}
}

func TestTurnRunner_AutoSaveNovelChapter_FullAutoCreatesFile(t *testing.T) {
	ws := t.TempDir()

	project := "demo"
	if err := os.MkdirAll(filepath.Join(ws, "novel", project, "volumes", "v1"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "novel"), 0o755); err != nil {
		t.Fatalf("mkdir novel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "novel", ".current"), []byte(project), 0o644); err != nil {
		t.Fatalf("write .current: %v", err)
	}

	out := "# 第004章 逃亡者的直觉\n\n" + strings.Repeat("正文内容。\n", 80)

	reg := tools.NewRegistry()
	reg.MustRegister(tools.NewWriteFileTool(ws))

	sessionStore, err := store.NewFileSessionStore(ws)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	planStore, err := store.NewFilePlanStore(ws)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}

	runner := NewTurnRunner(TurnRunnerConfig{
		LLM:           staticLLM{out: out},
		Tools:         reg,
		Policy:        policy.NewDefaultPolicy(),
		SessionStore:  sessionStore,
		PlanStore:     planStore,
		Middlewares:   nil,
		WorkspaceRoot: ws,
		SkillIndex: stubSkillIndex{sk: &api.Skill{
			SkillMeta: api.SkillMeta{Name: "chapter-write"},
			Metadata:  map[string]string{"autosave": "novel_chapter"},
		}},
		ApprovalMode:       api.ModeFullAuto,
		FilterHistoryTools: true,
	})

	sess := &api.Session{SessionID: "s1", ActiveSkill: "chapter-write", Metadata: map[string]string{}}
	stream, err := runner.Run(context.Background(), sess, "写第4章")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	drainEvents(t, stream)

	gotPath := filepath.Join(ws, "novel", project, "volumes", "v1", "c004.md")
	b, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if strings.TrimSpace(string(b)) != strings.TrimSpace(out) {
		t.Fatalf("unexpected file content")
	}
}
