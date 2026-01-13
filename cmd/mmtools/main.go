package main

import (
	"os"

	"github.com/joho/godotenv"

	"github.com/user/mattermost-tools/internal/commands"
)

func main() {
	_ = godotenv.Load()

	if err := commands.Execute(); err != nil {
		os.Exit(1)
	}
}
