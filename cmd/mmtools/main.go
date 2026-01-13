package main

import (
	"os"

	"github.com/user/mattermost-tools/internal/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
