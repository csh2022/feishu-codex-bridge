package bridge

import "testing"

func TestParseCommand_ShowDir(t *testing.T) {
	for _, in := range []string{"/pwd", "  /pwd  "} {
		cmd, ok := ParseCommand(in)
		if !ok {
			t.Fatalf("expected ok for %q", in)
		}
		if cmd.Kind != CommandShowDir {
			t.Fatalf("expected %s for %q, got %s", CommandShowDir, in, cmd.Kind)
		}
	}
}

func TestParseCommand_Help(t *testing.T) {
	for _, in := range []string{"/help", "  /help  ", "/h"} {
		cmd, ok := ParseCommand(in)
		if !ok {
			t.Fatalf("expected ok for %q", in)
		}
		if cmd.Kind != CommandHelp {
			t.Fatalf("expected %s for %q, got %s", CommandHelp, in, cmd.Kind)
		}
	}
}

func TestParseCommand_Clear(t *testing.T) {
	for _, in := range []string{"/clear", "  /clear  ", "/c"} {
		cmd, ok := ParseCommand(in)
		if !ok {
			t.Fatalf("expected ok for %q", in)
		}
		if cmd.Kind != CommandClear {
			t.Fatalf("expected %s for %q, got %s", CommandClear, in, cmd.Kind)
		}
	}
}

func TestParseCommand_Queue(t *testing.T) {
	for _, in := range []string{"/queue", "  /queue  ", "/q"} {
		cmd, ok := ParseCommand(in)
		if !ok {
			t.Fatalf("expected ok for %q", in)
		}
		if cmd.Kind != CommandQueue {
			t.Fatalf("expected %s for %q, got %s", CommandQueue, in, cmd.Kind)
		}
	}
}

func TestParseCommand_Status(t *testing.T) {
	for _, in := range []string{"/status", "  /status  ", "/s"} {
		cmd, ok := ParseCommand(in)
		if !ok {
			t.Fatalf("expected ok for %q", in)
		}
		if cmd.Kind != CommandStatus {
			t.Fatalf("expected %s for %q, got %s", CommandStatus, in, cmd.Kind)
		}
	}
}

func TestParseCommand_Reset(t *testing.T) {
	for _, in := range []string{"/reset", "  /reset  ", "/r"} {
		cmd, ok := ParseCommand(in)
		if !ok {
			t.Fatalf("expected ok for %q", in)
		}
		if cmd.Kind != CommandReset {
			t.Fatalf("expected %s for %q, got %s", CommandReset, in, cmd.Kind)
		}
	}
}

func TestParseCommand_SwitchDir(t *testing.T) {
	cmd, ok := ParseCommand("/cd /tmp/foo")
	if !ok {
		t.Fatalf("expected ok")
	}
	if cmd.Kind != CommandSwitchDir {
		t.Fatalf("expected %s, got %s", CommandSwitchDir, cmd.Kind)
	}
	if cmd.Arg != "/tmp/foo" {
		t.Fatalf("expected arg /tmp/foo, got %q", cmd.Arg)
	}
}

func TestParseCommand_WorkdirWithoutArg_NotCommand(t *testing.T) {
	for _, in := range []string{"/workdir", "  /workdir  ", "/workdir   ", "/w", "  /w  ", "/w   ", "/w /tmp"} {
		if _, ok := ParseCommand(in); ok {
			t.Fatalf("expected not a command for %q", in)
		}
	}
}
