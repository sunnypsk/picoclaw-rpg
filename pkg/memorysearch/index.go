package memorysearch

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sipeed/picoclaw/pkg/logger"
	_ "modernc.org/sqlite"
)

const (
	sqliteDriver = "sqlite"
)

type Result struct {
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

type Index struct {
	workspace  string
	database   string
	mu         sync.Mutex
	db         *sql.DB
	hasTrigram bool
}

type sourceDoc struct {
	Path    string
	Content string
	MTimeNS int64
	Hash    string
}

type metaRecord struct {
	Hash string
}

func NewIndex(workspace string) *Index {
	return &Index{
		workspace: strings.TrimSpace(workspace),
		database:  filepath.Join(strings.TrimSpace(workspace), "memory", "memory_search.sqlite"),
	}
}

func (i *Index) Search(ctx context.Context, query string, limit int, pathPrefix string) (results []Result, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []Result{}, nil
	}
	if limit <= 0 {
		limit = 5
	}
	pathPrefix = strings.TrimSpace(pathPrefix)

	start := time.Now()
	defer func() {
		fields := map[string]any{
			"workspace":     i.workspace,
			"query_preview": previewForLog(query, 120),
			"query_len":     len([]rune(query)),
			"limit":         limit,
			"path_prefix":   pathPrefix,
			"duration_ms":   time.Since(start).Milliseconds(),
		}

		if err != nil {
			fields["status"] = "error"
			fields["error"] = err.Error()
			logger.WarnCF("memory", "Memory search failed", fields)
			return
		}

		fields["status"] = "miss"
		fields["result_count"] = len(results)
		if len(results) > 0 {
			fields["status"] = "hit"
			fields["top_result_path"] = results[0].Path
			fields["top_result_score"] = results[0].Score
			fields["top_result_snippet_preview"] = previewForLog(results[0].Snippet, 160)
		}
		logger.InfoCF("memory", "Memory search completed", fields)
	}()

	i.mu.Lock()
	defer i.mu.Unlock()
	defer i.closeLocked()

	if err := i.ensureDBLocked(ctx); err != nil {
		return nil, err
	}
	if err := i.syncLocked(ctx); err != nil {
		return nil, err
	}

	tables := []string{"memory_fts_u"}
	if i.hasTrigram {
		tables = append(tables, "memory_fts_t")
	}

	merged := make(map[string]Result)
	var firstErr error
	for _, table := range tables {
		results, err := i.queryTableLocked(ctx, table, query, limit*3, pathPrefix)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, r := range results {
			existing, ok := merged[r.Path]
			if !ok || r.Score > existing.Score {
				merged[r.Path] = r
				continue
			}
			if ok && existing.Snippet == "" && r.Snippet != "" {
				existing.Snippet = r.Snippet
				merged[r.Path] = existing
			}
		}
	}

	if len(merged) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return []Result{}, nil
	}

	ordered := make([]Result, 0, len(merged))
	for _, r := range merged {
		ordered = append(ordered, r)
	}

	sort.SliceStable(ordered, func(a, b int) bool {
		if ordered[a].Score == ordered[b].Score {
			return ordered[a].Path < ordered[b].Path
		}
		return ordered[a].Score > ordered[b].Score
	})

	if len(ordered) > limit {
		ordered = ordered[:limit]
	}

	return ordered, nil
}

func (i *Index) Sync(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	defer i.closeLocked()

	if err := i.ensureDBLocked(ctx); err != nil {
		return err
	}
	return i.syncLocked(ctx)
}

func (i *Index) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.closeLocked()
}

func (i *Index) ensureDBLocked(ctx context.Context) error {
	if i.db != nil {
		return nil
	}

	if strings.TrimSpace(i.workspace) == "" {
		return fmt.Errorf("memory search workspace is empty")
	}

	if err := os.MkdirAll(filepath.Dir(i.database), 0o755); err != nil {
		return err
	}

	db, err := sql.Open(sqliteDriver, "file:"+i.database)
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA synchronous=NORMAL`); err != nil {
		_ = db.Close()
		return err
	}

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS memory_meta (
  path TEXT PRIMARY KEY,
  mtime_ns INTEGER NOT NULL,
  hash TEXT NOT NULL
)`); err != nil {
		_ = db.Close()
		return err
	}

	if _, err := db.ExecContext(ctx, `
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts_u
USING fts5(path UNINDEXED, content, tokenize='unicode61')`); err != nil {
		_ = db.Close()
		return err
	}

	i.hasTrigram = true
	if _, err := db.ExecContext(ctx, `
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts_t
USING fts5(path UNINDEXED, content, tokenize='trigram')`); err != nil {
		i.hasTrigram = false
	}

	i.db = db
	return nil
}

func (i *Index) syncLocked(ctx context.Context) error {
	docs, err := i.collectSourceDocs()
	if err != nil {
		return err
	}

	meta, err := i.loadMetaLocked(ctx)
	if err != nil {
		return err
	}

	for path, doc := range docs {
		existing, ok := meta[path]
		if ok && existing.Hash == doc.Hash {
			delete(meta, path)
			continue
		}

		if err := i.upsertDocLocked(ctx, doc); err != nil {
			return err
		}
		delete(meta, path)
	}

	for path := range meta {
		if err := i.deleteDocLocked(ctx, path); err != nil {
			return err
		}
	}

	return nil
}

