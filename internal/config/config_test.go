package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseNoLaunchBrowser(t *testing.T) {
	cfg, err := Parse([]string{"--port=9999", "--no-launchBrowser"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9999 {
		t.Fatalf("port = %d, want 9999", cfg.Port)
	}
	if cfg.LaunchBrowser {
		t.Fatal("launchBrowser = true, want false")
	}
}

func TestParseGitBinPath(t *testing.T) {
	cfg, err := Parse([]string{"--gitBinPath=/usr/bin"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitBinPath == nil || *cfg.GitBinPath != "/usr/bin" {
		t.Fatalf("gitBinPath = %v, want /usr/bin", cfg.GitBinPath)
	}
}

func TestParseNodeCompatibleBooleanNegationsAndAliases(t *testing.T) {
	cfg, err := Parse([]string{"--no-autoFetch", "--no-logRESTRequests", "--no-b", "-o"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoFetch || cfg.LogRESTRequests || cfg.LaunchBrowser {
		t.Fatalf("negations not applied: %+v", cfg)
	}
	if !cfg.GitVersionCheckOverride {
		t.Fatal("-o alias did not enable gitVersionCheckOverride")
	}
}

func TestParseRCPrecedenceAndCLIConfigOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	rc := `{"port":1111,"autoFetch":false,"launchCommand":"echo %U","autoShutdownTimeout":99,"theme":"dark"}`
	if err := os.WriteFile(filepath.Join(home, ".ungitrc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse([]string{"--port=2222"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 2222 || cfg.AutoFetch || cfg.LaunchCommand == nil || *cfg.LaunchCommand != "echo %U" || cfg.AutoShutdownTimeout == nil || *cfg.AutoShutdownTimeout != 99 || cfg.Theme != "dark" {
		t.Fatalf("rc/cli precedence mismatch: %+v", cfg)
	}
	cfg, err = Parse([]string{"--cliconfigonly"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != DefaultPort || !cfg.AutoFetch || cfg.LaunchCommand != nil || cfg.AutoShutdownTimeout != nil || cfg.Theme != "system" {
		t.Fatalf("cliconfigonly did not ignore rc: %+v", cfg)
	}
}

func TestLoggingDefaultsAndValidation(t *testing.T) {
	cfg, err := Parse([]string{"--cliconfigonly"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "info" || !cfg.LogRESTRequests || !cfg.LogGitCommands || cfg.LogGitOutput {
		t.Fatalf("logging defaults mismatch: %+v", cfg)
	}
	if cfg.LogMaxSizeMB != 50 || cfg.LogMaxBackups != 10 || cfg.LogMaxAgeDays != 30 || !cfg.LogCompress {
		t.Fatalf("rotation defaults mismatch: %+v", cfg)
	}
	cfg, err = Parse([]string{"--cliconfigonly", "--logLevel=trace", "--logGitOutput", "--logMaxSizeMB=5", "--logMaxBackups=2", "--logMaxAgeDays=7", "--no-logCompress"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "trace" || !cfg.LogGitOutput || cfg.LogMaxSizeMB != 5 || cfg.LogMaxBackups != 2 || cfg.LogMaxAgeDays != 7 || cfg.LogCompress {
		t.Fatalf("logging overrides mismatch: %+v", cfg)
	}
	for _, args := range [][]string{{"--logLevel=nope"}, {"--logMaxSizeMB=0"}, {"--logMaxBackups=0"}, {"--logMaxAgeDays=-1"}} {
		if _, err := Parse(append([]string{"--cliconfigonly"}, args...)); err == nil {
			t.Fatalf("expected invalid logging config for %v", args)
		}
	}
}

func TestParseAdditionalNodeOptions(t *testing.T) {
	cfg, err := Parse([]string{
		`--allowedIPs=["127.0.0.1","::1"]`,
		`--defaultRepositories=["/a","/b"]`,
		`--mergeTool="meld"`,
		`--diffType="sidebysidediff"`,
		`--tabSize=4`,
		`--autoShutdownTimeout=5000`,
		`--launchCommand=browser --app=%U`,
		`--alwaysLoadActiveBranch`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedIPs) != 2 || len(cfg.DefaultRepositories) != 2 {
		t.Fatalf("array options not parsed: %+v", cfg)
	}
	if cfg.MergeTool != "meld" || cfg.DiffType != "sidebysidediff" || cfg.TabSize != float64(4) {
		t.Fatalf("scalar options mismatch: merge=%#v diff=%#v tab=%#v", cfg.MergeTool, cfg.DiffType, cfg.TabSize)
	}
	if cfg.AutoShutdownTimeout == nil || *cfg.AutoShutdownTimeout != 5000 || cfg.LaunchCommand == nil {
		t.Fatalf("runtime options mismatch: %+v", cfg)
	}
	if cfg.MaxActiveBranchSearchIteration != 25 {
		t.Fatalf("deprecated alwaysLoadActiveBranch parity mismatch: %d", cfg.MaxActiveBranchSearchIteration)
	}
}

func TestRCForcedLaunchPathNullMeansHomeScreen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, ".ungitrc"), []byte(`{"forcedLaunchPath":null}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ForcedLaunchPath == nil || *cfg.ForcedLaunchPath != "" {
		t.Fatalf("forcedLaunchPath=%v, want explicit empty path", cfg.ForcedLaunchPath)
	}
}
