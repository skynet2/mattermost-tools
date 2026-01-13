package commands

import (
	"github.com/spf13/cobra"

	"github.com/user/mattermost-tools/internal/commands/changes"
	"github.com/user/mattermost-tools/internal/commands/prs"
	"github.com/user/mattermost-tools/internal/commands/serve"
)

var rootCmd = &cobra.Command{
	Use:   "mmtools",
	Short: "Mattermost tools CLI",
}

func init() {
	rootCmd.AddCommand(prs.NewCommand())
	rootCmd.AddCommand(serve.NewCommand())
	rootCmd.AddCommand(changes.NewCommand())
}

func Execute() error {
	return rootCmd.Execute()
}