func (i *Index) collectSourceDocs() (map[string]sourceDoc, error) {
	docs := make(map[string]sourceDoc)

	addMarkdown := func(absPath string) error {
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() || !strings.EqualFold(filepath.Ext(absPath), ".md") {
			return nil
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(i.workspace, absPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		content := string(data)
		docs[rel] = sourceDoc{
			Path:    rel,
			Content: content,
			MTimeNS: info.ModTime().UnixNano(),
			Hash:    hashText(content),
		}
		return nil
	}

	if err := addMarkdown(filepath.Join(i.workspace, "MEMORY.md")); err != nil {
		return nil, err
	}

	memoryDir := filepath.Join(i.workspace, "memory")
	if _, err := os.Stat(memoryDir); err == nil {
		err = filepath.WalkDir(memoryDir, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsNotExist(walkErr) {
					return nil
				}
				return walkErr
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
				return nil
			}
			return addMarkdown(path)
		})
		if err != nil {
			return nil, err
		}
	}

	return docs, nil
}

func (i *Index) loadMetaLocked(ctx context.Context) (map[string]metaRecord, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT path, hash FROM memory_meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meta := make(map[string]metaRecord)
	for rows.Next() {
		var path string
		var hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, err
		}
		meta[path] = metaRecord{Hash: hash}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return meta, nil
}

func (i *Index) upsertDocLocked(ctx context.Context, doc sourceDoc) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts_u WHERE path = ?`, doc.Path); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_fts_u(path, content) VALUES(?, ?)`,
		doc.Path, doc.Content); err != nil {
		return err
	}

	if i.hasTrigram {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts_t WHERE path = ?`, doc.Path); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO memory_fts_t(path, content) VALUES(?, ?)`,
			doc.Path, doc.Content); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_meta(path, mtime_ns, hash)
		 VALUES(?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET mtime_ns=excluded.mtime_ns, hash=excluded.hash`,
		doc.Path, doc.MTimeNS, doc.Hash); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true

	return nil
}

func (i *Index) deleteDocLocked(ctx context.Context, path string) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts_u WHERE path = ?`, path); err != nil {
		return err
	}
	if i.hasTrigram {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memory_fts_t WHERE path = ?`, path); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_meta WHERE path = ?`, path); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true

	return nil
}

func (i *Index) queryTableLocked(
	ctx context.Context,
	table string,
	query string,
	limit int,
	pathPrefix string,
) ([]Result, error) {
	return i.queryCandidatesLocked(ctx, table, buildMatchCandidates(query), limit, pathPrefix)
}

func (i *Index) queryCandidatesLocked(
	ctx context.Context,
	table string,
	candidates []string,
	limit int,
	pathPrefix string,
) ([]Result, error) {
	if limit <= 0 {
		limit = 5
	}

	prefix := strings.TrimSpace(pathPrefix)
	if prefix != "" {
		prefix = strings.TrimSuffix(filepath.ToSlash(prefix), "/") + "/"
	}

	var lastErr error
	var hadSuccessfulCandidate bool
	for _, matchQuery := range candidates {
		sqlText := fmt.Sprintf(
			`SELECT path, snippet(%[1]s, 1, '[', ']', ' ... ', 24) AS snippet, bm25(%[1]s) AS rank
			 FROM %[1]s
			 WHERE %[1]s MATCH ?`,
			table,
		)
		args := []any{matchQuery}
		if prefix != "" {
			sqlText += ` AND path LIKE ?`
			args = append(args, prefix+"%")
		}
		sqlText += ` ORDER BY rank LIMIT ?`
		args = append(args, limit)

		rows, err := i.db.QueryContext(ctx, sqlText, args...)
		if err != nil {
			lastErr = err
			continue
		}

		results := make([]Result, 0, limit)
		for rows.Next() {
			var path string
			var snippet sql.NullString
			var rank sql.NullFloat64
			if err := rows.Scan(&path, &snippet, &rank); err != nil {
				_ = rows.Close()
				return nil, err
			}
			rawRank := 0.0
			if rank.Valid {
				rawRank = rank.Float64
			}
			results = append(results, Result{
				Path:    path,
				Snippet: snippet.String,
				Score:   normalizeFTSScore(rawRank),
			})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			lastErr = err
			continue
		}
		_ = rows.Close()
		hadSuccessfulCandidate = true
		if len(results) > 0 {
			return results, nil
		}
	}

	if hadSuccessfulCandidate {
		return []Result{}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return []Result{}, nil
}

func normalizeFTSScore(rank float64) float64 {
	if rank < 0 {
		rank = -rank
	}
	return 1.0 / (1.0 + rank)
}

func buildMatchCandidates(query string) []string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []string{trimmed}
	}

	addCandidate := func(out []string, value string) []string {
		value = strings.TrimSpace(value)
		if value == "" {
			return out
		}
		for _, existing := range out {
			if existing == value {
				return out
			}
		}
		return append(out, value)
	}

	candidates := make([]string, 0, 3)
	if isSafeRawMatchCandidate(trimmed) {
		candidates = addCandidate(candidates, trimmed)
	}

	quoted := `"` + strings.ReplaceAll(trimmed, `"`, `""`) + `"`
	candidates = addCandidate(candidates, quoted)

	tokens := strings.FieldsFunc(trimmed, func(r rune) bool {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return false
		}
		return true
	})
	if len(tokens) > 0 {
		parts := make([]string, 0, len(tokens))
		for _, token := range tokens {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			parts = append(parts, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
		}
		if len(parts) > 0 {
			candidates = addCandidate(candidates, strings.Join(parts, " OR "))
		}
	}

	return candidates
}

func isSafeRawMatchCandidate(query string) bool {
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return true
}

func previewForLog(value string, maxRunes int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if maxRunes <= 0 {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func hashText(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (i *Index) closeLocked() error {
	if i.db == nil {
		return nil
	}
	err := i.db.Close()
	i.db = nil
	return err
}
