package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/cmd/soloqueue/cli"
	"github.com/xiaobaitu/soloqueue/internal/infra/localonnx"
)

// Release builds override this with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__internal_laya" {
		os.Exit(localonnx.InternalLayaMain(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == "__internal_embedding" {
		os.Exit(localonnx.InternalEmbeddingMain(os.Args[2:]))
	}
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

Use 'soloqueue start' to start the backend and browser consoles together.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(cli.VersionCmd(version))
	root.AddCommand(cli.ServeCmd(version))
	root.AddCommand(cli.StartCmd(version))
	root.AddCommand(cli.WebCmd(version))
	root.AddCommand(cli.WechatCmd(version))
	root.AddCommand(cli.MemoryCmd())
	root.AddCommand(cli.SkillsCmd())

	return root
}
