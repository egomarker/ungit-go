package observability

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

const (
	LogFileName = "ungit-go.log"
	LevelTrace  = slog.Level(-8)
)

type Options struct {
	Directory  *string
	Level      string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
	Version    string
}

type Runtime struct {
	path        string
	statePath   string
	writer      *lumberjack.Logger
	runID       string
	started     time.Time
	stateActive bool
}

type runState struct {
	RunID     string    `json:"runId"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`
	Clean     bool      `json:"clean"`
}

type contextKey uint8

const (
	requestIDKey contextKey = iota
	actionIDKey
	socketIDKey
	watcherIDKey
)

var idCounter atomic.Uint64

func init() {
	// Libraries and tests remain silent until the executable explicitly starts
	// file logging. Production never mirrors operational events to stderr.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func Start(options Options) (*Runtime, error) {
	directory, err := resolveDirectory(options.Directory)
	if err != nil {
		return nil, err
	}
	directoryExisted := true
	if info, statErr := os.Stat(directory); statErr == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("log directory %q is not a directory", directory)
		}
	} else {
		if !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("inspect log directory %q: %w", directory, statErr)
		}
		directoryExisted = false
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory %q: %w", directory, err)
	}
	// Never change an existing executable directory's permissions. For an
	// explicitly-created log directory, enforce owner-only access.
	if !directoryExisted {
		if err := os.Chmod(directory, 0o700); err != nil {
			return nil, fmt.Errorf("secure log directory %q: %w", directory, err)
		}
	}
	path := filepath.Join(directory, LogFileName)
	probe, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	if err := probe.Chmod(0o600); err != nil {
		_ = probe.Close()
		return nil, fmt.Errorf("secure log file %q: %w", path, err)
	}
	if err := probe.Close(); err != nil {
		return nil, fmt.Errorf("close log file probe %q: %w", path, err)
	}

	writer := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    positive(options.MaxSizeMB, 50),
		MaxBackups: positive(options.MaxBackups, 10),
		MaxAge:     nonNegative(options.MaxAgeDays, 30),
		Compress:   options.Compress,
		LocalTime:  true,
	}
	level, err := parseLevel(options.Level)
	if err != nil {
		return nil, err
	}
	runID := NewID("run")
	handler := &redactingHandler{next: slog.NewJSONHandler(writer, &slog.HandlerOptions{
		AddSource: true,
		Level:     level,
	})}
	logger := slog.New(handler).With(
		"service", "ungit-go",
		"version", options.Version,
		"run_id", runID,
		"pid", os.Getpid(),
	)
	slog.SetDefault(logger)
	started := time.Now()
	return &Runtime{
		path: path, statePath: filepath.Join(directory, "ungit-go-run-state.json"),
		writer: writer, runID: runID, started: started,
	}, nil
}

// MarkRunning creates the unclean-run marker after the server has acquired its
// listener. Delaying this avoids treating a second launch against an already
// running instance as a crash and avoids overwriting the live process marker.
func (r *Runtime) MarkRunning() error {
	if r == nil || r.writer == nil || r.stateActive {
		return nil
	}
	if previous, readErr := readRunState(r.statePath); readErr == nil {
		if !previous.Clean {
			Warn(context.Background(), "process.previous_run_unclean", "previous Ungit-Go run did not record a clean shutdown",
				"previous_run_id", previous.RunID,
				"previous_pid", previous.PID,
				"previous_started_at", previous.StartedAt,
			)
		}
	} else if !os.IsNotExist(readErr) {
		Warn(context.Background(), "process.run_state.read_failed", "failed to read previous run state", ErrorFields(readErr)...)
	}
	if err := writeRunState(r.statePath, runState{RunID: r.runID, PID: os.Getpid(), StartedAt: r.started}); err != nil {
		return fmt.Errorf("write run-state file %q: %w", r.statePath, err)
	}
	r.stateActive = true
	return nil
}

func (r *Runtime) Close() error {
	if r == nil || r.writer == nil {
		return nil
	}
	var stateErr error
	if r.stateActive {
		stateErr = writeRunState(r.statePath, runState{
			RunID: r.runID, PID: os.Getpid(), StartedAt: r.started, EndedAt: time.Now(), Clean: true,
		})
	}
	writerErr := r.writer.Close()
	if stateErr != nil {
		return stateErr
	}
	return writerErr
}

func (r *Runtime) Path() string       { return r.path }
func (r *Runtime) RunID() string      { return r.runID }
func (r *Runtime) Started() time.Time { return r.started }

func readRunState(path string) (runState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return runState{}, err
	}
	var state runState
	if err := json.Unmarshal(data, &state); err != nil {
		return runState{}, err
	}
	return state, nil
}

