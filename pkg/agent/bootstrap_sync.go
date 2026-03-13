package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/fileutil"
)

const bootstrapMetadataFilename = ".picoclaw-bootstrap.json"
const bootstrapMetadataVersion = 1

type WorkspaceDefaultsSyncOptions struct {
	DryRun      bool
	ForceLegacy bool
}

type WorkspaceDefaultsSyncReport struct {
	Workspace string
	Created   []string
	Updated   []string
	Adopted   []string
	Deleted   []string
	Preserved []string
	Conflicts []string
	Warnings  []string
}

type workspaceBootstrapMetadata struct {
	Version int                               `json:"version"`
	Files   map[string]workspaceBootstrapFile `json:"files,omitempty"`
}

type workspaceBootstrapFile struct {
	Hash string `json:"hash"`
}

type desiredWorkspaceFile struct {
	Content []byte
	Perm    os.FileMode
}

func (r WorkspaceDefaultsSyncReport) HasActions() bool {
	return len(r.Created) > 0 ||
		len(r.Updated) > 0 ||
		len(r.Adopted) > 0 ||
		len(r.Deleted) > 0 ||
		len(r.Preserved) > 0 ||
		len(r.Conflicts) > 0
}

func DiscoverDefaultSyncWorkspaces(cfg *config.Config) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	sourceWorkspace := strings.TrimSpace(cfg.WorkspacePath())
	if sourceWorkspace == "" {
		return nil, nil
	}

	paths := make(map[string]string)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		cleaned := filepath.Clean(path)
		if isSamePath(cleaned, sourceWorkspace) {
			return
		}
		paths[pathKey(cleaned)] = cleaned
	}

	for i := range cfg.Agents.List {
		add(resolveAgentWorkspace(&cfg.Agents.List[i], &cfg.Agents.Defaults))
	}

	parentDir := filepath.Dir(sourceWorkspace)
	entries, err := os.ReadDir(parentDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "workspace-") {
				continue
			}
			add(filepath.Join(parentDir, entry.Name()))
		}
	}

	results := make([]string, 0, len(paths))
	for _, path := range paths {
		results = append(results, path)
	}
	sort.Strings(results)
	return results, nil
}

