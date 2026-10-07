package server

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gitapi "github.com/egomarker/ungit-go/internal/git"
	"github.com/egomarker/ungit-go/internal/observability"
)

const watchPollInterval = 350 * time.Millisecond

type eventDebouncer struct {
	mu      sync.Mutex
	timer   *time.Timer
	first   time.Time
	emit    func()
	stopped bool
}

func newEventDebouncer(emit func()) *eventDebouncer { return &eventDebouncer{emit: emit} }

func (d *eventDebouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	now := time.Now()
	if d.first.IsZero() {
		d.first = now
	}
	if !d.first.IsZero() && now.Sub(d.first) >= time.Second {
		if d.timer != nil {
			d.timer.Stop()
			d.timer = nil
		}
		d.first = time.Time{}
		go d.emit()
		return
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(500*time.Millisecond, func() {
		d.mu.Lock()
		if d.stopped {
			d.mu.Unlock()
			return
		}
		d.timer = nil
		d.first = time.Time{}
		d.mu.Unlock()
		d.emit()
	})
}

func (d *eventDebouncer) Stop() {
	d.mu.Lock()
	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
	}
	d.mu.Unlock()
}

func (s *Server) watchRepositoryPolling(ctx context.Context, c *realtimeClient, repoPath string, ready chan<- struct{}) {
	started := time.Now()
	observability.Info(ctx, "watch.polling.started", "polling repository watcher started", "repository", repoPath)
	defer func() {
		observability.Info(ctx, "watch.polling.stopped", "polling repository watcher stopped",
			"repository", repoPath, "duration_ms", time.Since(started).Milliseconds(), "reason", ctx.Err())
	}()
	workDebounce := newEventDebouncer(func() {
		observability.Info(ctx, "watch.working_tree.changed", "working tree change detected", "repository", repoPath)
		s.realtime.emit(c, "working-tree-changed", map[string]string{"repository": repoPath})
	})
	gitDebounce := newEventDebouncer(func() {
		observability.Info(ctx, "watch.git_directory.changed", "Git directory change detected", "repository", repoPath)
		s.realtime.emit(c, "git-directory-changed", map[string]string{"repository": repoPath})
	})
	defer workDebounce.Stop()
	defer gitDebounce.Stop()

	workFP, workErr := s.worktreeFingerprint(ctx, repoPath)
	if workErr != nil {
		observability.Error(ctx, "watch.fingerprint.failed", "initial working tree fingerprint failed", workErr, "repository", repoPath)
	}
	gitFP, gitErr := s.gitStateFingerprint(ctx, repoPath)
	if gitErr != nil {
		observability.Error(ctx, "watch.fingerprint.failed", "initial Git state fingerprint failed", gitErr, "repository", repoPath)
	}
	workFailures, gitFailures := 0, 0
	close(ready)
	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if next, err := s.worktreeFingerprint(ctx, repoPath); err == nil {
				if workFailures > 0 {
					observability.Info(ctx, "watch.fingerprint.recovered", "working tree fingerprint recovered",
						"repository", repoPath, "consecutive_failures", workFailures)
					workFailures = 0
				}
				if next != workFP {
					workFP = next
					workDebounce.Trigger()
				}
			} else {
				workFailures++
				if workFailures == 1 || workFailures%10 == 0 {
					observability.Error(ctx, "watch.fingerprint.failed", "working tree fingerprint failed", err,
						"repository", repoPath, "consecutive_failures", workFailures)
				}
			}
			if next, err := s.gitStateFingerprint(ctx, repoPath); err == nil {
				if gitFailures > 0 {
					observability.Info(ctx, "watch.fingerprint.recovered", "Git state fingerprint recovered",
						"repository", repoPath, "consecutive_failures", gitFailures)
					gitFailures = 0
				}
				if next != gitFP {
					gitFP = next
					gitDebounce.Trigger()
				}
			} else {
				gitFailures++
				if gitFailures == 1 || gitFailures%10 == 0 {
					observability.Error(ctx, "watch.fingerprint.failed", "Git state fingerprint failed", err,
						"repository", repoPath, "consecutive_failures", gitFailures)
				}
			}
		}
	}
}

func (s *Server) worktreeFingerprint(ctx context.Context, repoPath string) ([32]byte, error) {
	var zero [32]byte
	res, err := s.git.Runner.Run(ctx, gitapi.Command{
		RepoPath:   repoPath,
		Args:       []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"},
		AllowError: true,
		Timeout:    10 * time.Second,
		Env:        []string{"GIT_OPTIONAL_LOCKS=0"},
	})
	if err != nil {
		return zero, err
	}
	h := sha256.New()
	_, _ = h.Write(res.Stdout)
	for _, p := range statusPaths(res.Stdout) {
		if p == "" {
			continue
		}
		full := filepath.Join(repoPath, filepath.FromSlash(p))
		if info, statErr := os.Stat(full); statErr == nil {
			writeStatFingerprint(h, p, info)
		}
	}
	copy(zero[:], h.Sum(nil))
	return zero, nil
}

func statusPaths(out []byte) []string {
	parts := strings.Split(string(out), "\x00")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if len(part) >= 4 && part[2] == ' ' {
			paths = append(paths, part[3:])
			continue
		}
		// Rename/copy records contain a second NUL-delimited path without a status prefix.
		paths = append(paths, part)
	}
	return paths
}

func writeStatFingerprint(h hash.Hash, path string, info os.FileInfo) {
	_, _ = h.Write([]byte(path))
	var buf [24]byte
	binary.LittleEndian.PutUint64(buf[0:8], uint64(info.Size()))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(info.ModTime().UnixNano()))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(info.Mode()))
	_, _ = h.Write(buf[:])
}

func (s *Server) gitStateFingerprint(ctx context.Context, repoPath string) ([32]byte, error) {
	var sum [32]byte
	gitDirText, err := s.git.Runner.RunText(ctx, repoPath, "rev-parse", "--git-dir")
	if err != nil {
		return sum, err
	}
	gitDir := strings.TrimSpace(gitDirText)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}
	h := sha256.New()
	for _, name := range []string{"HEAD", "index"} {
		p := filepath.Join(gitDir, name)
		if info, statErr := os.Stat(p); statErr == nil {
			writeStatFingerprint(h, name, info)
			if name == "HEAD" {
				b, readErr := os.ReadFile(p)
				if readErr != nil {
					return sum, readErr
				}
				_, _ = h.Write(b)
			}
		} else if !os.IsNotExist(statErr) {
			return sum, statErr
		}
	}
	refs := filepath.Join(gitDir, "refs")
	refsErr := filepath.WalkDir(refs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".lock") {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		rel, relErr := filepath.Rel(gitDir, path)
		if relErr != nil {
			return relErr
		}
		writeStatFingerprint(h, rel, info)
		return nil
	})
	if refsErr != nil && !os.IsNotExist(refsErr) {
		return sum, refsErr
	}
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
