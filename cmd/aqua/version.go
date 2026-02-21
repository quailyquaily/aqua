package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildDate    = "unknown"
)

func newVersionCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print Aqua version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			view := map[string]string{
				"version": buildVersion,
				"commit":  buildCommit,
				"date":    buildDate,
			}
			if outputJSON {
				return writeJSON(cmd.OutOrStdout(), view)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "version: %s\ncommit: %s\ndate: %s\n", buildVersion, buildCommit, buildDate)
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print as JSON")
	return cmd
}
