package turtlesoup

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const publicCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

const recentGameSummaryLimit = 3

const (
	OutcomeSolved      = "solved"
	OutcomeSurrendered = "surrendered"
)

type StartOptions struct {
	Difficulty string
	Themes     []string
	Message    string
	Generator  PuzzleGenerator
}

type GenerationRequest struct {
	Difficulty  string
	Themes      []string
	Message     string
	RecentGames []GameSummary
}

type PuzzleGenerator interface {
	Generate(ctx context.Context, request GenerationRequest) (Puzzle, error)
}

type Engine struct {
	store   *Store
	puzzles []Puzzle
	clock   func() time.Time
}

func NewEngine(store *Store, puzzles []Puzzle) *Engine {
	return &Engine{
		store:   store,
		puzzles: append([]Puzzle(nil), puzzles...),
		clock:   time.Now,
	}
}

func (e *Engine) IsStartRequest(input string) bool {
	return isStartRequest(input)
}

func (e *Engine) HasActive(sessionKey string) bool {
	if e == nil || e.store == nil {
		return false
	}
	_, err := e.store.Load(sessionKey)
	return err == nil
}

func (e *Engine) ReferencesGameCode(input string) bool {
	ref := parseGameReference(input)
	return ref.hasCode
}

func (e *Engine) Start(sessionKey string) (string, error) {
	return e.StartWithOptions(context.Background(), sessionKey, StartOptions{})
}

func (e *Engine) StartWithOptions(ctx context.Context, sessionKey string, options StartOptions) (string, error) {
	if e == nil || e.store == nil {
		return "", errors.New("turtle soup engine is not configured")
	}
	if state, err := e.store.Load(sessionKey); err == nil && state != nil {
		if err := e.ensurePublicCode(sessionKey, state); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"已經有一局海龜湯進行中。\n代號：%s\n\n湯面：%s\n\n可以問是非題，或輸入「提示」「放棄」。",
			state.PublicCode,
			state.Surface,
		), nil
	}

	difficulty, err := normalizeDifficulty(options.Difficulty)
	if err != nil {
		return "", err
	}
	themes, err := normalizeThemes(options.Themes)
	if err != nil {
		return "", err
	}
	recentGames, err := e.store.LoadHistory(sessionKey, recentGameSummaryLimit)
	if err != nil {
		return "", err
	}
	puzzle, err := e.startPuzzle(ctx, GenerationRequest{
		Difficulty:  difficulty,
		Themes:      themes,
		Message:     strings.TrimSpace(options.Message),
		RecentGames: recentGames,
	}, options.Generator)
	if err != nil {
		return "", err
	}
	now := e.now()
	state := &GameState{
		GameID:     newGameID(),
		PublicCode: newPublicCode(),
		PuzzleID:   puzzle.ID,
		Surface:    surfaceWithPublicSettings(puzzle.Surface, puzzle.Difficulty, puzzle.Themes),
		Solution:   puzzle.Solution,
		Hints:      append([]string(nil), puzzle.Hints...),
		Difficulty: defaultString(puzzle.Difficulty, difficulty),
		Themes:     append([]string(nil), firstNonEmptyThemes(puzzle.Themes, themes)...),
		StartedAt:  now,
		UpdatedAt:  now,
	}
	if err := e.store.Save(sessionKey, state); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"海龜湯開始。\n代號：%s\n\n湯面：%s\n\n你可以問是非題；我只會回答「是 / 否 / 無關 / 部分是 / 不能回答」。",
		state.PublicCode,
		state.Surface,
	), nil
}