func SyncWorkspaceDefaults(
	workspace string,
	defaults *config.AgentDefaults,
	opts WorkspaceDefaultsSyncOptions,
) (WorkspaceDefaultsSyncReport, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return WorkspaceDefaultsSyncReport{}, fmt.Errorf("workspace is empty")
	}

	report := WorkspaceDefaultsSyncReport{Workspace: filepath.Clean(workspace)}
	sourceWorkspace := ""
	if defaults != nil {
		sourceWorkspace = strings.TrimSpace(expandHome(defaults.Workspace))
	}

	trackMetadata := true
	if sourceWorkspace != "" && isSamePath(sourceWorkspace, workspace) {
		trackMetadata = false
	}

	if !opts.DryRun {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			return report, err
		}
		if err := os.MkdirAll(filepath.Join(workspace, "skills"), 0o755); err != nil {
			return report, err
		}
	}

	if err := ensureUnmanagedBootstrapFiles(workspace, sourceWorkspace, opts.DryRun, &report); err != nil {
		return report, err
	}

	desiredFiles, err := buildDesiredManagedFiles(sourceWorkspace, defaults)
	if err != nil {
		return report, err
	}

	meta := newBootstrapMetadata()
	metaExisted := false
	if trackMetadata {
		var warning string
		meta, metaExisted, warning, err = loadWorkspaceBootstrapMetadata(workspace)
		if err != nil {
			return report, err
		}
		if warning != "" {
			report.Warnings = append(report.Warnings, warning)
		}
	}

	for _, relPath := range sortedDesiredPaths(desiredFiles) {
		desired := desiredFiles[relPath]
		targetPath := filepath.Join(workspace, filepath.FromSlash(relPath))
		currentHash, exists, err := fileHashIfExists(targetPath)
		if err != nil {
			return report, err
		}

		metaEntry, hasMeta := meta.Files[relPath]
		desiredHash := hashBytes(desired.Content)

		switch {
		case !exists:
			report.Created = append(report.Created, relPath)
			if !opts.DryRun {
				if err := writeManagedFile(targetPath, desired.Content, desired.Perm); err != nil {
					return report, err
				}
			}
			if trackMetadata {
				meta.Files[relPath] = workspaceBootstrapFile{Hash: desiredHash}
			}
		case hasMeta && currentHash == metaEntry.Hash:
			if currentHash == desiredHash {
				continue
			}
			report.Updated = append(report.Updated, relPath)
			if !opts.DryRun {
				if err := writeManagedFile(targetPath, desired.Content, desired.Perm); err != nil {
					return report, err
				}
			}
			if trackMetadata {
				meta.Files[relPath] = workspaceBootstrapFile{Hash: desiredHash}
			}
		case hasMeta && currentHash != metaEntry.Hash:
			if currentHash == desiredHash {
				if trackMetadata {
					meta.Files[relPath] = workspaceBootstrapFile{Hash: currentHash}
				}
				report.Adopted = append(report.Adopted, relPath)
				continue
			}
			report.Preserved = append(report.Preserved, relPath)
		case !hasMeta && currentHash == desiredHash:
			if trackMetadata {
				meta.Files[relPath] = workspaceBootstrapFile{Hash: currentHash}
				report.Adopted = append(report.Adopted, relPath)
			}
		case !hasMeta && opts.ForceLegacy:
			report.Updated = append(report.Updated, relPath)
			if !opts.DryRun {
				if err := writeManagedFile(targetPath, desired.Content, desired.Perm); err != nil {
					return report, err
				}
			}
			if trackMetadata {
				meta.Files[relPath] = workspaceBootstrapFile{Hash: desiredHash}
			}
		default:
			report.Conflicts = append(report.Conflicts, relPath)
		}
	}

	if trackMetadata {
		for _, relPath := range sortedMetadataPaths(meta.Files) {
			if _, ok := desiredFiles[relPath]; ok {
				continue
			}
			if !strings.HasPrefix(relPath, "skills/") {
				delete(meta.Files, relPath)
				continue
			}

			targetPath := filepath.Join(workspace, filepath.FromSlash(relPath))
			currentHash, exists, err := fileHashIfExists(targetPath)
			if err != nil {
				return report, err
			}

			metaEntry := meta.Files[relPath]
			switch {
			case !exists:
				delete(meta.Files, relPath)
			case currentHash == metaEntry.Hash:
				report.Deleted = append(report.Deleted, relPath)
				if !opts.DryRun {
					if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
						return report, err
					}
				}
				delete(meta.Files, relPath)
			default:
				report.Preserved = append(report.Preserved, relPath)
				delete(meta.Files, relPath)
			}
		}
	}

	if trackMetadata && !opts.DryRun {
		if err := saveWorkspaceBootstrapMetadata(workspace, meta, metaExisted); err != nil {
			return report, err
		}
	}

	return report, nil
}

func ensureUnmanagedBootstrapFiles(
	workspace string,
	sourceWorkspace string,
	dryRun bool,
	report *WorkspaceDefaultsSyncReport,
) error {
	for _, relPath := range unmanagedBootstrapFiles {
		targetPath := filepath.Join(workspace, filepath.FromSlash(relPath))
		_, exists, err := fileHashIfExists(targetPath)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		content, perm, err := desiredUnmanagedBootstrapFile(sourceWorkspace, relPath)
		if err != nil {
			return err
		}
		if len(content) == 0 {
			continue
		}

		report.Created = append(report.Created, relPath)
		if dryRun {
			continue
		}
		if err := writeManagedFile(targetPath, content, perm); err != nil {
			return err
		}
	}
	return nil
}

