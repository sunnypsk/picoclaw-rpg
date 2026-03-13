package agent

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/cmd/picoclaw/internal/onboard"
	agentpkg "github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newSyncDefaultsCommand() *cobra.Command {
	var dryRun bool
	var forceLegacy bool

	cmd := &cobra.Command{
		Use:   "sync-defaults",
		Short: "Sync managed default workspace files into existing agent workspaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return syncDefaultsCmd(cmd.OutOrStdout(), dryRun, forceLegacy)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show planned changes without writing files")
	cmd.Flags().BoolVar(&forceLegacy, "force-legacy", false,
		"Overwrite existing managed files that predate sync metadata")

	return cmd
}

func syncDefaultsCmd(w io.Writer, dryRun, forceLegacy bool) error {
	cfg, err := internal.LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	embeddedWorkspace, cleanup, err := prepareEmbeddedWorkspaceSource()
	if err != nil {
		return err
	}
	defer cleanup()

	workspaces, err := agentpkg.DiscoverDefaultSyncWorkspaces(cfg)
	if err != nil {
		return fmt.Errorf("discover sync workspaces: %w", err)
	}

	total := agentpkg.WorkspaceDefaultsSyncReport{}
	defaultReport, err := syncDefaultWorkspaceFromCurrentBinary(
		cfg,
		embeddedWorkspace,
		dryRun,
		forceLegacy,
	)
	if err != nil {
		return err
	}
	printSyncReport(w, defaultReport)
	accumulateSyncReport(&total, defaultReport)

	if len(workspaces) == 0 {
		mode := "Applied"
		if dryRun {
			mode = "Dry run"
		}
		_, _ = fmt.Fprintf(
			w,
			"%s 1 workspace(s): %d created, %d updated, %d adopted, %d deleted, %d preserved, %d conflict(s).\n",
			mode,
			len(total.Created),
			len(total.Updated),
			len(total.Adopted),
			len(total.Deleted),
			len(total.Preserved),
			len(total.Conflicts),
		)
		return nil
	}

	childSourceWorkspace := cfg.WorkspacePath()
	childSourceCleanup := func() {}
	if dryRun {
		childSourceWorkspace, childSourceCleanup, err = prepareSimulatedDefaultWorkspace(cfg, embeddedWorkspace, forceLegacy)
		if err != nil {
			return err
		}
		defer childSourceCleanup()
	}

	workspaceDefaults := cfg.Agents.Defaults
	workspaceDefaults.Workspace = childSourceWorkspace
	for _, workspace := range workspaces {
		report, err := agentpkg.SyncWorkspaceDefaults(
			workspace,
			&workspaceDefaults,
			agentpkg.WorkspaceDefaultsSyncOptions{
				DryRun:      dryRun,
				ForceLegacy: forceLegacy,
			},
		)
		if err != nil {
			return fmt.Errorf("sync %s: %w", workspace, err)
		}

		printSyncReport(w, report)
		accumulateSyncReport(&total, report)
	}

	mode := "Applied"
	if dryRun {
		mode = "Dry run"
	}
	_, _ = fmt.Fprintf(
		w,
		"%s %d workspace(s): %d created, %d updated, %d adopted, %d deleted, %d preserved, %d conflict(s).\n",
		mode,
		len(workspaces)+1,
		len(total.Created),
		len(total.Updated),
		len(total.Adopted),
		len(total.Deleted),
		len(total.Preserved),
		len(total.Conflicts),
	)

	return nil
}

func printSyncReport(w io.Writer, report agentpkg.WorkspaceDefaultsSyncReport) {
	_, _ = fmt.Fprintf(w, "%s\n", report.Workspace)
	printReportSection(w, "created", report.Created)
	printReportSection(w, "updated", report.Updated)
	printReportSection(w, "adopted", report.Adopted)
	printReportSection(w, "deleted", report.Deleted)
	printReportSection(w, "preserved", report.Preserved)
	printReportSection(w, "conflicts", report.Conflicts)
	printReportSection(w, "warnings", report.Warnings)
	if !report.HasActions() && len(report.Warnings) == 0 {
		_, _ = fmt.Fprintln(w, "  no changes")
	}
}

