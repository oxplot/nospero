package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestColorAlwaysOverridesNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("NOSPERO_COLOR", "")

	if !colorEnabled(&bytes.Buffer{}, []string{"--color=always"}) {
		t.Fatal("expected --color=always to enable color even when NO_COLOR is set")
	}
}

func TestColorNeverDisablesColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("NOSPERO_COLOR", "")

	if colorEnabled(&bytes.Buffer{}, []string{"--color=never"}) {
		t.Fatal("expected --color=never to disable color")
	}
}

func TestHelpTemplateFunctionsUseXcodePaletteWhenForced(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("NOSPERO_COLOR", "")

	funcs := helpTemplateFuncs(&bytes.Buffer{}, []string{"--color=always"})
	section := funcs["section"].(func(string) string)
	got := section("usage")
	if !strings.Contains(got, "\x1b[38;2;31;95;173") {
		t.Fatalf("got %q, want Xcode selection blue section color", got)
	}
	for _, underlineCode := range []string{"4", "24"} {
		if hasSGRCode(got, underlineCode) {
			t.Fatalf("got %q, want styling without underline code %s", got, underlineCode)
		}
	}
}

func hasSGRCode(text string, code string) bool {
	return strings.Contains(text, "["+code+"m") ||
		strings.Contains(text, "["+code+";") ||
		strings.Contains(text, ";"+code+"m") ||
		strings.Contains(text, ";"+code+";")
}

func TestVisibleUserCommandsFiltersBuiltInHelpCommand(t *testing.T) {
	app := &cli.App{
		Commands: []*cli.Command{
			{Name: "status", Usage: "read printer status"},
			{Name: "help", Usage: "Shows a list of commands or help for one command"},
		},
	}

	got := visibleUserCommands(app)
	if len(got) != 1 || got[0].Name != "status" {
		t.Fatalf("got %#v, want only user-facing commands", got)
	}
}