func writeRunState(path string, state runState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func resolveDirectory(override *string) (string, error) {
	if override != nil && strings.TrimSpace(*override) != "" {
		return filepath.Abs(filepath.Clean(*override))
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable for default log directory: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	return filepath.Dir(executable), nil
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "trace":
		return LevelTrace, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", value)
	}
}

func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func nonNegative(value, fallback int) int {
	if value >= 0 {
		return value
	}
	return fallback
}

func NewID(prefix string) string {
	var random [6]byte
	if _, err := rand.Read(random[:]); err == nil {
		return prefix + "-" + hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), idCounter.Add(1))
}

func WithRequestID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, requestIDKey, value)
}

func WithActionID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, actionIDKey, value)
}

func WithSocketID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, socketIDKey, value)
}

func WithWatcherID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, watcherIDKey, value)
}

func RequestID(ctx context.Context) string { return contextValue(ctx, requestIDKey) }
func ActionID(ctx context.Context) string  { return contextValue(ctx, actionIDKey) }
func SocketID(ctx context.Context) string  { return contextValue(ctx, socketIDKey) }
func WatcherID(ctx context.Context) string { return contextValue(ctx, watcherIDKey) }

func contextValue(ctx context.Context, key contextKey) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(key).(string)
	return value
}

func Log(ctx context.Context, level slog.Level, event, message string, args ...any) {
	attrs := make([]any, 0, len(args)+10)
	attrs = append(attrs, "event", event)
	for _, item := range []struct {
		key   string
		value string
	}{
		{"request_id", RequestID(ctx)},
		{"action_id", ActionID(ctx)},
		{"socket_id", SocketID(ctx)},
		{"watcher_id", WatcherID(ctx)},
	} {
		if item.value != "" {
			attrs = append(attrs, item.key, item.value)
		}
	}
	attrs = append(attrs, args...)
	slog.Default().Log(ctx, level, message, attrs...)
}

func Trace(ctx context.Context, event, message string, args ...any) {
	Log(ctx, LevelTrace, event, message, args...)
}

func Debug(ctx context.Context, event, message string, args ...any) {
	Log(ctx, slog.LevelDebug, event, message, args...)
}

func Info(ctx context.Context, event, message string, args ...any) {
	Log(ctx, slog.LevelInfo, event, message, args...)
}

func Warn(ctx context.Context, event, message string, args ...any) {
	Log(ctx, slog.LevelWarn, event, message, args...)
}

func Error(ctx context.Context, event, message string, err error, args ...any) {
	if err != nil {
		args = append(args, ErrorFields(err)...)
	}
	Log(ctx, slog.LevelError, event, message, args...)
}

func ErrorFields(err error) []any {
	if err == nil {
		return nil
	}
	return []any{
		"error_class", ClassifyError(err),
		"error_summary", RedactFreeText(err.Error()),
		"error_type", fmt.Sprintf("%T", err),
	}
}

func ClassifyError(err error) string {
	if err == nil {
		return "none"
	}
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case os.IsNotExist(err):
		return "not_found"
	case os.IsPermission(err):
		return "permission_denied"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	text := strings.ToLower(err.Error())
	for _, item := range []struct {
		contains string
		class    string
	}{
		{"address already in use", "address_in_use"},
		{"connection refused", "connection_refused"},
		{"connection reset", "connection_reset"},
		{"broken pipe", "broken_pipe"},
		{"no space left", "storage_full"},
		{"too many open files", "resource_exhausted"},
		{"file exists", "already_exists"},
		{"permission denied", "permission_denied"},
		{"authentication", "authentication_failed"},
		{"not a git repository", "not_a_repository"},
	} {
		if strings.Contains(text, item.contains) {
			return item.class
		}
	}
	return "other"
}

type standardLogWriter struct{ event string }

func (w standardLogWriter) Write(data []byte) (int, error) {
	message := strings.TrimSpace(string(data))
	if message != "" {
		Error(context.Background(), w.event, "standard library diagnostic", nil,
			"diagnostic_summary", RedactFreeText(message))
	}
	return len(data), nil
}

func StandardLogger(event string) *log.Logger {
	return log.New(standardLogWriter{event: event}, "", 0)
}

func RuntimeFields() []any {
	return []any{
		"go_version", runtime.Version(),
		"go_os", runtime.GOOS,
		"go_arch", runtime.GOARCH,
		"goroutines", runtime.NumGoroutine(),
	}
}

var (
	urlUserInfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)([^/@\s]+)@`)
	scpUserInfoPattern = regexp.MustCompile(`(?i)([^\s/@:]+):([^\s/@]+)@`)
	secretValuePattern = regexp.MustCompile(`(?i)(password|passwd|token|access_token|api_key|authorization|cookie|secret)=([^&\s]+)`)
	bearerPattern      = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+\-/=]+`)
)

