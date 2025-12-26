package main

import (
	"fmt"
	"os"

	"github.com/piwi3910/nebulaio/cmd/nebulaio-cli/commands"
	"github.com/spf13/cobra"
)

var (
	// Version is set at build time
	Version = "dev"
	// Commit is set at build time
	Commit = "none"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "nebulaio-cli",
		Short: "NebulaIO CLI - S3-compatible object storage client",
		Long: `NebulaIO CLI is a command-line interface for interacting with NebulaIO
and other S3-compatible object storage services.

Configure your endpoint and credentials:
  nebulaio-cli config set endpoint http://localhost:9000
  nebulaio-cli config set access-key YOUR_ACCESS_KEY
  nebulaio-cli config set secret-key YOUR_SECRET_KEY

Or use environment variables:
  NEBULAIO_ENDPOINT
  NEBULAIO_ACCESS_KEY
  NEBULAIO_SECRET_KEY
  AWS_ACCESS_KEY_ID
  AWS_SECRET_ACCESS_KEY`,
		Version: fmt.Sprintf("%s (commit: %s)", Version, Commit),
	}

	// Add sub-commands
	rootCmd.AddCommand(commands.NewConfigCmd())
	rootCmd.AddCommand(commands.NewBucketCmd())
	rootCmd.AddCommand(commands.NewObjectCmd())
	rootCmd.AddCommand(commands.NewAdminCmd())

	// Add aliases for common operations
	rootCmd.AddCommand(commands.NewMakeBucketCmd())
	rootCmd.AddCommand(commands.NewRemoveBucketCmd())
	rootCmd.AddCommand(commands.NewListCmd())
	rootCmd.AddCommand(commands.NewCopyCmd())
	rootCmd.AddCommand(commands.NewRemoveCmd())
	rootCmd.AddCommand(commands.NewCatCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
