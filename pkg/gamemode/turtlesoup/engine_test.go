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
	inputs  []string
	states  []GameState
}

func (j *scriptedJudge) Evaluate(_ context.Context, state GameState, input string) (Evaluation, error) {
	j.calls++
	j.inputs = append(j.inputs, input)
	j.states = append(j.states, state)
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

type scriptedGenerator struct {
	puzzles  []Puzzle
	err      error
	calls    int
	requests []GenerationRequest
}

func (g *scriptedGenerator) Generate(_ context.Context, request GenerationRequest) (Puzzle, error) {
	g.calls++
	g.requests = append(g.requests, request)
	if g.err != nil {
		return Puzzle{}, g.err
	}
	if len(g.puzzles) == 0 {
		return Puzzle{}, errors.New("no scripted puzzle")
	}
	puzzle := g.puzzles[0]
	g.puzzles = g.puzzles[1:]
	return puzzle, nil
}

type scriptedHintProvider struct {
	hint   string
	err    error
	calls  int
	states []GameState
}

func (p *scriptedHintProvider) GenerateHint(_ context.Context, state GameState) (string, error) {
	p.calls++
	p.states = append(p.states, state)
	if p.err != nil {
		return "", p.err
	}
	return p.hint, nil
}

func TestStartWithGeneratedPuzzleStoresSettingsAndHidesSolution(t *testing.T) {
	privateRoot := filepath.Join(t.TempDir(), "private-games")
	secret := "the librarian moved the clock"
	engine := NewEngine(NewStore(privateRoot), nil)
	generator := &scriptedGenerator{puzzles: []Puzzle{{
		Surface:  "A guest leaves the library smiling after hearing the clock strike thirteen.",
		Solution: secret,
		Hints:    []string{"It is not a supernatural clock.", "The room matters.", "The guest wanted proof of tampering."},
	}}}

	response, err := engine.StartWithOptions(context.Background(), "agent:main:test", StartOptions{
		Difficulty: "harder than last time",
		Themes:     []string{"library", "time"},
		Message:    "start a harder turtle soup about a library",
		Generator:  generator,
	})
	if err != nil {
		t.Fatalf("StartWithOptions() error = %v", err)
	}
	if generator.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", generator.calls)
	}
	if got := generator.requests[0].Difficulty; got != "harder than last time" {
		t.Fatalf("generator difficulty = %q", got)
	}
	if strings.Contains(response, secret) {
		t.Fatalf("start response leaked solution: %s", response)
	}
	for _, want := range []string{"TS-", "harder than last time", "library", "time"} {
		if !strings.Contains(response, want) {
			t.Fatalf("start response missing %q: %s", want, response)
		}
	}

	state, err := engine.store.Load("agent:main:test")
	if err != nil {
		t.Fatalf("Load(started state) error = %v", err)
	}
	if state.Solution != secret {
		t.Fatalf("state solution = %q, want hidden solution", state.Solution)
	}
	if state.Difficulty != "harder than last time" {
		t.Fatalf("state difficulty = %q", state.Difficulty)
	}
	if strings.Join(state.Themes, ",") != "library,time" {
		t.Fatalf("state themes = %v", state.Themes)
	}
}

func TestStartWithGeneratedPuzzleRetriesInvalidResponse(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), nil)
	generator := &scriptedGenerator{puzzles: []Puzzle{
		{Surface: "missing solution", Hints: []string{"one", "two", "three"}},
		{Surface: "valid surface", Solution: "valid secret", Hints: []string{"one", "two", "three"}},
	}}

	response, err := engine.StartWithOptions(context.Background(), "agent:main:test", StartOptions{Generator: generator})
	if err != nil {
		t.Fatalf("StartWithOptions() error = %v", err)
	}
	if generator.calls != 2 {
		t.Fatalf("generator calls = %d, want retry once", generator.calls)
	}
	if !strings.Contains(response, "valid surface") || strings.Contains(response, "valid secret") {
		t.Fatalf("unexpected start response: %s", response)
	}
}

func TestStartWithGeneratedPuzzleFailsWithoutSavingAfterTwoInvalidResponses(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), nil)
	generator := &scriptedGenerator{puzzles: []Puzzle{
		{Surface: "missing solution", Hints: []string{"one", "two", "three"}},
		{Surface: "still missing solution", Hints: []string{"one", "two", "three"}},
	}}

	_, err := engine.StartWithOptions(context.Background(), "agent:main:test", StartOptions{Generator: generator})
	if err == nil {
		t.Fatal("expected generation error")
	}
	if generator.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", generator.calls)
	}
	if _, loadErr := engine.store.Load("agent:main:test"); !errors.Is(loadErr, ErrNoActiveGame) {
		t.Fatalf("expected no saved game after generation failure, got %v", loadErr)
	}
}

