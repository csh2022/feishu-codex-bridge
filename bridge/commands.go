package bridge

import (
	"path/filepath"
	"strings"
)

type Command struct {
	Kind string
	Arg  string
}

const (
	CommandSwitchDir = "switch_dir"
	CommandShowDir   = "show_dir"
	CommandHelp      = "help"
	CommandClear     = "clear"
	CommandQueue     = "queue"
)

func ParseCommand(content string) (Command, bool) {
	s := strings.TrimSpace(content)
	if s == "" {
		return Command{}, false
	}

	if s == "/help" || s == "/h" {
		return Command{Kind: CommandHelp}, true
	}

	if s == "/clear" || s == "/c" {
		return Command{Kind: CommandClear}, true
	}

	if s == "/queue" || s == "/q" {
		return Command{Kind: CommandQueue}, true
	}

	if s == "/pwd" {
		return Command{Kind: CommandShowDir}, true
	}

	if strings.HasPrefix(s, "/cd ") {
		arg := strings.TrimSpace(strings.TrimPrefix(s, "/cd "))
		if arg == "" {
			return Command{}, false
		}
		cleaned := filepath.Clean(arg)
		return Command{Kind: CommandSwitchDir, Arg: cleaned}, true
	}

	return Command{}, false
}
