# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make build      # Build binary + generate shell completions
make test       # Run all tests
make generate   # Generate mocks (mockgen)
make install    # Symlink binary to /usr/local/bin
make clean      # Remove build artifacts
```

Run a single test:
```bash
go test -run TestFunctionName ./pkg/mattermost/...
```

## Architecture

CLI tool using Cobra for command structure. Three layers:

```
cmd/mmtools/main.go            # Entry point
internal/commands/             # Cobra commands (private)
    cmd.go                     # Root command, global flags, factory functions
    completion/                # Shell completion generator
pkg/                           # Reusable API clients
    mattermost/                # Mattermost REST API client
```

## Mattermost Authentication

Use Mattermost Personal Access Token or session token for API authentication:

```go
client := mattermost.NewClient(
    os.Getenv("MATTERMOST_URL"),
    os.Getenv("MATTERMOST_TOKEN"),
)
```

## Adding a New Command

### Checklist

- [ ] Create command implementation
- [ ] Register command in `cmd.go`
- [ ] Update documentation (this file)
- [ ] Run linter and tests
- [ ] Build and verify

### Step 1: Create Command

Create `internal/commands/<name>/<name>.go`:
```go
package mycommand

type ClientFactory func() (*mattermost.Client, error)

func NewCommand(factory ClientFactory, debug *bool) *cobra.Command {
    return &cobra.Command{
        Use:   "mycommand",
        Short: "Description",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runMyCommand(factory, *debug)
        },
    }
}
```

### Step 2: Register Command

Update `internal/commands/cmd.go`:
- Add import for the new command package
- Add factory function if command needs a new API client
- Call `cmd.AddCommand(mycommand.NewCommand(...))`

### Step 3: Update Documentation

Update `CLAUDE.md`:
- Add command to **Architecture** section under `internal/commands/`
- Add new environment variables to **Environment Variables** table (if any)

Update `README.md`:
- Add usage examples for the new command
- Document any new flags or options

### Step 4: Validate

```bash
go vet ./...                    # Static analysis
go build ./...                  # Verify compilation
make test                       # Run all tests
make build                      # Build binary + completions
```

### Step 5: Verify Shell Completions

After `make build`, verify completions are generated:
```bash
./bin/mmtools completion zsh   # Should include new command
```

## Adding a New API Client

### Checklist

- [ ] Create client package structure
- [ ] Add factory function in `cmd.go`
- [ ] Generate mocks
- [ ] Update documentation (this file)
- [ ] Run linter and tests

### Step 1: Create Client Package

Create `pkg/<name>/client.go` with `HTTPDoer` interface for testability:
```go
//go:generate mockgen -destination=mocks/http_doer_mock.go -package=mocks mattermost-tools/pkg/<name> HTTPDoer

type HTTPDoer interface {
    Do(req *http.Request) (*http.Response, error)
}
```

Create `pkg/<name>/types.go` for API response structures.

Create API method files (e.g., `channels.go`, `posts.go`).

### Step 2: Register Factory

Add factory function in `internal/commands/cmd.go`:
```go
func MyClientFactory() (*mypkg.Client, error) {
    return mypkg.NewClient(
        os.Getenv("MY_ENV_VAR"),
        debug,
    )
}
```

### Step 3: Generate Mocks

```bash
make generate
```

### Step 4: Update Documentation

Update `CLAUDE.md`:
- Add package to **Architecture** section under `pkg/`
- Add new environment variables to **Environment Variables** table

### Step 5: Validate

```bash
go vet ./...
make test
make build
```

## Environment Variables

| Variable | Command | Description |
|----------|---------|-------------|
| `MATTERMOST_URL` | all | Mattermost server URL |
| `MATTERMOST_TOKEN` | all | Mattermost Personal Access Token |

## Watch Mode Pattern

Commands supporting watch mode follow this pattern:
- Capture baseline on first run
- Clear screen with ANSI codes (`\033[H\033[2J`)
- Calculate and display deltas on subsequent runs
- Sleep for interval between iterations

## TUI Design Guide

Interactive terminal UIs use Bubble Tea framework. Follow these patterns for consistency.

### Dependencies

```go
import (
    "github.com/charmbracelet/bubbletea"      // TUI framework
    "github.com/charmbracelet/bubbles/viewport" // Scrollable content
    "github.com/charmbracelet/bubbles/key"      // Key bindings
    "github.com/charmbracelet/lipgloss"         // Styling
)
```

### Color Palette

```go
// Standard colors - use consistently across all TUIs
var (
    colorGray    = lipgloss.Color("244")  // Timestamps, secondary text
    colorBlue    = lipgloss.Color("75")   // Info, method names, labels
    colorYellow  = lipgloss.Color("220")  // Warnings, file paths, numbers
    colorRed     = lipgloss.Color("196")  // Errors, exceptions
    colorMagenta = lipgloss.Color("201")  // Critical
    colorPink    = lipgloss.Color("212")  // Headers
    colorPurple  = lipgloss.Color("170")  // Selected item marker
)
```

### Keyboard Navigation

Standard key bindings (keep consistent):

| Key | Action |
|-----|--------|
| `↑`/`k` | Move up |
| `↓`/`j` | Move down |
| `Tab` | Switch tabs |
| `Enter` | View details |
| `Esc` | Back to list |
| `o` | Open in browser |
| `q`/`Ctrl+C` | Quit |
| `PgUp`/`PgDn` | Scroll in detail view |
| `1-9` | Jump to numbered item |

### Running TUI

```go
func runTUI(data []Item) error {
    m := newModel()
    m.items = data

    p := tea.NewProgram(m, tea.WithAltScreen())
    _, err := p.Run()
    return err
}
```

### Adding --ui Flag

```go
var useUI bool

cmd.Flags().BoolVar(&useUI, "ui", false, "Interactive TUI mode")

// In RunE:
if useUI {
    return runTUI(data)
}
// ... normal output
```