func TestStartWithActiveGameDoesNotGenerateAgain(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), nil)
	generator := &scriptedGenerator{puzzles: []Puzzle{{
		Surface:  "first surface",
		Solution: "first secret",
		Hints:    []string{"one", "two", "three"},
	}}}
	sessionKey := "agent:main:test"
	if _, err := engine.StartWithOptions(context.Background(), sessionKey, StartOptions{Generator: generator}); err != nil {
		t.Fatalf("initial StartWithOptions() error = %v", err)
	}
	if _, err := engine.StartWithOptions(context.Background(), sessionKey, StartOptions{Generator: generator}); err != nil {
		t.Fatalf("active StartWithOptions() error = %v", err)
	}
	if generator.calls != 1 {
		t.Fatalf("active start should not generate again, got %d calls", generator.calls)
	}
}

func TestStartWithInvalidDifficultyReturnsError(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), nil)
	tooLong := strings.Repeat("hard ", 40)

	_, err := engine.StartWithOptions(context.Background(), "agent:main:test", StartOptions{
		Difficulty: tooLong,
		Generator: &scriptedGenerator{puzzles: []Puzzle{{
			Surface:  "surface",
			Solution: "secret",
			Hints:    []string{"one", "two", "three"},
		}}},
	})
	if err == nil {
		t.Fatal("expected invalid difficulty error")
	}
}

func TestSolvedGameRecordsPrivateSummaryWithoutSolution(t *testing.T) {
	privateRoot := t.TempDir()
	engine := NewEngine(NewStore(privateRoot), []Puzzle{{
		ID:         "test",
		Surface:    "public surface",
		Solution:   "secret solution",
		Hints:      []string{"first hint"},
		Difficulty: "tricky",
		Themes:     []string{"clock"},
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "hint", nil); err != nil {
		t.Fatalf("Handle(hint) error = %v", err)
	}
	reveal, err := engine.Handle(context.Background(), sessionKey, "final guess", &scriptedJudge{
		results: []Evaluation{{Kind: "guess", Solved: true}},
	})
	if err != nil {
		t.Fatalf("Handle(solved guess) error = %v", err)
	}
	if !strings.Contains(reveal, "secret solution") {
		t.Fatalf("reveal should include solution, got %q", reveal)
	}

	history, err := engine.store.LoadHistory(sessionKey, 3)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("history length = %d, want 1", len(history))
	}
	summary := history[0]
	if summary.Outcome != OutcomeSolved || summary.HintsUsed != 1 || summary.Difficulty != "tricky" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if strings.Contains(summary.Surface, "secret solution") {
		t.Fatalf("summary surface leaked solution: %+v", summary)
	}
	historyPath, err := engine.store.historyPathForSession(sessionKey)
	if err != nil {
		t.Fatalf("historyPathForSession() error = %v", err)
	}
	data, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("ReadFile(history) error = %v", err)
	}
	if strings.Contains(string(data), "secret solution") {
		t.Fatalf("private summary history leaked solution: %s", data)
	}
}

func TestSurrenderRecordsPrivateSummary(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), []Puzzle{{
		ID:       "test",
		Surface:  "surface text",
		Solution: "solution secret",
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "giveup", nil); err != nil {
		t.Fatalf("Handle(giveup) error = %v", err)
	}

	history, err := engine.store.LoadHistory(sessionKey, 3)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(history) != 1 || history[0].Outcome != OutcomeSurrendered {
		t.Fatalf("history = %+v, want one surrendered game", history)
	}
}