func printReportSection(w io.Writer, label string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "  %s: %s\n", label, values[0])
	for _, value := range values[1:] {
		_, _ = fmt.Fprintf(w, "  %s: %s\n", label, value)
	}
}

func syncDefaultWorkspaceFromCurrentBinary(
	cfg *config.Config,
	sourceWorkspace string,
	dryRun bool,
	forceLegacy bool,
) (agentpkg.WorkspaceDefaultsSyncReport, error) {
	defaults := cfg.Agents.Defaults
	defaults.Workspace = sourceWorkspace
	report, err := agentpkg.SyncWorkspaceDefaults(
		cfg.WorkspacePath(),
		&defaults,
		agentpkg.WorkspaceDefaultsSyncOptions{
			DryRun:      dryRun,
			ForceLegacy: forceLegacy,
		},
	)
	if err != nil {
		return agentpkg.WorkspaceDefaultsSyncReport{}, fmt.Errorf("sync default workspace: %w", err)
	}
	return report, nil
}

func prepareEmbeddedWorkspaceSource() (string, func(), error) {
	tmpEmbedded, err := os.MkdirTemp("", "picoclaw-sync-defaults-embedded-*")
	if err != nil {
		return "", nil, fmt.Errorf("create embedded defaults workspace: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tmpEmbedded)
	}

	if err := onboard.CopyEmbeddedWorkspaceTemplates(tmpEmbedded); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("prepare embedded defaults workspace: %w", err)
	}

	return tmpEmbedded, cleanup, nil
}

func prepareSimulatedDefaultWorkspace(
	cfg *config.Config,
	embeddedWorkspace string,
	forceLegacy bool,
) (string, func(), error) {
	tmpComposite, err := os.MkdirTemp("", "picoclaw-sync-defaults-composite-*")
	if err != nil {
		return "", nil, fmt.Errorf("create composite defaults workspace: %w", err)
	}

	compositeCleanup := func() {
		_ = os.RemoveAll(tmpComposite)
	}

	if err := copyDirContents(cfg.WorkspacePath(), tmpComposite); err != nil {
		compositeCleanup()
		return "", nil, fmt.Errorf("copy current default workspace: %w", err)
	}

	compositeDefaults := cfg.Agents.Defaults
	compositeDefaults.Workspace = embeddedWorkspace
	if _, err := agentpkg.SyncWorkspaceDefaults(
		tmpComposite,
		&compositeDefaults,
		agentpkg.WorkspaceDefaultsSyncOptions{
			DryRun:      false,
			ForceLegacy: forceLegacy,
		},
	); err != nil {
		compositeCleanup()
		return "", nil, fmt.Errorf("prepare composite managed source workspace: %w", err)
	}

	return tmpComposite, compositeCleanup, nil
}

func accumulateSyncReport(total *agentpkg.WorkspaceDefaultsSyncReport, report agentpkg.WorkspaceDefaultsSyncReport) {
	total.Created = append(total.Created, report.Created...)
	total.Updated = append(total.Updated, report.Updated...)
	total.Adopted = append(total.Adopted, report.Adopted...)
	total.Deleted = append(total.Deleted, report.Deleted...)
	total.Preserved = append(total.Preserved, report.Preserved...)
	total.Conflicts = append(total.Conflicts, report.Conflicts...)
	total.Warnings = append(total.Warnings, report.Warnings...)
}

func copyDirContents(sourceDir, targetDir string) error {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	info, err := os.Stat(sourceDir)
	if os.IsNotExist(err) || !info.IsDir() {
		return nil
	}
	if err != nil {
		return err
	}

	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil || relPath == "." {
			return err
		}

		targetPath := filepath.Join(targetDir, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		perm := os.FileMode(0o644)
		if info, err := d.Info(); err == nil && info.Mode().Perm() != 0 {
			perm = info.Mode().Perm()
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, perm)
	})
}
