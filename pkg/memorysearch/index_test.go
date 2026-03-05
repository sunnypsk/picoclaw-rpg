package memorysearch

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndex_SearchFindsEnglishAndCJKContent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memory-search-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(tmpDir, "MEMORY.md"),
		[]byte("Favorite editor is neovim with terminal workflows."),
		0o644,
	); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(tmpDir, "memory", "20260305.md"),
		[]byte("今天討論資料庫遷移計畫與回滾策略。"),
		0o644,
	); err != nil {
		t.Fatalf("write daily note: %v", err)
	}

	idx := NewIndex(tmpDir)

	english, err := idx.Search(context.Background(), "favorite editor", 5, "")
	if err != nil {
		t.Fatalf("english search failed: %v", err)
	}
	if len(english) == 0 {
		t.Fatal("expected english search result")
	}
	if english[0].Path != "MEMORY.md" {
		t.Fatalf("first english result path = %q, want %q", english[0].Path, "MEMORY.md")
	}

	cjk, err := idx.Search(context.Background(), "資料庫遷移", 5, "")
	if err != nil {
		t.Fatalf("cjk search failed: %v", err)
	}
	if len(cjk) == 0 {
		t.Fatal("expected cjk search result")
	}
	foundDaily := false
	for _, r := range cjk {
		if r.Path == "memory/20260305.md" {
			foundDaily = true
			break
		}
	}
	if !foundDaily {
		t.Fatalf("expected cjk results to include daily note, got: %+v", cjk)
	}
}

func TestIndex_SearchPathPrefixFilter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memory-search-prefix-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(filepath.Join(tmpDir, "memory", "202603"), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "MEMORY.md"), []byte("project alpha long-term notes"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(tmpDir, "memory", "202603", "20260305.md"),
		[]byte("project alpha today decisions"),
		0o644,
	); err != nil {
		t.Fatalf("write daily note: %v", err)
	}

	idx := NewIndex(tmpDir)
	results, err := idx.Search(context.Background(), "project alpha", 5, "memory/")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results with path prefix")
	}

	for _, r := range results {
		if !strings.HasPrefix(r.Path, "memory/") {
			t.Fatalf("result path %q does not match memory/ prefix", r.Path)
		}
	}
}

func TestIndex_UpsertDocRollbackPreservesMetaOnFTSFailure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "memory-search-tx-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	idx := NewIndex(tmpDir)
	ctx := context.Background()

	oldDoc := sourceDoc{Path: "MEMORY.md", Content: "old memory", MTimeNS: 1, Hash: hashText("old memory")}
	newDoc := sourceDoc{Path: "MEMORY.md", Content: "new memory", MTimeNS: 2, Hash: hashText("new memory")}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	defer idx.closeLocked()

	if err := idx.ensureDBLocked(ctx); err != nil {
		t.Fatalf("ensure DB: %v", err)
	}
	if err := idx.upsertDocLocked(ctx, oldDoc); err != nil {
		t.Fatalf("seed old doc: %v", err)
	}

	if _, err := idx.db.ExecContext(ctx, `DROP TABLE memory_fts_u`); err != nil {
		t.Fatalf("drop memory_fts_u: %v", err)
	}

	if err := idx.upsertDocLocked(ctx, newDoc); err == nil {
		t.Fatal("expected upsert to fail after dropping memory_fts_u")
	}

	var gotHash string
	err = idx.db.QueryRowContext(ctx, `SELECT hash FROM memory_meta WHERE path = ?`, oldDoc.Path).Scan(&gotHash)
	if err != nil {
		t.Fatalf("query memory_meta hash: %v", err)
	}
	if gotHash != oldDoc.Hash {
		t.Fatalf("memory_meta hash changed after failed upsert: got %q, want %q", gotHash, oldDoc.Hash)
	}

	if _, err := idx.db.ExecContext(ctx, `CREATE VIRTUAL TABLE memory_fts_u USING fts5(path UNINDEXED, content, tokenize='unicode61')`); err != nil {
		t.Fatalf("recreate memory_fts_u: %v", err)
	}

	if err := idx.upsertDocLocked(ctx, newDoc); err != nil {
		t.Fatalf("upsert after recovery failed: %v", err)
	}

	var count int
	err = idx.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM memory_fts_u WHERE path = ?`, newDoc.Path).Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		t.Fatalf("query memory_fts_u count: %v", err)
	}
	if count != 1 {
		t.Fatalf("memory_fts_u count = %d, want 1", count)
	}
}
