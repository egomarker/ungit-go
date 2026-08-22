package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egomarker/ungit-go/internal/config"
)

const DefaultTimeout = 2 * time.Minute

var baseConfigArgs = []string{
	"-c", "color.ui=false",
	"-c", "core.quotepath=false",
	"-c", "core.pager=cat",
	"-c", "core.editor=:",
}

type Command struct {
	RepoPath   string
	Args       []string
	AllowError bool
	Stdin      []byte
	Timeout    time.Duration
	Stdout     io.Writer
	Env        []string
}

type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type Error struct {
	IsGitError       bool   `json:"isGitError"`
	ErrorCode        string `json:"errorCode"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"workingDirectory"`
	ErrorText        string `json:"error"`
	Message          string `json:"message"`
	Stderr           string `json:"stderr"`
	Stdout           string `json:"stdout"`
	StdoutLower      string `json:"stdoutLower"`
	StderrLower      string `json:"stderrLower"`
}

func (e *Error) Error() string { return e.Message }

func (e *Error) MarshalJSON() ([]byte, error) {
	type alias Error
	return json.Marshal((*alias)(e))
}

type Runner struct {
	cfg              config.Config
	sem              chan struct{}
	optionalLocks    bool
	mu               sync.Mutex
	randSleepForTest func() time.Duration
}

func NewRunner(cfg config.Config) *Runner {
	limit := cfg.MaxConcurrentGitOperations
	if limit < 1 {
		limit = 1
	}
	return &Runner{
		cfg:           cfg,
		sem:           make(chan struct{}, limit),
		optionalLocks: supportsOptionalLocks(cfg.GitBinPath),
		randSleepForTest: func() time.Duration {
			return time.Duration(rand.IntN(501)+250) * time.Millisecond
		},
	}
}

func supportsOptionalLocks(gitBinPath *string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	info := GetVersionInfo(ctx, gitBinPath)
	if !info.Satisfied || info.Version == "" || info.Version == "unkown" {
		return false
	}
	var major, minor int
	_, _ = fmt.Sscanf(info.Version, "%d.%d", &major, &minor)
	return major == 2 && minor == 15 && info.Version == "2.15.0"
}

func (r *Runner) Run(ctx context.Context, c Command) (Result, error) {
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	filtered := make([]string, 0, len(c.Args))
	for _, arg := range c.Args {
		if arg != "" {
			filtered = append(filtered, arg)
		}
	}
	args := append(append([]string{}, baseConfigArgs...), filtered...)
	return r.runRetry(ctx, c, args, r.cfg.LockConflictRetryCount)
}

func (r *Runner) RunText(ctx context.Context, repoPath string, args ...string) (string, error) {
	res, err := r.Run(ctx, Command{RepoPath: repoPath, Args: args})
	return string(res.Stdout), err
}

func (r *Runner) runRetry(ctx context.Context, c Command, args []string, retries int) (Result, error) {
	res, err := r.runOnce(ctx, c, args)
	if err == nil || retries <= 0 || !isRetryable(err) {
		return res, err
	}
	if r.cfg.LogGitCommands {
		log.Printf("retrying git commands after lock conflict (remaining=%d)", retries)
	}
	timer := time.NewTimer(r.randSleepForTest())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return res, ctx.Err()
	case <-timer.C:
		return r.runRetry(ctx, c, args, retries-1)
	}
}