func TestSurrenderStillEndsWhenHistoryIsCorrupt(t *testing.T) {
	root := t.TempDir()
	engine := NewEngine(NewStore(root), []Puzzle{{
		ID:       "test",
		Surface:  "surface text",
		Solution: "solution secret",
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	historyPath, err := engine.store.historyPathForSession(sessionKey)
	if err != nil {
		t.Fatalf("historyPathForSession() error = %v", err)
	}
	if err := os.WriteFile(historyPath, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile(history) error = %v", err)
	}

	reveal, err := engine.Handle(context.Background(), sessionKey, "giveup", nil)
	if err != nil {
		t.Fatalf("Handle(giveup) error = %v", err)
	}
	if !strings.Contains(reveal, "solution secret") {
		t.Fatalf("reveal response = %q", reveal)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "status", nil); !errors.Is(err, ErrNoActiveGame) {
		t.Fatalf("expected active game to be deleted despite history error, got %v", err)
	}
}

func TestStartWithGeneratedPuzzleReceivesRecentGameSummary(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), nil)
	generator := &scriptedGenerator{puzzles: []Puzzle{
		{Surface: "first surface", Solution: "first secret", Hints: []string{"one", "two", "three"}},
		{Surface: "second surface", Solution: "second secret", Hints: []string{"one", "two", "three"}},
	}}
	sessionKey := "agent:main:test"
	if _, err := engine.StartWithOptions(context.Background(), sessionKey, StartOptions{
		Difficulty: "hard",
		Themes:     []string{"library"},
		Generator:  generator,
	}); err != nil {
		t.Fatalf("initial StartWithOptions() error = %v", err)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "final guess", &scriptedJudge{
		results: []Evaluation{{Kind: "guess", Solved: true}},
	}); err != nil {
		t.Fatalf("Handle(solved guess) error = %v", err)
	}

	if _, err := engine.StartWithOptions(context.Background(), sessionKey, StartOptions{
		Difficulty: "harder than last time",
		Generator:  generator,
	}); err != nil {
		t.Fatalf("second StartWithOptions() error = %v", err)
	}
	if len(generator.requests) != 2 {
		t.Fatalf("generator requests = %d, want 2", len(generator.requests))
	}
	recent := generator.requests[1].RecentGames
	if len(recent) != 1 {
		t.Fatalf("recent games = %+v, want one completed game", recent)
	}
	if recent[0].Difficulty != "hard" || recent[0].Outcome != OutcomeSolved {
		t.Fatalf("recent game = %+v", recent[0])
	}
	if strings.Contains(recent[0].Surface, "first secret") {
		t.Fatalf("recent game leaked solution: %+v", recent[0])
	}
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
	state, err := engine.store.Load("agent:main:test")
	if err != nil {
		t.Fatalf("Load(started state) error = %v", err)
	}
	if !strings.HasPrefix(state.PublicCode, "TS-") || len(state.PublicCode) != len("TS-7K3P") {
		t.Fatalf("unexpected public code %q", state.PublicCode)
	}
	if !strings.Contains(response, "代號："+state.PublicCode) {
		t.Fatalf("start response should include public code %q, got %q", state.PublicCode, response)
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
	if !strings.Contains(string(data), `"public_code": "`+state.PublicCode+`"`) {
		t.Fatalf("private state should contain public code %q", state.PublicCode)
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

func TestHandleStoresTurnHistoryForLaterJudging(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), []Puzzle{{
		ID:       "test",
		Surface:  "surface text",
		Solution: "solution secret",
		Hints:    []string{"first hint"},
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	judge := &scriptedJudge{results: []Evaluation{
		{Kind: "question", Label: LabelYes},
		{Kind: "question", Label: LabelNo},
	}}
	if _, err := engine.Handle(context.Background(), sessionKey, "is the driver involved?", judge); err != nil {
		t.Fatalf("Handle(first question) error = %v", err)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "is the passenger the victim?", judge); err != nil {
		t.Fatalf("Handle(second question) error = %v", err)
	}
	if len(judge.states) != 2 {
		t.Fatalf("judge states = %d, want 2", len(judge.states))
	}
	if len(judge.states[0].Turns) != 0 {
		t.Fatalf("first judge call should not see current turn history, got %+v", judge.states[0].Turns)
	}
	if len(judge.states[1].Turns) != 1 || judge.states[1].Turns[0].Label != LabelYes {
		t.Fatalf("second judge call should see first turn, got %+v", judge.states[1].Turns)
	}

	state, err := engine.store.Load(sessionKey)
	if err != nil {
		t.Fatalf("Load(state) error = %v", err)
	}
	if state.QuestionCount != 2 || len(state.Turns) != 2 {
		t.Fatalf("state question count/turns = %d/%d, want 2/2", state.QuestionCount, len(state.Turns))
	}
	if state.Turns[0].PlayerMessage != "is the driver involved?" || state.Turns[0].Label != LabelYes {
		t.Fatalf("first stored turn = %+v", state.Turns[0])
	}
	if state.Turns[1].PlayerMessage != "is the passenger the victim?" || state.Turns[1].Label != LabelNo {
		t.Fatalf("second stored turn = %+v", state.Turns[1])
	}
}

func TestContextAwareHintReceivesPriorTurns(t *testing.T) {
	engine := NewEngine(NewStore(t.TempDir()), []Puzzle{{
		ID:       "test",
		Surface:  "surface text",
		Solution: "solution secret",
		Hints:    []string{"static hint one", "static hint two"},
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "is the driver involved?", &scriptedJudge{
		results: []Evaluation{{Kind: "question", Label: LabelYes}},
	}); err != nil {
		t.Fatalf("Handle(question) error = %v", err)
	}

	hints := &scriptedHintProvider{hint: "new useful hint"}
	response, err := engine.HandleWithOptions(context.Background(), sessionKey, "hint", HandleOptions{HintProvider: hints})
	if err != nil {
		t.Fatalf("HandleWithOptions(hint) error = %v", err)
	}
	if !strings.Contains(response, "new useful hint") {
		t.Fatalf("hint response = %q", response)
	}
	if hints.calls != 1 {
		t.Fatalf("hint provider calls = %d, want 1", hints.calls)
	}
	if len(hints.states) != 1 || len(hints.states[0].Turns) != 1 {
		t.Fatalf("hint provider should receive prior turn history, got %+v", hints.states)
	}
}

func TestHandleAcceptsPublicCodeReferences(t *testing.T) {
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
	state, err := engine.store.Load(sessionKey)
	if err != nil {
		t.Fatalf("Load(started state) error = %v", err)
	}
	code := state.PublicCode

	status, err := engine.Handle(context.Background(), sessionKey, "/turtle "+code+" status", nil)
	if err != nil {
		t.Fatalf("Handle(status) error = %v", err)
	}
	if !strings.Contains(status, "代號："+code) || !strings.Contains(status, "湯面測試") {
		t.Fatalf("status response should include code and surface, got %q", status)
	}

	judge := &scriptedJudge{results: []Evaluation{{Kind: "question", Label: LabelNo}}}
	compactCode := strings.ReplaceAll(strings.ToLower(code), "-", "")
	answer, err := engine.Handle(context.Background(), sessionKey, compactCode+" 這跟八樓有關嗎？", judge)
	if err != nil {
		t.Fatalf("Handle(question with code) error = %v", err)
	}
	if answer != "否" {
		t.Fatalf("answer = %q, want 否", answer)
	}
	if judge.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", judge.calls)
	}
	if got := judge.inputs[0]; got != "這跟八樓有關嗎？" {
		t.Fatalf("judge input = %q, want stripped player question", got)
	}

	wrongJudge := &scriptedJudge{results: []Evaluation{{Kind: "question", Label: LabelYes}}}
	missing, err := engine.Handle(context.Background(), sessionKey, "/turtle TS-0000 hint", wrongJudge)
	if err != nil {
		t.Fatalf("Handle(wrong code) error = %v", err)
	}
	if !strings.Contains(missing, "找不到這局海龜湯") {
		t.Fatalf("wrong-code response = %q", missing)
	}
	if wrongJudge.calls != 0 {
		t.Fatalf("wrong code should not call judge, got %d calls", wrongJudge.calls)
	}
}

func TestHandleAcceptsCodedGiveupCommand(t *testing.T) {
	privateRoot := t.TempDir()
	engine := NewEngine(NewStore(privateRoot), []Puzzle{{
		ID:       "test",
		Surface:  "surface text",
		Solution: "solution secret",
	}})
	sessionKey := "agent:main:test"
	if _, err := engine.Start(sessionKey); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	state, err := engine.store.Load(sessionKey)
	if err != nil {
		t.Fatalf("Load(started state) error = %v", err)
	}

	judge := &scriptedJudge{results: []Evaluation{{Kind: "guess"}}}
	reveal, err := engine.Handle(context.Background(), sessionKey, "/turtle "+state.PublicCode+" giveup", judge)
	if err != nil {
		t.Fatalf("Handle(coded giveup) error = %v", err)
	}
	if !strings.Contains(reveal, "solution secret") {
		t.Fatalf("coded giveup should reveal solution, got %q", reveal)
	}
	if judge.calls != 0 {
		t.Fatalf("coded giveup should not call judge, got %d calls", judge.calls)
	}
	if _, err := engine.Handle(context.Background(), sessionKey, "status", nil); !errors.Is(err, ErrNoActiveGame) {
		t.Fatalf("expected ErrNoActiveGame after coded giveup, got %v", err)
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
