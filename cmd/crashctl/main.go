package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version   = "0.0.1"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:     "crashctl",
		Short:   "Self-hosted error tracking and Kubernetes crash detection",
		Version: version,
	}

	root.AddCommand(
		newServeCmd(),
		newProjectCmd(),
		newVersionCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the crashctl server (web UI + API + K8s watcher)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("crashctl v" + version)
			fmt.Println("server not yet implemented (Step 4)")
			return nil
		},
	}
}

func newProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Manage projects",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "create",
			Short: "Create a project and print its DSN",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println("project create not yet implemented (Step 3)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List all projects",
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Println("project list not yet implemented (Step 3)")
				return nil
			},
		},
	)

	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("crashctl %s (commit: %s, built: %s)\n", version, commit, buildDate)
		},
	}
}
