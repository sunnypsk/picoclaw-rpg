package turtlesoup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type scriptedJudge struct {
	results []Evaluation
	err     error
	calls   int
}

func (j *scriptedJudge) Evaluate(context.Context, GameState, string) (Evaluation, error) {
	j.calls++
	if j.err != nil {
		return Evaluation{}, j.err
	}
	if len(j.results) == 0 {
		return Evaluation{Kind: "question", Label: LabelCannotAnswer}, nil
	}
	result := j.results[0]
	j.results = j.results[1:]
	return result, nil
}

func TestStartStoresHiddenSolutionOnlyInPrivateStore(t *testing.T) {
	workspace := t.TempDir()
	privateRoot := filepath.Join(t.TempDir(), "private-games")
	secret := "hidden culprit is the lighthouse keeper"
	engine := NewEngine(NewStore(privateRoot), []Puzzle{{
		ID:       "test",
		Surface:  "湯面測試",
		Solution: secret,
	}})

	response, err := engine.Start("agent:main:test")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if strings.Contains(response, secret) {
		t.Fatalf("start response leaked solution: %s", response)
	}

	entries, err := os.ReadDir(privateRoot)
	if err != nil {
		t.Fatalf("ReadDir(privateRoot) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one private state file, got %d", len(entries))
	}
	if strings.Contains(entries[0].Name(), "agent") || strings.Contains(entries[0].Name(), ":") {
		t.Fatalf("private state filename should be hashed, got %q", entries[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(privateRoot, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile(private state) error = %v", err)
	}
	if !strings.Contains(string(data), secret) {
		t.Fatal("private state should contain the hidden solution")
	}

	workspaceEntries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("ReadDir(workspace) error = %v", err)
	}
	if len(workspaceEntries) != 0 {
		t.Fatalf("engine should not write hidden state into workspace, got %d entries", len(workspaceEntries))
	}
}

func TestHandleQuestionHintAndSolvedGuess(t *testing.T) {
	privateRoot := t.TempDir()
	engine := NewEngine(NewStore(privateRoot), []Puzzle{{
		ID:       "test",
		Surface:  "湯面測試",
		Solution: "湯底測試",
		Hints:    []string{"第一個提示"},
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	hint, err := engine.Handle(context.Background(), sessionKey, "提示", nil)
	if err != nil {
		t.Fatalf("Handle(hint) error = %v", err)
	}
	if !strings.Contains(hint, "第一個提示") {
		t.Fatalf("hint response = %q", hint)
	}

	judge := &scriptedJudge{results: []Evaluation{
		{Kind: "question", Label: LabelYes},
		{Kind: "guess", Solved: true},
	}}
	answer, err := engine.Handle(context.Background(), sessionKey, "他有看到別人嗎？", judge)
	if err != nil {
		t.Fatalf("Handle(question) error = %v", err)
	}
	if answer != "是" {
		t.Fatalf("answer = %q, want 是", answer)
	}

	reveal, err := engine.Handle(context.Background(), sessionKey, "我猜湯底測試", judge)
	if err != nil {
		t.Fatalf("Handle(guess) error = %v", err)
	}
	if !strings.Contains(reveal, "答對了") || !strings.Contains(reveal, "湯底測試") {
		t.Fatalf("reveal response = %q", reveal)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "還能問嗎？", judge); !errors.Is(err, ErrNoActiveGame) {
		t.Fatalf("expected ErrNoActiveGame after solved game, got %v", err)
	}
}

func TestJudgeFailureReturnsCannotAnswer(t *testing.T) {
	privateRoot := t.TempDir()
	engine := NewEngine(NewStore(privateRoot), []Puzzle{{
		ID:       "test",
		Surface:  "湯面測試",
		Solution: "湯底測試",
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	answer, err := engine.Handle(context.Background(), sessionKey, "這是問題嗎？", &scriptedJudge{err: errors.New("boom")})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if answer != "不能回答" {
		t.Fatalf("answer = %q, want 不能回答", answer)
	}
}
