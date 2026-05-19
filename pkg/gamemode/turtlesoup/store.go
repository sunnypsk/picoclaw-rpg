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

const stateVersion = 1

var ErrNoActiveGame = errors.New("no active turtle soup game")

type GameState struct {
	Version       int       `json:"version"`
	GameID        string    `json:"game_id"`
	SessionHash   string    `json:"session_hash"`
	PuzzleID      string    `json:"puzzle_id"`
	Surface       string    `json:"surface"`
	Solution      string    `json:"solution"`
	Hints         []string  `json:"hints,omitempty"`
	HintsUsed     int       `json:"hints_used"`
	QuestionCount int       `json:"question_count"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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

func hashSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sessionKey))
	return hex.EncodeToString(sum[:])
}
