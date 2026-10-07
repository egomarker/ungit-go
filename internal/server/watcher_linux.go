//go:build linux

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/egomarker/ungit-go/internal/observability"
)

type inotifyKind uint8

const (
	watchWork inotifyKind = iota
	watchGit
)

type inotifyDir struct {
	path string
	kind inotifyKind
}

func (s *Server) watchRepository(ctx context.Context, c *realtimeClient, repoPath string, ready chan<- struct{}) {
	started, err := s.watchRepositoryInotify(ctx, c, repoPath, ready)
	if err != nil && ctx.Err() == nil {
		if started {
			observability.Error(ctx, "watch.inotify.failed", "inotify repository watcher failed after startup", err, "repository", repoPath)
			return
		}
		// inotify can be unavailable or exceed the user's watch limit. Preserve
		// correctness with the portable M4 poller in that case.
		observability.Warn(ctx, "watch.inotify.fallback", "inotify unavailable; falling back to polling",
			append([]any{"repository", repoPath}, observability.ErrorFields(err)...)...)
		s.watchRepositoryPolling(ctx, c, repoPath, ready)
	}
}

func (s *Server) watchRepositoryInotify(ctx context.Context, c *realtimeClient, repoPath string, ready chan<- struct{}) (bool, error) {
	startedAt := time.Now()
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC)
	if err != nil {
		return false, err
	}
	closed := make(chan struct{})
	var closeOnce sync.Once
	closeFD := func() { closeOnce.Do(func() { _ = syscall.Close(fd) }) }
	defer func() {
		closeFD()
		close(closed)
		observability.Info(ctx, "watch.inotify.stopped", "inotify repository watcher stopped",
			"repository", repoPath, "duration_ms", time.Since(startedAt).Milliseconds(), "reason", ctx.Err())
	}()
	go func() {
		select {
		case <-ctx.Done():
			closeFD()
		case <-closed:
		}
	}()

	gitDir, err := s.resolveGitDir(ctx, repoPath)
	if err != nil {
		return false, err
	}
	workRoot := ""
	if info, statErr := os.Stat(filepath.Join(repoPath, ".git")); statErr == nil && info != nil {
		workRoot = filepath.Clean(repoPath)
	}

	dirs := map[int]inotifyDir{}
	addTree := func(root string, kind inotifyKind, skipGit bool) error { return nil }
	addOne := func(path string, kind inotifyKind) error {
		wd, addErr := syscall.InotifyAddWatch(fd, path, inotifyMask())
		if addErr != nil {
			return addErr
		}
		dirs[wd] = inotifyDir{path: path, kind: kind}
		return nil
	}
	addTree = func(root string, kind inotifyKind, skipGit bool) error {
		return filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if skipGit && path != root && d.Name() == ".git" {
				return filepath.SkipDir
			}
			if addErr := addOne(path, kind); addErr != nil {
				// A single unreadable directory should not disable watching the repo.
				if errors.Is(addErr, syscall.EACCES) || errors.Is(addErr, syscall.ENOENT) {
					return filepath.SkipDir
				}
				return addErr
			}
			return nil
		})
	}

	if workRoot != "" {
		if err := addTree(workRoot, watchWork, true); err != nil {
			return false, err
		}
	}
	if err := addOne(gitDir, watchGit); err != nil {
		return false, err
	}
	refsDir := filepath.Join(gitDir, "refs")
	if info, statErr := os.Stat(refsDir); statErr == nil && info.IsDir() {
		if err := addTree(refsDir, watchGit, false); err != nil {
			return false, err
		}
	}

	workFP, workErr := s.worktreeFingerprint(ctx, repoPath)
	if workErr != nil {
		observability.Error(ctx, "watch.fingerprint.failed", "initial working tree fingerprint failed", workErr, "repository", repoPath)
	}
	gitFP, gitErr := s.gitStateFingerprint(ctx, repoPath)
	if gitErr != nil {
		observability.Error(ctx, "watch.fingerprint.failed", "initial Git state fingerprint failed", gitErr, "repository", repoPath)
	}
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
	observability.Info(ctx, "watch.inotify.started", "inotify repository watcher started",
		"repository", repoPath, "watched_directories", len(dirs))
	close(ready)

	buf := make([]byte, 64*1024)
	for {
		n, readErr := syscall.Read(fd, buf)
		if readErr != nil {
			if ctx.Err() != nil || errors.Is(readErr, syscall.EBADF) || errors.Is(readErr, syscall.EINTR) {
				return true, nil
			}
			return true, readErr
		}
		workDirty, gitDirty := false, false
		for off := 0; off+syscall.SizeofInotifyEvent <= n; {
			ev := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[off]))
			base, ok := dirs[int(ev.Wd)]
			name := ""
			if ev.Len > 0 {
				raw := buf[off+syscall.SizeofInotifyEvent : off+syscall.SizeofInotifyEvent+int(ev.Len)]
				name = strings.TrimRight(string(raw), "\x00")
			}
			full := base.path
			if name != "" {
				full = filepath.Join(base.path, name)
			}
			if ok {
				if base.kind == watchGit {
					if !strings.HasSuffix(name, ".lock") {
						gitDirty = true
					}
				} else {
					workDirty = true
				}
				if ev.Mask&syscall.IN_ISDIR != 0 && ev.Mask&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0 {
					if base.kind != watchWork || filepath.Base(full) != ".git" {
						if addErr := addTree(full, base.kind, base.kind == watchWork); addErr != nil {
							observability.Error(ctx, "watch.inotify.add_tree_failed", "failed to add new directory to inotify watcher", addErr,
								"repository", repoPath, "directory_summary", observability.RedactFreeText(full))
						}
					}
				}
			}
			off += syscall.SizeofInotifyEvent + int(ev.Len)
		}

		if workDirty {
			if next, fpErr := s.worktreeFingerprint(ctx, repoPath); fpErr == nil {
				if next != workFP {
					workFP = next
					workDebounce.Trigger()
				}
			} else {
				observability.Error(ctx, "watch.fingerprint.failed", "working tree fingerprint failed after inotify event", fpErr,
					"repository", repoPath)
			}
		}
		if gitDirty {
			if next, fpErr := s.gitStateFingerprint(ctx, repoPath); fpErr == nil {
				if next != gitFP {
					gitFP = next
					gitDebounce.Trigger()
				}
			} else {
				observability.Error(ctx, "watch.fingerprint.failed", "Git state fingerprint failed after inotify event", fpErr,
					"repository", repoPath)
			}
		}
	}
}

func (s *Server) resolveGitDir(ctx context.Context, repoPath string) (string, error) {
	text, err := s.git.Runner.RunText(ctx, repoPath, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(text)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(repoPath, gitDir)
	}
	return filepath.Clean(gitDir), nil
}

func inotifyMask() uint32 {
	return syscall.IN_ATTRIB |
		syscall.IN_CLOSE_WRITE |
		syscall.IN_CREATE |
		syscall.IN_DELETE |
		syscall.IN_DELETE_SELF |
		syscall.IN_MODIFY |
		syscall.IN_MOVE_SELF |
		syscall.IN_MOVED_FROM |
		syscall.IN_MOVED_TO
}
