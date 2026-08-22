package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CommitFile struct {
	Name          string `json:"name"`
	PatchLineList []bool `json:"patchLineList"`
}

func (s *Service) RunMutation(ctx context.Context, repoPath string, args ...string) (string, error) {
	res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: args})
	return string(res.Stdout), err
}

// AutoStashExecuteAndPop matches Ungit-compatible autoStashExecuteAndPop wrapper. It
// intentionally uses plain `git stash` just like the Node implementation.
func (s *Service) AutoStashExecuteAndPop(ctx context.Context, repoPath string, args []string, timeout time.Duration) (string, error) {
	if !s.cfg.AutoStashAndPop {
		res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: args, Timeout: timeout})
		return string(res.Stdout), err
	}

	hadLocalChanges := true
	stash, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"stash"}})
	if err != nil {
		if ge, ok := err.(*Error); ok && strings.Contains(ge.Stderr, "You do not have the initial commit yet") {
			hadLocalChanges = false
		} else {
			return "", err
		}
	} else if len(stash.Stdout) == 0 || strings.Contains(string(stash.Stdout), "No local changes to save") {
		hadLocalChanges = false
	}

	res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: args, Timeout: timeout})
	if err != nil {
		return string(res.Stdout), err
	}
	if !hadLocalChanges {
		// Node resolves null here, which is serialized as {} by jsonResultOrFailProm.
		return "", nil
	}
	pop, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"stash", "pop"}})
	return string(pop.Stdout), err
}

func (s *Service) DiscardAllChanges(ctx context.Context, repoPath string) (string, error) {
	if _, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"reset", "--hard", "HEAD"}}); err != nil {
		return "", err
	}
	res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"clean", "-fd"}})
	return string(res.Stdout), err
}

func (s *Service) DiscardChangesInFile(ctx context.Context, repoPath, filename string) (string, error) {
	status, err := s.Status(ctx, repoPath, filename)
	if err != nil {
		return "", err
	}
	if len(status.Files) == 0 {
		return "", fmt.Errorf("No files in status in discard, filename: %s", filename)
	}
	var fileStatus FileStatus
	for _, st := range status.Files {
		fileStatus = st
		break
	}
	filename = strings.TrimSpace(filename)
	fullPath := filepath.Join(repoPath, filename)

	if fileStatus.Staged {
		res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"rm", "-f", filename}})
		return string(res.Stdout), err
	}
	if fileStatus.IsNew {
		if err := os.Remove(fullPath); err != nil {
			return "", fmt.Errorf("unlink: %w", err)
		}
		return "", nil
	}

	if st, statErr := os.Stat(fullPath); statErr == nil && st.IsDir() {
		if _, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"submodule", "sync"}}); err != nil {
			return "", err
		}
		res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"submodule", "update", "--init", "-f", "--recursive", filename}})
		return string(res.Stdout), err
	}
	res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"checkout", "HEAD", "--", filename}})
	return string(res.Stdout), err
}

func (s *Service) ResolveConflicts(ctx context.Context, repoPath string, files []string) (string, error) {
	toAdd := []string{}
	toRemove := []string{}
	for _, file := range files {
		if _, err := os.Stat(filepath.Join(repoPath, file)); err == nil {
			toAdd = append(toAdd, file)
		} else {
			toRemove = append(toRemove, file)
		}
	}
	var last string
	if len(toAdd) > 0 {
		res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: append([]string{"add"}, toAdd...)})
		if err != nil {
			return string(res.Stdout), err
		}
		last = string(res.Stdout)
	}
	if len(toRemove) > 0 {
		res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: append([]string{"rm"}, toRemove...)})
		if err != nil {
			return string(res.Stdout), err
		}
		last = string(res.Stdout)
	}
	return last, nil
}

func (s *Service) Commit(ctx context.Context, repoPath string, amend, emptyCommit bool, message *string, files []CommitFile) (string, error) {
	if message == nil {
		return "", fmt.Errorf("Must specify commit message")
	}
	if len(files) == 0 && !amend && !emptyCommit {
		return "", fmt.Errorf("Must specify files or amend to commit")
	}
	status, err := s.Status(ctx, repoPath, "")
	if err != nil {
		return "", err
	}

	toAdd := []string{}
	toRemove := []string{}
	for _, file := range files {
		name := file.Name
		fileStatus, ok := status.Files[name]
		if !ok {
			if rel, relErr := filepath.Rel(repoPath, name); relErr == nil {
				fileStatus, ok = status.Files[rel]
			}
		}
		if !ok {
			return "", fmt.Errorf("No such file in staging: %s", file.Name)
		}

		trimmed := strings.TrimSpace(name)
		if fileStatus.Removed {
			toRemove = append(toRemove, trimmed)
			continue
		}
		if file.PatchLineList != nil {
			diff, diffErr := s.Runner.RunText(ctx, repoPath, "diff", "--", trimmed)
			if diffErr != nil {
				return "", diffErr
			}
			patch := ParsePatchDiffResult(file.PatchLineList, diff)
			if patch != "" {
				if _, patchErr := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"apply", "--cached"}, Stdin: []byte(patch + "\n\n")}); patchErr != nil {
					return "", patchErr
				}
			}
			continue
		}
		toAdd = append(toAdd, trimmed)
	}

	if len(toRemove) > 0 {
		if _, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"update-index", "--remove", "--stdin"}, Stdin: []byte(strings.Join(toRemove, "\n"))}); err != nil {
			return "", err
		}
	}
	if len(toAdd) > 0 {
		if _, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: []string{"update-index", "--add", "--stdin"}, Stdin: []byte(strings.Join(toAdd, "\n"))}); err != nil {
			return "", err
		}
	}

	args := []string{"commit"}
	if amend {
		args = append(args, "--amend")
	}
	if emptyCommit || amend {
		args = append(args, "--allow-empty")
	}
	if s.cfg.IsForceGPGSign {
		args = append(args, "-S")
	}
	args = append(args, "--file=-")
	res, err := s.Runner.Run(ctx, Command{RepoPath: repoPath, Args: args, Stdin: []byte(*message)})
	if err != nil {
		if ge, ok := err.(*Error); ok && strings.Contains(ge.Stdout, "Changes not staged for commit") {
			return "", nil
		}
		return string(res.Stdout), err
	}
	return string(res.Stdout), nil
}
