package archstate

import (
	"strings"
	"testing"
)

func TestHelpAliasesPrintCanonicalHelp(t *testing.T) {
	env := newTestEnv(t)

	if err := env.run("help"); err != nil {
		t.Fatal(err)
	}
	want := env.stdout.String()
	for _, text := range []string{
		"Archstate tracks explicit Arch packages",
		"Usage:\n  archstate <command> [options]",
		"Repo:\n  ~/.config/archstate-src",
		"Common workflow:",
		"Commands:",
		"  init       Create repo state and install archstate to ~/.local/bin.",
		"  packages   Fuzzy-select explicit packages to remove.",
		"  snapshot   Save, list, restore, or remove repo-state snapshots.",
		"  service    Manage the optional systemd user sync timer.",
		"archstate bootstrap --dry-run",
		"Command help:\n  archstate help <command>",
		"archstate <command> --help",
		"Examples:",
	} {
		if !strings.Contains(want, text) {
			t.Fatalf("help output missing %q:\n%s", text, want)
		}
	}

	for _, args := range [][]string{
		nil,
		{"--help"},
		{"-h"},
	} {
		if err := env.run(args...); err != nil {
			t.Fatalf("run(%v): %v", args, err)
		}
		if got := env.stdout.String(); got != want {
			t.Fatalf("help alias %v did not match canonical help\nwant:\n%s\ngot:\n%s", args, want, got)
		}
	}
}

func TestCommandTopicHelp(t *testing.T) {
	env := newTestEnv(t)

	if err := env.run("help", "snapshot"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Usage:\n  archstate snapshot save <name>",
		"archstate snapshot list [--manual|--auto]",
		"Manual snapshots are kept until removed.",
	} {
		if !strings.Contains(env.stdout.String(), want) {
			t.Fatalf("snapshot help missing %q:\n%s", want, env.stdout.String())
		}
	}

	if err := env.run("help", "config"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Usage:\n  archstate config add <name>",
		"archstate config list",
		"list        Show currently tracked config entries.",
	} {
		if !strings.Contains(env.stdout.String(), want) {
			t.Fatalf("config help missing %q:\n%s", want, env.stdout.String())
		}
	}

	if err := env.run("help", "home"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Usage:\n  archstate home add <name>",
		"archstate home list",
		"list        Show currently tracked home entries.",
	} {
		if !strings.Contains(env.stdout.String(), want) {
			t.Fatalf("home help missing %q:\n%s", want, env.stdout.String())
		}
	}
}

func TestHelpCommandOrderFollowsUserFlow(t *testing.T) {
	env := newTestEnv(t)

	if err := env.run("help"); err != nil {
		t.Fatal(err)
	}
	out := env.stdout.String()
	commandsStart := strings.Index(out, "Commands:")
	if commandsStart == -1 {
		t.Fatalf("help output missing Commands section:\n%s", out)
	}
	out = out[commandsStart:]
	assertHelpOrder(t, out,
		"  init",
		"  install",
		"  sync",
		"  packages",
		"  status",
		"  config",
		"  home",
		"  snapshot",
		"  bootstrap",
		"  doctor",
		"  service",
	)
}

func TestBootstrapFlagHelpPrintsBootstrapTopic(t *testing.T) {
	env := newTestEnv(t)

	for _, args := range [][]string{
		{"bootstrap", "--help"},
		{"bootstrap", "-h"},
	} {
		if err := env.run(args...); err != nil {
			t.Fatalf("run(%v): %v", args, err)
		}
		got := env.stdout.String()
		for _, want := range []string{
			"Usage:\n  archstate bootstrap --dry-run",
			"--aur-helper paru|yay",
			"--adopt",
			"--restore",
			"--packages",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("bootstrap help %v missing %q:\n%s", args, want, got)
			}
		}
		if strings.Contains(got, "Commands:\n  init") {
			t.Fatalf("bootstrap help should not print top-level overview:\n%s", got)
		}
		if env.stderr.Len() != 0 {
			t.Fatalf("bootstrap help wrote to stderr: %s", env.stderr.String())
		}
	}
}

func TestCommandFlagHelpMatchesTopicHelp(t *testing.T) {
	topics := []string{
		"init",
		"install",
		"sync",
		"packages",
		"status",
		"config",
		"home",
		"snapshot",
		"bootstrap",
		"doctor",
		"service",
	}

	for _, topic := range topics {
		env := newTestEnv(t)
		if err := env.run("help", topic); err != nil {
			t.Fatalf("help %s: %v", topic, err)
		}
		want := env.stdout.String()

		for _, flag := range []string{"--help", "-h"} {
			if err := env.run(topic, flag); err != nil {
				t.Fatalf("%s %s: %v", topic, flag, err)
			}
			if got := env.stdout.String(); got != want {
				t.Fatalf("%s %s did not match topic help\nwant:\n%s\ngot:\n%s", topic, flag, want, got)
			}
		}
	}
}

func assertHelpOrder(t *testing.T, out string, snippets ...string) {
	t.Helper()
	last := -1
	for _, snippet := range snippets {
		idx := strings.Index(out, snippet)
		if idx == -1 {
			t.Fatalf("help output missing %q:\n%s", snippet, out)
		}
		if idx <= last {
			t.Fatalf("help output has %q out of order:\n%s", snippet, out)
		}
		last = idx
	}
}

func TestUnknownCommandFailsClearly(t *testing.T) {
	env := newTestEnv(t)

	err := env.run("wat")
	if err == nil {
		t.Fatal("expected unknown command error")
	}
	if !strings.Contains(err.Error(), `unknown command "wat"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