func RedactString(value string) string {
	value = urlUserInfoPattern.ReplaceAllString(value, `${1}<redacted>@`)
	value = scpUserInfoPattern.ReplaceAllString(value, `<redacted>@`)
	value = secretValuePattern.ReplaceAllString(value, `${1}=<redacted>`)
	value = bearerPattern.ReplaceAllString(value, `${1}<redacted>`)
	return value
}

func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func RedactFreeText(value string) string {
	// Browser-provided messages/stacks can contain arbitrary secrets that do not
	// have recognizable key names. Preserve a fingerprint and shape, not content.
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fmt.Sprintf("<redacted-free-text length=%d sha256=%s>", len(value), Fingerprint(value))
}

func RedactURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return RedactString(value)
	}
	if parsed.User != nil {
		parsed.User = url.User("<redacted>")
	}
	query := parsed.Query()
	for key := range query {
		if sensitiveKey(key) {
			query.Set(key, "<redacted>")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func RedactArgs(args []string) []string {
	out := make([]string, len(args))
	redactNext := false
	for index, arg := range args {
		lower := strings.ToLower(arg)
		if redactNext {
			out[index] = "<redacted>"
			redactNext = false
			continue
		}
		if strings.Contains(lower, "credential.helper=") {
			out[index] = "credential.helper=<redacted>"
			continue
		}
		if lower == "--password" || lower == "--token" || lower == "--authorization" || lower == "http.extraheader" {
			out[index] = arg
			redactNext = true
			continue
		}
		out[index] = RedactString(arg)
	}
	return out
}

func Preview(value string, limit int) string {
	value = RedactString(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

type redactingHandler struct{ next slog.Handler }

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	copyRecord := slog.NewRecord(record.Time, record.Level, RedactString(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		copyRecord.AddAttrs(redactAttr(attr))
		return true
	})
	return h.next.Handle(ctx, copyRecord)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for index, attr := range attrs {
		redacted[index] = redactAttr(attr)
	}
	return &redactingHandler{next: h.next.WithAttrs(redacted)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if sensitiveKey(attr.Key) {
		return slog.String(attr.Key, "<redacted>")
	}
	switch attr.Value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, RedactString(attr.Value.String()))
	case slog.KindGroup:
		group := attr.Value.Group()
		for index := range group {
			group[index] = redactAttr(group[index])
		}
		return slog.Group(attr.Key, attrsToAny(group)...)
	case slog.KindAny:
		return slog.Any(attr.Key, redactAny(attr.Value.Any(), 0))
	}
	return attr
}

func redactAny(value any, depth int) any {
	if depth > 8 {
		return "<maximum-depth>"
	}
	switch typed := value.(type) {
	case nil, bool, json.Number,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return typed
	case error:
		return RedactFreeText(typed.Error())
	case string:
		return RedactString(typed)
	case []byte:
		return RedactFreeText(string(typed))
	case fmt.Stringer:
		return RedactFreeText(typed.String())
	case map[string]any:
		return redactStringMap(typed, depth)
	case map[string]string:
		output := make(map[string]any, len(typed))
		for key, item := range typed {
			output[key] = item
		}
		return redactStringMap(output, depth)
	case []any:
		output := make([]any, len(typed))
		for index, item := range typed {
			output[index] = redactAny(item, depth+1)
		}
		return output
	case []string:
		output := make([]string, len(typed))
		for index, item := range typed {
			output[index] = RedactString(item)
		}
		return output
	default:
		// Normalize arbitrary maps, slices, and structs through JSON so nested
		// secret-named fields cannot bypass the handler through slog.Any.
		data, err := json.Marshal(value)
		if err != nil {
			return RedactFreeText(fmt.Sprint(value))
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		var normalized any
		if err := decoder.Decode(&normalized); err != nil {
			return RedactFreeText(fmt.Sprint(value))
		}
		return redactAny(normalized, depth+1)
	}
}

func redactStringMap(input map[string]any, depth int) map[string]any {
	output := make(map[string]any, len(input))
	for key, item := range input {
		if sensitiveKey(key) {
			output[key] = "<redacted>"
		} else {
			output[key] = redactAny(item, depth+1)
		}
	}
	return output
}

func attrsToAny(attrs []slog.Attr) []any {
	out := make([]any, len(attrs))
	for index := range attrs {
		out[index] = attrs[index]
	}
	return out
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	compact := strings.ReplaceAll(key, "_", "")
	for _, word := range []string{"password", "passwd", "token", "accesstoken", "authorization", "cookie", "secret", "credential", "apikey", "privatekey"} {
		if compact == word || strings.HasSuffix(compact, word) {
			return true
		}
	}
	return false
}
