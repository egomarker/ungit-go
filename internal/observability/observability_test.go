package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestDefaultLogDirectoryIsExecutableRelative(t *testing.T) {
	got, err := resolveDirectory(nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	if got != filepath.Dir(executable) {
		t.Fatalf("default log directory=%q, want %q", got, filepath.Dir(executable))
	}
}

func TestFileLoggingRedactsSecretsAndWritesCorrelations(t *testing.T) {
	directory := t.TempDir()
	runtime, err := Start(Options{
		Directory:  &directory,
		Level:      "trace",
		MaxSizeMB:  1,
		MaxBackups: 2,
		MaxAgeDays: 1,
		Compress:   false,
		Version:    "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSocketID(WithActionID(WithRequestID(context.Background(), "request-1"), "action-1"), "socket-1")
	Info(ctx, "test.event", "request password=hunter2 token=abc123",
		"authorization", "Bearer top-secret",
		"remote", "https://alice:secret@example.test/repo.git?token=query-secret",
		"details", map[string]any{
			"password": "nested-secret",
			"safe":     "ok",
			"nested":   map[string]string{"api_key": "typed-map-secret", "note": "safe"},
		},
	)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(directory, LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"hunter2", "abc123", "top-secret", "alice:secret", "query-secret", "nested-secret", "typed-map-secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log leaked secret %q: %s", secret, text)
		}
	}
	var entry map[string]any
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatal(err)
	}
	if entry["event"] != "test.event" || entry["request_id"] != "request-1" || entry["action_id"] != "action-1" || entry["socket_id"] != "socket-1" {
		t.Fatalf("missing correlation fields: %#v", entry)
	}
	if goruntime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(directory, LogFileName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("log mode=%v", info.Mode().Perm())
		}
	}
}

func TestNewLogDirectoryIsOwnerOnly(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX directory modes")
	}
	directory := filepath.Join(t.TempDir(), "new-log-directory")
	logging, err := Start(Options{Directory: &directory, Level: "info", MaxBackups: 1, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := logging.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("new directory mode=%v", info.Mode().Perm())
	}
}

func TestExistingLogDirectoryPermissionsArePreserved(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX directory modes")
	}
	parent := t.TempDir()
	directory := filepath.Join(parent, "executable-directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	runtime, err := Start(Options{Directory: &directory, Level: "info", MaxBackups: 1, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("existing directory mode changed to %v", info.Mode().Perm())
	}
}

func TestRotationKeepsBoundedBackups(t *testing.T) {
	directory := t.TempDir()
	runtime, err := Start(Options{
		Directory:  &directory,
		Level:      "info",
		MaxSizeMB:  1,
		MaxBackups: 2,
		MaxAgeDays: 1,
		Version:    "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("x", 96<<10)
	for index := 0; index < 40; index++ {
		Info(context.Background(), "rotation.test", payload, "index", index)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	logFiles := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "ungit-go") && strings.Contains(entry.Name(), ".log") {
			logFiles++
		}
	}
	if logFiles > 3 {
		t.Fatalf("rotation retained %d log files; want at most active + 2 backups", logFiles)
	}
}

func TestPreviousUncleanRunIsReported(t *testing.T) {
	directory := t.TempDir()
	first, err := Start(Options{Directory: &directory, Level: "info", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.MarkRunning(); err != nil {
		t.Fatal(err)
	}
	// Simulate an abrupt process death: close only the writer, leaving run state unclean.
	if err := first.writer.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Start(Options{Directory: &directory, Level: "info", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.MarkRunning(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(directory, LogFileName))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	found := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry["event"] == "process.previous_run_unclean" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing process.previous_run_unclean event")
	}
}

func TestErrorFieldsDoNotExposeArbitraryErrorText(t *testing.T) {
	const canary = "arbitrary-repository-content-canary"
	fields := fmt.Sprint(ErrorFields(errors.New(canary)))
	if strings.Contains(fields, canary) {
		t.Fatalf("safe error fields leaked error text: %s", fields)
	}
	if !strings.Contains(fields, "error_class") || !strings.Contains(fields, "error_summary") {
		t.Fatalf("safe error fields missing diagnostics: %s", fields)
	}
}

func TestSanitizeDiagnosticPreservesErrorsAndRedactsCredentials(t *testing.T) {
	input := "\x1b[31mfatal: authentication failed for https://alice:url-secret@example.test/private.git?token=query-secret\x1b[0m\n" +
		"Authorization: Basic basic-secret\npassword=line-secret\n" +
		"remote token ghp_123456789012345678901234567890\n" +
		"-----BEGIN PRIVATE KEY-----\nprivate-key-secret\n-----END PRIVATE KEY-----\n" +
		"diagnostic detail\x00"
	got := SanitizeDiagnostic(input, 16<<10)
	for _, secret := range []string{"alice:url-secret", "query-secret", "basic-secret", "line-secret", "ghp_123456789012345678901234567890", "private-key-secret", "\x1b"} {
		if strings.Contains(got, secret) {
			t.Fatalf("sanitized diagnostic leaked %q: %s", secret, got)
		}
	}
	for _, expected := range []string{"fatal: authentication failed", "private.git", "diagnostic detail", "<redacted-private-key>"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("sanitized diagnostic is missing %q: %s", expected, got)
		}
	}
	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("sanitized diagnostic retained a control character: %q", got)
	}
}

func TestSanitizeDiagnosticIsBoundedWithoutSplittingUTF8(t *testing.T) {
	got := SanitizeDiagnostic(strings.Repeat("é", 100), 64)
	if len(got) > 64 {
		t.Fatalf("sanitized diagnostic length=%d, want <=64", len(got))
	}
	if !strings.HasSuffix(got, "<truncated>") || strings.ToValidUTF8(got, "") != got {
		t.Fatalf("unexpected truncated diagnostic: %q", got)
	}
}

func TestRedactArgsHidesCredentialHelpers(t *testing.T) {
	args := RedactArgs([]string{
		"-c",
		"credential.helper=!/tmp/ungit-go credential-helper 4 http://127.0.0.1:8448 github.com",
		"push",
		"https://alice:password@example.test/repo.git?token=secret",
	})
	text := strings.Join(args, " ")
	for _, secret := range []string{"credential-helper 4", "alice:password", "token=secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("redacted args leaked %q: %s", secret, text)
		}
	}
}

func TestDefaultLoggerIsSilentBeforeStart(t *testing.T) {
	// This mostly protects package users and tests from accidentally inheriting
	// stderr logging before the executable initializes its file logger.
	if slog.Default() == nil {
		t.Fatal("default logger is nil")
	}
}