func (r *Runner) runOnce(ctx context.Context, c Command, args []string) (Result, error) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	cmdCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	if r.cfg.LogGitCommands {
		log.Printf("git executing: %s %s", c.RepoPath, strings.Join(args, " "))
	}

	cmd := exec.CommandContext(cmdCtx, Executable(r.cfg.GitBinPath), args...)
	cmd.Dir = c.RepoPath
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.Env = append(cmd.Env, c.Env...)
	if len(c.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var stdout, stderr bytes.Buffer
	if c.Stdout != nil {
		cmd.Stdout = io.MultiWriter(c.Stdout, &stdout)
	} else {
		cmd.Stdout = &stdout
	}
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: exitCode(err)}

	if r.cfg.LogGitCommands || r.cfg.LogGitOutput {
		log.Printf("git result (first 400 bytes): %s\nstderr=%s\nstdout=%s", strings.Join(args, " "), trim400(stderr.String()), trim400(stdout.String()))
	}

	if err == nil || (result.ExitCode == 1 && c.AllowError) {
		return result, nil
	}
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return result, &Error{
			IsGitError: true, ErrorCode: "unknown", Command: strings.Join(args, " "), WorkingDirectory: c.RepoPath,
			ErrorText: stderr.String(), Message: firstLine(stderr.String()), Stderr: stderr.String(), Stdout: stdout.String(),
			StdoutLower: strings.ToLower(stdout.String()), StderrLower: strings.ToLower(stderr.String()),
		}
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return result, err
	}
	return result, NewError(c.RepoPath, args, stderr.String(), stdout.String())
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return -1
	}
	if status, ok := err.(interface{ Sys() any }); ok {
		if ws, ok := status.Sys().(syscall.WaitStatus); ok {
			return ws.ExitStatus()
		}
	}
	return -1
}

func trim400(s string) string {
	if len(s) <= 400 {
		return s
	}
	return s[:400]
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func NewError(repoPath string, args []string, stderr, stdout string) *Error {
	e := &Error{
		IsGitError: true, ErrorCode: "unknown", Command: strings.Join(args, " "), WorkingDirectory: repoPath,
		ErrorText: stderr, Message: firstLine(stderr), Stderr: stderr, Stdout: stdout,
		StdoutLower: strings.ToLower(stdout), StderrLower: strings.ToLower(stderr),
	}
	s := e.StderrLower
	o := e.StdoutLower
	switch {
	case strings.Contains(s, "not a git repository"):
		e.ErrorCode = "not-a-repository"
	case strings.Contains(s, "bad default revision 'head'"):
		e.ErrorCode = "no-head"
	case strings.Contains(s, "does not have any commits yet"):
		e.ErrorCode = "no-commits"
	case strings.Contains(s, "connection timed out"):
		e.ErrorCode = "remote-timeout"
	case strings.Contains(s, "permission denied (publickey)"):
		e.ErrorCode = "permision-denied-publickey"
	case strings.Contains(s, "ssh: connect to host") && strings.Contains(s, "bad file number"):
		e.ErrorCode = "ssh-bad-file-number"
	case strings.Contains(s, "no remote configured to list refs from."):
		e.ErrorCode = "no-remote-configured"
	case (strings.Contains(s, "unable to access") && strings.Contains(s, "could not resolve host:")) || strings.Contains(s, "could not resolve hostname"):
		e.ErrorCode = "offline"
	case strings.Contains(s, "proxy authentication required"):
		e.ErrorCode = "proxy-authentication-required"
	case strings.Contains(s, "please tell me who you are"):
		e.ErrorCode = "no-git-name-email-configured"
	case strings.HasPrefix(s, "fatal error: disconnected: no supported authentication methods available (server sent: publickey)"):
		e.ErrorCode = "no-supported-authentication-provided"
	case strings.HasPrefix(s, "fatal: no remote repository specified."):
		e.ErrorCode = "no-remote-specified"
	case strings.Contains(s, "non-fast-forward"):
		e.ErrorCode = "non-fast-forward"
	case strings.HasPrefix(s, "failed to merge in the changes.") || strings.Contains(o, "conflict (content): merge conflict in") || strings.Contains(s, "after resolving the conflicts"):
		e.ErrorCode = "merge-failed"
	case strings.Contains(s, "this operation must be run in a work tree"):
		e.ErrorCode = "must-be-in-working-tree"
	case strings.Contains(s, "your local changes to the following files would be overwritten by checkout"):
		e.ErrorCode = "local-changes-would-be-overwritten"
	}
	return e
}

func isRetryable(err error) bool {
	var ge *Error
	if !errors.As(err, &ge) {
		return false
	}
	return strings.Contains(ge.ErrorText, "index.lock': File exists") || strings.Contains(ge.ErrorText, "index file open failed: Permission denied")
}
