package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/cmd/soloqueue/cli"
)

const version = "0.1.0"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "soloqueue",
		Short: "SoloQueue — AI multi-agent collaboration tool",
		Long: `SoloQueue is an AI multi-agent collaboration tool built on the Actor model.

Use 'soloqueue serve' to start the local HTTP/WebSocket server.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(cli.VersionCmd(version))
	root.AddCommand(cli.ServeCmd(version))
	root.AddCommand(cli.CleanupCmd())

	return root
}
