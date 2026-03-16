package onboard

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCopyEmbeddedToTargetUsesAgentsMarkdown(t *testing.T) {
	targetDir := t.TempDir()

	if err := CopyEmbeddedWorkspaceTemplates(targetDir); err != nil {
		t.Fatalf("CopyEmbeddedWorkspaceTemplates() error = %v", err)
	}

	agentsPath := filepath.Join(targetDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); err != nil {
		t.Fatalf("expected %s to exist: %v", agentsPath, err)
	}

	legacyPath := filepath.Join(targetDir, "AGENT.md")
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy file %s to be absent, got err=%v", legacyPath, err)
	}
}

func TestCopyEmbeddedWorkspaceTemplates_ProactiveGuidanceIncludesAntiRepeatAndWalkImageBias(t *testing.T) {
	targetDir := t.TempDir()

	if err := CopyEmbeddedWorkspaceTemplates(targetDir); err != nil {
		t.Fatalf("CopyEmbeddedWorkspaceTemplates() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, "Do not repeat the same proactive point when the user has not replied") {
		t.Fatalf("expected anti-repeat proactive guidance in embedded AGENTS.md")
	}
	if !strings.Contains(text, "treat that as a stronger reason to share a brief life update or a scene image") {
		t.Fatalf("expected walk/image proactive guidance in embedded AGENTS.md")
	}
}

func TestGeneratedWorkspaceMatchesCanonicalWorkspace(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	packageDir := filepath.Dir(thisFile)
	repoRoot := filepath.Clean(filepath.Join(packageDir, "..", "..", "..", ".."))

	canonicalFiles := workspaceFixtureSnapshot(t, filepath.Join(repoRoot, "workspace"))
	generatedFiles := workspaceFixtureSnapshot(t, filepath.Join(packageDir, "workspace"))

	if !slices.Equal(canonicalFiles, generatedFiles) {
		t.Fatalf("generated onboard workspace is out of sync with workspace/: canonical=%v generated=%v", canonicalFiles, generatedFiles)
	}

	for _, relPath := range canonicalFiles {
		want, err := os.ReadFile(filepath.Join(repoRoot, "workspace", filepath.FromSlash(relPath)))
		if err != nil {
			t.Fatalf("read canonical %s: %v", relPath, err)
		}
		got, err := os.ReadFile(filepath.Join(packageDir, "workspace", filepath.FromSlash(relPath)))
		if err != nil {
			t.Fatalf("read generated %s: %v", relPath, err)
		}
		if string(got) != string(want) {
			t.Fatalf("generated onboard workspace file %s does not match workspace/", relPath)
		}
	}
}

func workspaceFixtureSnapshot(t *testing.T, root string) []string {
	t.Helper()

	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if strings.Contains(relPath, "__pycache__/") || strings.HasSuffix(relPath, ".pyc") {
			return nil
		}

		files = append(files, relPath)
		return nil
	})
	if err != nil {
		t.Fatalf("walk workspace fixture %s: %v", root, err)
	}

	slices.Sort(files)
	return files
}