func (e *Engine) Handle(ctx context.Context, sessionKey, input string, judge Judge) (string, error) {
	if e == nil || e.store == nil {
		return "", errors.New("turtle soup engine is not configured")
	}
	state, err := e.store.Load(sessionKey)
	if err != nil {
		return "", err
	}
	if err := e.ensurePublicCode(sessionKey, state); err != nil {
		return "", err
	}

	input = strings.TrimSpace(input)
	ref := parseGameReference(input)
	if ref.hasCode {
		if !samePublicCode(ref.code, state.PublicCode) {
			return "找不到這局海龜湯，請確認代號。", nil
		}
		input = ref.remaining
		if input == "" {
			return statusText(*state), nil
		}
	}
	if input == "" {
		return "請問一個是非題，或輸入「提示」「放棄」。", nil
	}
	if isHintRequest(input) {
		return e.hint(sessionKey, state)
	}
	if isStatusRequest(input) {
		return statusText(*state), nil
	}
	if isSurrenderRequest(input) {
		return e.revealAndEnd(sessionKey, state, "揭曉湯底", OutcomeSurrendered)
	}
	if isStartRequest(input) {
		return fmt.Sprintf(
			"這局還在進行中。\n代號：%s\n\n湯面：%s\n\n可以繼續問問題，或輸入「提示」「放棄」。",
			state.PublicCode,
			state.Surface,
		), nil
	}
	if judge == nil {
		return "不能回答", nil
	}

	eval, err := judge.Evaluate(ctx, *state, input)
	if err != nil {
		return "不能回答", nil
	}
	if eval.Kind == "guess" {
		if eval.Solved {
			return e.revealAndEnd(sessionKey, state, "答對了！", OutcomeSolved)
		}
		return "還不是湯底。你可以繼續問。", nil
	}

	state.QuestionCount++
	if err := e.store.Save(sessionKey, state); err != nil {
		return "", err
	}
	return labelText(eval.Label), nil
}

func (e *Engine) hint(sessionKey string, state *GameState) (string, error) {
	if len(state.Hints) == 0 {
		return "這題沒有提示。", nil
	}
	if state.HintsUsed >= len(state.Hints) {
		return "提示已經用完。", nil
	}
	hint := state.Hints[state.HintsUsed]
	state.HintsUsed++
	if err := e.store.Save(sessionKey, state); err != nil {
		return "", err
	}
	return fmt.Sprintf("代號：%s\n提示 %d：%s", state.PublicCode, state.HintsUsed, hint), nil
}

func (e *Engine) revealAndEnd(sessionKey string, state *GameState, prefix, outcome string) (string, error) {
	_ = e.recordCompletedGame(sessionKey, state, outcome)
	if err := e.store.Delete(sessionKey); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s\n代號：%s\n\n湯底：%s", prefix, state.PublicCode, state.Solution), nil
}

func (e *Engine) recordCompletedGame(sessionKey string, state *GameState, outcome string) error {
	if e == nil || e.store == nil || state == nil {
		return nil
	}
	return e.store.AppendHistory(sessionKey, GameSummary{
		GameID:        state.GameID,
		PublicCode:    state.PublicCode,
		PuzzleID:      state.PuzzleID,
		Surface:       state.Surface,
		Difficulty:    state.Difficulty,
		Themes:        append([]string(nil), state.Themes...),
		QuestionCount: state.QuestionCount,
		HintsUsed:     state.HintsUsed,
		Outcome:       outcome,
		StartedAt:     state.StartedAt,
		EndedAt:       e.now(),
	})
}

func (e *Engine) ensurePublicCode(sessionKey string, state *GameState) error {
	if state == nil || strings.TrimSpace(state.PublicCode) != "" {
		return nil
	}
	state.PublicCode = newPublicCode()
	return e.store.Save(sessionKey, state)
}

func (e *Engine) now() time.Time {
	if e != nil && e.clock != nil {
		return e.clock()
	}
	return time.Now()
}

func (e *Engine) pickPuzzle() Puzzle {
	if e == nil || len(e.puzzles) == 0 {
		return Puzzle{}
	}
	if len(e.puzzles) == 1 {
		return e.puzzles[0]
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return e.puzzles[0]
	}
	idx := int(binary.BigEndian.Uint64(buf[:]) % uint64(len(e.puzzles)))
	return e.puzzles[idx]
}

