package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egomarker/ungit-go/internal/config"
	"github.com/egomarker/ungit-go/internal/observability"
)

func TestRunnerLogDoesNotLeakGitArgumentsOrOutput(t *testing.T) {
	directory := t.TempDir()
	logging, err := observability.Start(observability.Options{
		Directory: &directory, Level: "trace", MaxSizeMB: 1, MaxBackups: 1, Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	runner := NewRunner(cfg)
	if _, err := runner.Run(context.Background(), Command{RepoPath: directory, Args: []string{"init", "--quiet"}}); err != nil {
		t.Fatal(err)
	}
	const canary = "git-argument-and-output-secret-canary"
	_, _ = runner.Run(context.Background(), Command{RepoPath: directory, Args: []string{"rev-parse", canary}})
	if err := logging.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, observability.LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), canary) {
		t.Fatalf("Git log leaked command argument or output: %s", data)
	}
	for _, event := range []string{"git.command.started", "git.command.failed"} {
		if !strings.Contains(string(data), event) {
			t.Fatalf("Git log is missing %s: %s", event, data)
		}
	}
}

func TestWatcherCommandsSuppressRoutineLifecycleButKeepFailures(t *testing.T) {
	directory := t.TempDir()
	logging, err := observability.Start(observability.Options{
		Directory: &directory, Level: "trace", MaxSizeMB: 1, MaxBackups: 1, Version: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	runner := NewRunner(cfg)
	ctx := observability.WithWatcherID(context.Background(), "watch-test")
	if _, err := runner.Run(ctx, Command{RepoPath: directory, Args: []string{"init", "--quiet"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(ctx, Command{RepoPath: directory, Args: []string{"status", "--short"}}); err != nil {
		t.Fatal(err)
	}
	_, _ = runner.Run(ctx, Command{RepoPath: directory, Args: []string{"rev-parse", "missing-ref"}})
	if err := logging.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, observability.LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, event := range []string{"git.command.queued", "git.command.started", "git.command.completed"} {
		if strings.Contains(text, event) {
			t.Fatalf("watcher log contains routine lifecycle event %s: %s", event, text)
		}
	}
	for _, expected := range []string{"git.command.failed", "watch-test"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("watcher log is missing %s: %s", expected, text)
		}
	}
}

func TestGitCommandFieldsExcludeArgumentValues(t *testing.T) {
	const (
		messageSecret = "commit-message-secret"
		remoteSecret  = "remote-password-secret"
	)
	command := Command{
		RepoPath: "/repository",
		Args: []string{
			"push",
			"--force-with-lease=refs/heads/main:secret-sha",
			"https://alice:" + remoteSecret + "@example.test/private.git",
			messageSecret,
		},
	}
	args := append(append([]string{}, baseConfigArgs...), command.Args...)
	fields := gitCommandFields(command, args, "operation-1", "command-1")
	text := fmt.Sprint(fields)
	for _, secret := range []string{messageSecret, remoteSecret, "secret-sha", "private.git"} {
		if strings.Contains(text, secret) {
			t.Fatalf("Git command fields leaked %q: %s", secret, text)
		}
	}
	for _, expected := range []string{"git_operation", "push", "option_count", "operation-1", "command-1"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Git command fields missing %q: %s", expected, text)
		}
	}

	command.Args = []string{"private-command-secret", "private-argument-secret"}
	fields = gitCommandFields(command, command.Args, "operation-2", "command-2")
	text = fmt.Sprint(fields)
	if strings.Contains(text, "private-command-secret") || strings.Contains(text, "private-argument-secret") {
		t.Fatalf("unknown Git command leaked argument values: %s", text)
	}
	if !strings.Contains(text, "other") {
		t.Fatalf("unknown Git command was not classified safely: %s", text)
	}
}
