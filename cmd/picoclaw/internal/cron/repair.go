package cron

import (
	"encoding/json"
	"fmt"

	"github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	cronservice "github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/spf13/cobra"
)

func newRepairCommand(path func() string) *cobra.Command {
	var options cronservice.RepairOptions
	var minimum int
	var stopped bool
	cmd := &cobra.Command{Use: "repair", Short: "Preview or repair selected cron routing (stop gateway before applying)", Args: cobra.NoArgs}
	cmd.Flags().StringVar(&options.ID, "id", "", "Select one job ID")
	cmd.Flags().StringVar(&options.ExpectedHash, "expected-hash", "", "SHA-256 from preview")
	cmd.Flags().BoolVar(&options.Apply, "apply", false, "Apply selected repair with backup")
	cmd.Flags().BoolVar(&stopped, "gateway-stopped", false, "Confirm the gateway using this data is stopped")
	cmd.Flags().StringVar(&options.Timezone, "timezone", "", "Set selected job timezone")
	cmd.Flags().IntVar(&minimum, "min-verified-sources", 0, "Set selected job news verification policy")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if options.Apply && !stopped {
			return fmt.Errorf("stop the gateway first, then pass --gateway-stopped")
		}
		if cmd.Flags().Changed("min-verified-sources") {
			options.MinVerifiedSources = &minimum
		}
		if options.ID == "" && (options.Timezone != "" || options.MinVerifiedSources != nil) {
			return fmt.Errorf("policy changes require --id")
		}
		cfg, err := internal.LoadConfig()
		if err != nil {
			return err
		}
		reports, hash, err := cronservice.Repair(path(), options, routing.NewRouteResolver(cfg))
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"sha256": hash, "applied": options.Apply, "jobs": reports})
	}
	return cmd
}