func (e *Engine) startPuzzle(ctx context.Context, request GenerationRequest, generator PuzzleGenerator) (Puzzle, error) {
	if generator != nil {
		var lastErr error
		for attempt := 0; attempt < 2; attempt++ {
			puzzle, err := generator.Generate(ctx, request)
			if err == nil {
				puzzle = normalizePuzzleMetadata(puzzle, request)
				if validateErr := validatePuzzle(puzzle, true); validateErr == nil {
					return puzzle, nil
				} else {
					err = validateErr
				}
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = errors.New("invalid turtle soup puzzle")
		}
		return Puzzle{}, fmt.Errorf("generate turtle soup puzzle: %w", lastErr)
	}

	if len(e.puzzles) == 0 {
		return Puzzle{}, errors.New("turtle soup puzzle generator is not configured")
	}
	puzzle := normalizePuzzleMetadata(e.pickPuzzle(), request)
	if err := validatePuzzle(puzzle, false); err != nil {
		return Puzzle{}, err
	}
	return puzzle, nil
}

func normalizePuzzleMetadata(puzzle Puzzle, request GenerationRequest) Puzzle {
	puzzle.ID = strings.TrimSpace(puzzle.ID)
	if puzzle.ID == "" {
		puzzle.ID = "generated-" + newGameID()
	}
	puzzle.Surface = strings.TrimSpace(puzzle.Surface)
	puzzle.Solution = strings.TrimSpace(puzzle.Solution)
	puzzle.Difficulty = defaultString(strings.TrimSpace(puzzle.Difficulty), request.Difficulty)
	puzzle.Themes = firstNonEmptyThemes(puzzle.Themes, request.Themes)
	hints := make([]string, 0, len(puzzle.Hints))
	for _, hint := range puzzle.Hints {
		hint = strings.TrimSpace(hint)
		if hint != "" {
			hints = append(hints, hint)
		}
	}
	if len(hints) > 3 {
		hints = hints[:3]
	}
	puzzle.Hints = hints
	return puzzle
}

func validatePuzzle(puzzle Puzzle, generated bool) error {
	if strings.TrimSpace(puzzle.Surface) == "" {
		return errors.New("turtle soup puzzle surface is empty")
	}
	if strings.TrimSpace(puzzle.Solution) == "" {
		return errors.New("turtle soup puzzle solution is empty")
	}
	if generated && len(puzzle.Hints) < 3 {
		return errors.New("generated turtle soup puzzle must include at least 3 hints")
	}
	if visibleContainsSolution(puzzle.Surface, puzzle.Solution) {
		return errors.New("generated turtle soup puzzle surface leaks the solution")
	}
	for _, hint := range puzzle.Hints {
		if visibleContainsSolution(hint, puzzle.Solution) {
			return errors.New("generated turtle soup puzzle hint leaks the solution")
		}
	}
	return nil
}

func visibleContainsSolution(visible, solution string) bool {
	visible = strings.ToLower(strings.TrimSpace(visible))
	solution = strings.ToLower(strings.TrimSpace(solution))
	return solution != "" && visible != "" && strings.Contains(visible, solution)
}

func normalizeDifficulty(value string) (string, error) {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len([]rune(value)) > 120 {
		return "", errors.New("turtle soup difficulty must be 120 characters or fewer")
	}
	return value, nil
}

func normalizeThemes(values []string) ([]string, error) {
	const (
		maxThemes     = 8
		maxThemeRunes = 40
	)
	if len(values) == 0 {
		return nil, nil
	}
	themes := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
		if value == "" {
			continue
		}
		if len([]rune(value)) > maxThemeRunes {
			return nil, errors.New("turtle soup theme tags must be 40 characters or fewer")
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		themes = append(themes, value)
		if len(themes) > maxThemes {
			return nil, fmt.Errorf("turtle soup supports at most %d theme tags", maxThemes)
		}
	}
	return themes, nil
}

func firstNonEmptyThemes(first, second []string) []string {
	if len(first) > 0 {
		themes, err := normalizeThemes(first)
		if err == nil && len(themes) > 0 {
			return themes
		}
	}
	return append([]string(nil), second...)
}

func surfaceWithPublicSettings(surface, difficulty string, themes []string) string {
	settings := publicSettingsText(difficulty, themes)
	if settings == "" {
		return surface
	}
	return surface + "\n\n" + settings
}

func publicSettingsText(difficulty string, themes []string) string {
	parts := make([]string, 0, 2)
	if difficulty = strings.TrimSpace(difficulty); difficulty != "" {
		parts = append(parts, "Difficulty: "+difficulty)
	}
	if len(themes) > 0 {
		parts = append(parts, "Themes: "+strings.Join(themes, ", "))
	}
	return strings.Join(parts, "\n")
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func statusText(state GameState) string {
	remainingHints := len(state.Hints) - state.HintsUsed
	if remainingHints < 0 {
		remainingHints = 0
	}
	return fmt.Sprintf("代號：%s\n湯面：%s\n\n已問問題：%d\n已用提示：%d\n剩餘提示：%d",
		state.PublicCode,
		state.Surface,
		state.QuestionCount,
		state.HintsUsed,
		remainingHints,
	)
}

func isStartRequest(input string) bool {
	trimmed := strings.TrimSpace(input)
	lower := strings.ToLower(trimmed)
	if lower == "/turtle" || lower == "/turtle start" || lower == "/turtle-soup" || lower == "/turtlesoup" {
		return true
	}
	if strings.HasPrefix(lower, "/turtle ") {
		parts := strings.Fields(lower)
		return len(parts) >= 2 && (parts[1] == "start" || parts[1] == "new")
	}
	hasName := strings.Contains(trimmed, "海龜湯") || strings.Contains(trimmed, "海龟汤")
	if !hasName {
		return false
	}
	return strings.Contains(trimmed, "開") ||
		strings.Contains(trimmed, "开") ||
		strings.Contains(trimmed, "玩") ||
		strings.Contains(strings.ToLower(trimmed), "start")
}

func isHintRequest(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return lower == "提示" || lower == "hint" || lower == "/turtle hint"
}

func isStatusRequest(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return lower == "狀態" || lower == "状态" || lower == "status" || lower == "/turtle status"
}

func isSurrenderRequest(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	return lower == "放棄" ||
		lower == "放弃" ||
		lower == "揭曉" ||
		lower == "揭晓" ||
		lower == "答案" ||
		lower == "give up" ||
		lower == "giveup" ||
		lower == "surrender" ||
		lower == "/turtle giveup" ||
		lower == "/turtle surrender"
}

type gameReference struct {
	hasCode   bool
	code      string
	remaining string
}

func parseGameReference(input string) gameReference {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return gameReference{}
	}

	first := strings.ToLower(fields[0])
	if first == "/turtle" || first == "/turtle-soup" || first == "/turtlesoup" {
		if len(fields) < 2 {
			return gameReference{}
		}
		code := normalizePublicCode(fields[1])
		if code == "" {
			return gameReference{}
		}
		return gameReference{
			hasCode:   true,
			code:      code,
			remaining: strings.TrimSpace(strings.Join(fields[2:], " ")),
		}
	}

	code := normalizePublicCode(fields[0])
	if code == "" {
		return gameReference{}
	}
	return gameReference{
		hasCode:   true,
		code:      code,
		remaining: strings.TrimSpace(strings.Join(fields[1:], " ")),
	}
}

func samePublicCode(a, b string) bool {
	return normalizePublicCode(a) != "" && normalizePublicCode(a) == normalizePublicCode(b)
}

func normalizePublicCode(code string) string {
	code = strings.TrimSpace(code)
	code = strings.Trim(code, ":：,，.。")
	code = strings.ToUpper(strings.ReplaceAll(code, "-", ""))
	if len(code) != 6 || !strings.HasPrefix(code, "TS") {
		return ""
	}
	suffix := code[2:]
	for _, r := range suffix {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return "TS-" + suffix
}

func newPublicCode() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("TS-%04X", time.Now().UnixNano()%0x10000)
	}
	out := make([]byte, 3+len(buf))
	copy(out, "TS-")
	for i, b := range buf {
		out[i+3] = publicCodeAlphabet[int(b)%len(publicCodeAlphabet)]
	}
	return string(out)
}

func newGameID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
