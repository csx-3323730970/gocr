// Package cli wires cobra commands. No business logic lives here.
package cli

import "github.com/spf13/cobra"

// version is injected at build time via -ldflags "-X gocr/internal/cli.version=...".
var version = "dev"

func Execute() error {
	root := &cobra.Command{
		Use:           "gocr",
		Short:         "AI code review service with line-level anchoring",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newReviewCmd(), newServeCmd())
	return root.Execute()
}