func desiredUnmanagedBootstrapFile(sourceWorkspace, relPath string) ([]byte, os.FileMode, error) {
	if strings.TrimSpace(sourceWorkspace) != "" {
		sourcePath := filepath.Join(sourceWorkspace, filepath.FromSlash(relPath))
		if data, err := os.ReadFile(sourcePath); err == nil {
			return data, 0o644, nil
		} else if !os.IsNotExist(err) {
			return nil, 0, err
		}
	}

	content := fallbackBootstrapContent(relPath)
	if strings.TrimSpace(content) == "" {
		return nil, 0, nil
	}
	return []byte(content), 0o644, nil
}

func buildDesiredManagedFiles(
	sourceWorkspace string,
	defaults *config.AgentDefaults,
) (map[string]desiredWorkspaceFile, error) {
	desired := make(map[string]desiredWorkspaceFile, len(managedBootstrapFiles))

	for _, relPath := range managedBootstrapFiles {
		if override, ok := personaBootstrapContent(defaults, relPath); ok {
			desired[relPath] = desiredWorkspaceFile{
				Content: []byte(override),
				Perm:    0o644,
			}
			continue
		}

		if sourceWorkspace != "" {
			sourcePath := filepath.Join(sourceWorkspace, filepath.FromSlash(relPath))
			if data, err := os.ReadFile(sourcePath); err == nil {
				desired[relPath] = desiredWorkspaceFile{
					Content: data,
					Perm:    0o644,
				}
				continue
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}

		fallback := fallbackBootstrapContent(relPath)
		if strings.TrimSpace(fallback) == "" {
			continue
		}
		desired[relPath] = desiredWorkspaceFile{
			Content: []byte(fallback),
			Perm:    0o644,
		}
	}

	if sourceWorkspace == "" {
		return desired, nil
	}

	sourceSkillsDir := filepath.Join(sourceWorkspace, "skills")
	info, err := os.Stat(sourceSkillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return desired, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return desired, nil
	}

	err = filepath.WalkDir(sourceSkillsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(sourceSkillsDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(filepath.Join("skills", relPath))

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		perm := os.FileMode(0o644)
		if info, err := d.Info(); err == nil && info.Mode().Perm() != 0 {
			perm = info.Mode().Perm()
		}

		desired[relPath] = desiredWorkspaceFile{
			Content: data,
			Perm:    perm,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return desired, nil
}

func newBootstrapMetadata() workspaceBootstrapMetadata {
	return workspaceBootstrapMetadata{
		Version: bootstrapMetadataVersion,
		Files:   map[string]workspaceBootstrapFile{},
	}
}

func loadWorkspaceBootstrapMetadata(
	workspace string,
) (workspaceBootstrapMetadata, bool, string, error) {
	path := filepath.Join(workspace, bootstrapMetadataFilename)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newBootstrapMetadata(), false, "", nil
	}
	if err != nil {
		return workspaceBootstrapMetadata{}, false, "", err
	}

	meta := newBootstrapMetadata()
	if err := json.Unmarshal(data, &meta); err != nil {
		return newBootstrapMetadata(), true,
			fmt.Sprintf("invalid sync metadata in %s; treating workspace as legacy", bootstrapMetadataFilename), nil
	}
	if meta.Version == 0 {
		meta.Version = bootstrapMetadataVersion
	}
	if meta.Files == nil {
		meta.Files = map[string]workspaceBootstrapFile{}
	}
	return meta, true, "", nil
}

func saveWorkspaceBootstrapMetadata(
	workspace string,
	meta workspaceBootstrapMetadata,
	metaExisted bool,
) error {
	path := filepath.Join(workspace, bootstrapMetadataFilename)
	if len(meta.Files) == 0 {
		if metaExisted {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}

	meta.Version = bootstrapMetadataVersion
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func fileHashIfExists(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return hashBytes(data), true, nil
}

func writeManagedFile(path string, content []byte, perm os.FileMode) error {
	return fileutil.WriteFileAtomic(path, content, perm)
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortedDesiredPaths(files map[string]desiredWorkspaceFile) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedMetadataPaths(files map[string]workspaceBootstrapFile) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func pathKey(path string) string {
	cleaned := filepath.Clean(path)
	if os.PathSeparator == '\\' {
		return strings.ToLower(cleaned)
	}
	return cleaned
}
