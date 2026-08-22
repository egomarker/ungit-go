package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
)

const RequiredVersion = ">=1.8.x"

var versionRE = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

type VersionInfo struct {
	RequiredVersion string `json:"requiredVersion"`
	Version         string `json:"version"`
	Satisfied       bool   `json:"satisfied"`
	Error           string `json:"error,omitempty"`
}

func Executable(gitBinPath *string) string {
	if gitBinPath == nil || *gitBinPath == "" {
		return "git"
	}
	return filepath.Join(*gitBinPath, "git")
}

func GetVersionInfo(ctx context.Context, gitBinPath *string) VersionInfo {
	result := VersionInfo{RequiredVersion: RequiredVersion, Version: "unkown", Satisfied: false}
	out, err := exec.CommandContext(ctx, Executable(gitBinPath), "--version").CombinedOutput()
	if err != nil {
		result.Error = fmt.Sprintf("Failed to parse git version number. Note that Ungit-Go requires git version %s", RequiredVersion)
		return result
	}
	match := versionRE.FindSubmatch(out)
	if match == nil {
		result.Error = fmt.Sprintf("Failed to parse git version number. Note that Ungit-Go requires git version %s", RequiredVersion)
		return result
	}
	result.Version = string(match[0])
	major, _ := strconv.Atoi(string(match[1]))
	minor, _ := strconv.Atoi(string(match[2]))
	result.Satisfied = major > 1 || (major == 1 && minor >= 8)
	if !result.Satisfied {
		result.Error = fmt.Sprintf("Ungit-Go requires git version %s, you are currently running %s", RequiredVersion, result.Version)
	}
	return result
}
