package agent

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	agentpkg "github.com/sipeed/picoclaw/pkg/agent"
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

	workspaces, err := agentpkg.DiscoverDefaultSyncWorkspaces(cfg)
	if err != nil {
		return fmt.Errorf("discover sync workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		_, _ = fmt.Fprintln(w, "No existing agent workspaces found.")
		return nil
	}

	total := agentpkg.WorkspaceDefaultsSyncReport{}
	for _, workspace := range workspaces {
		report, err := agentpkg.SyncWorkspaceDefaults(
			workspace,
			&cfg.Agents.Defaults,
			agentpkg.WorkspaceDefaultsSyncOptions{
				DryRun:      dryRun,
				ForceLegacy: forceLegacy,
			},
		)
		if err != nil {
			return fmt.Errorf("sync %s: %w", workspace, err)
		}

		printSyncReport(w, report)
		total.Created = append(total.Created, report.Created...)
		total.Updated = append(total.Updated, report.Updated...)
		total.Adopted = append(total.Adopted, report.Adopted...)
		total.Deleted = append(total.Deleted, report.Deleted...)
		total.Preserved = append(total.Preserved, report.Preserved...)
		total.Conflicts = append(total.Conflicts, report.Conflicts...)
		total.Warnings = append(total.Warnings, report.Warnings...)
	}

	mode := "Applied"
	if dryRun {
		mode = "Dry run"
	}
	_, _ = fmt.Fprintf(
		w,
		"%s %d workspace(s): %d created, %d updated, %d adopted, %d deleted, %d preserved, %d conflict(s).\n",
		mode,
		len(workspaces),
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
