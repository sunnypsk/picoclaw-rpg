package turtlesoup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

const (
	stateVersion              = 1
	historyVersion            = 1
	maxCompletedGameSummaries = 10
)

var ErrNoActiveGame = errors.New("no active turtle soup game")

type GameState struct {
	Version       int       `json:"version"`
	GameID        string    `json:"game_id"`
	PublicCode    string    `json:"public_code,omitempty"`
	SessionHash   string    `json:"session_hash"`
	PuzzleID      string    `json:"puzzle_id"`
	Surface       string    `json:"surface"`
	Solution      string    `json:"solution"`
	Hints         []string  `json:"hints,omitempty"`
	Difficulty    string    `json:"difficulty,omitempty"`
	Themes        []string  `json:"themes,omitempty"`
	HintsUsed     int       `json:"hints_used"`
	QuestionCount int       `json:"question_count"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type GameSummary struct {
	GameID        string    `json:"game_id"`
	PublicCode    string    `json:"public_code,omitempty"`
	PuzzleID      string    `json:"puzzle_id,omitempty"`
	Surface       string    `json:"surface"`
	Difficulty    string    `json:"difficulty,omitempty"`
	Themes        []string  `json:"themes,omitempty"`
	QuestionCount int       `json:"question_count"`
	HintsUsed     int       `json:"hints_used"`
	Outcome       string    `json:"outcome"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
}

type gameHistory struct {
	Version     int           `json:"version"`
	SessionHash string        `json:"session_hash"`
	Games       []GameSummary `json:"games"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

func NewStore(root string) *Store {
	return &Store{root: strings.TrimSpace(root)}
}

func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *Store) Load(sessionKey string) (*GameState, error) {
	if s == nil {
		return nil, ErrNoActiveGame
	}
	path, err := s.pathForSession(sessionKey)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoActiveGame
		}
		return nil, fmt.Errorf("read turtle soup state: %w", err)
	}

	var state GameState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode turtle soup state: %w", err)
	}
	if state.Version == 0 {
		state.Version = stateVersion
	}
	return &state, nil
}

func (s *Store) Save(sessionKey string, state *GameState) error {
	if s == nil || state == nil {
		return nil
	}
	path, err := s.pathForSession(sessionKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create turtle soup state dir: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state.Version = stateVersion
	state.SessionHash = hashSessionKey(sessionKey)
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode turtle soup state: %w", err)
	}
	if err := fileutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("save turtle soup state: %w", err)
	}
	return nil
}

func (s *Store) Delete(sessionKey string) error {
	if s == nil {
		return nil
	}
	path, err := s.pathForSession(sessionKey)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete turtle soup state: %w", err)
	}
	return nil
}

func (s *Store) LoadHistory(sessionKey string, limit int) ([]GameSummary, error) {
	if s == nil {
		return nil, nil
	}
	path, err := s.historyPathForSession(sessionKey)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.readHistoryLocked(path)
	if err != nil {
		return nil, err
	}
	games := append([]GameSummary(nil), history.Games...)
	if limit > 0 && len(games) > limit {
		games = games[:limit]
	}
	return games, nil
}

func (s *Store) AppendHistory(sessionKey string, summary GameSummary) error {
	if s == nil {
		return nil
	}
	path, err := s.historyPathForSession(sessionKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create turtle soup history dir: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	history, err := s.readHistoryLocked(path)
	if err != nil {
		return err
	}
	history.Version = historyVersion
	history.SessionHash = hashSessionKey(sessionKey)
	history.Games = append([]GameSummary{summary}, history.Games...)
	if len(history.Games) > maxCompletedGameSummaries {
		history.Games = history.Games[:maxCompletedGameSummaries]
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("encode turtle soup history: %w", err)
	}
	if err := fileutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("save turtle soup history: %w", err)
	}
	return nil
}

func (s *Store) readHistoryLocked(path string) (gameHistory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return gameHistory{Version: historyVersion}, nil
		}
		return gameHistory{}, fmt.Errorf("read turtle soup history: %w", err)
	}
	var history gameHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return gameHistory{}, fmt.Errorf("decode turtle soup history: %w", err)
	}
	if history.Version == 0 {
		history.Version = historyVersion
	}
	return history, nil
}

func (s *Store) pathForSession(sessionKey string) (string, error) {
	if strings.TrimSpace(s.root) == "" {
		return "", errors.New("turtle soup store root is empty")
	}
	hash := hashSessionKey(sessionKey)
	if hash == "" {
		return "", errors.New("turtle soup session key is empty")
	}
	return filepath.Join(s.root, hash+".json"), nil
}

func (s *Store) historyPathForSession(sessionKey string) (string, error) {
	if strings.TrimSpace(s.root) == "" {
		return "", errors.New("turtle soup store root is empty")
	}
	hash := hashSessionKey(sessionKey)
	if hash == "" {
		return "", errors.New("turtle soup session key is empty")
	}
	return filepath.Join(s.root, hash+".history.json"), nil
}

func hashSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionKey))
	return hex.EncodeToString(sum[:])
}
