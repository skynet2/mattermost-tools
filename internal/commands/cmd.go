package commands

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mmtools",
	Short: "Mattermost tools CLI",
}

func Execute() error {
	return rootCmd.Execute()
}
