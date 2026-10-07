package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DefaultPort    = 8448
	DefaultBindIP  = "127.0.0.1"
	DefaultURLBase = "http://localhost"
)

// Config mirrors the browser-visible/runtime configuration of the Node
// implementation, excluding the intentionally unsupported third-party plugin
// settings. Pointer fields represent Node's undefined/null-capable options.
type Config struct {
	Port                           int               `json:"port"`
	URLBase                        string            `json:"urlBase"`
	RootPath                       string            `json:"rootPath"`
	LogDirectory                   *string           `json:"logDirectory"`
	LogRESTRequests                bool              `json:"logRESTRequests"`
	LogGitCommands                 bool              `json:"logGitCommands"`
	LogGitOutput                   bool              `json:"logGitOutput"`
	Bugtracking                    bool              `json:"bugtracking"`
	Authentication                 bool              `json:"authentication"`
	Users                          map[string]string `json:"users"`
	ShowRebaseAndMergeOnlyOnRefs   bool              `json:"showRebaseAndMergeOnlyOnRefs"`
	MaxConcurrentGitOperations     int               `json:"maxConcurrentGitOperations"`
	LaunchBrowser                  bool              `json:"launchBrowser"`
	AutoShutdownTimeout            *int              `json:"autoShutdownTimeout,omitempty"`
	NoFFMerge                      bool              `json:"noFFMerge"`
	AutoFetch                      bool              `json:"autoFetch"`
	Dev                            bool              `json:"dev"`
	LogLevel                       string            `json:"logLevel"`
	LogMaxSizeMB                   int               `json:"logMaxSizeMB"`
	LogMaxBackups                  int               `json:"logMaxBackups"`
	LogMaxAgeDays                  int               `json:"logMaxAgeDays"`
	LogCompress                    bool              `json:"logCompress"`
	LaunchCommand                  *string           `json:"launchCommand,omitempty"`
	AllowCheckoutNodes             bool              `json:"allowCheckoutNodes"`
	AllowedIPs                     []string          `json:"allowedIPs"`
	AutoPruneOnFetch               bool              `json:"autoPruneOnFetch"`
	GitVersionCheckOverride        bool              `json:"gitVersionCheckOverride"`
	UngitVersionCheckOverride      bool              `json:"ungitVersionCheckOverride"`
	AutoStashAndPop                bool              `json:"autoStashAndPop"`
	FileSeparator                  string            `json:"fileSeparator"`
	DisableDiscardWarning          bool              `json:"disableDiscardWarning"`
	DisableDiscardMuteTime         int               `json:"disableDiscardMuteTime"`
	LockConflictRetryCount         int               `json:"lockConflictRetryCount"`
	AutoCheckoutOnBranchCreate     bool              `json:"autoCheckoutOnBranchCreate"`
	AlwaysLoadActiveBranch         bool              `json:"alwaysLoadActiveBranch"`
	MaxActiveBranchSearchIteration int               `json:"maxActiveBranchSearchIteration"`
	NumberOfNodesPerLoad           int               `json:"numberOfNodesPerLoad"`
	MergeTool                      any               `json:"mergeTool"`
	DiffType                       any               `json:"diffType"`
	Theme                          string            `json:"theme"`
	IgnoreWhiteSpaceDiff           bool              `json:"ignoreWhiteSpaceDiff"`
	TabSize                        any               `json:"tabSize"`
	NumRefsToShow                  int               `json:"numRefsToShow"`
	IsForceGPGSign                 bool              `json:"isForceGPGSign"`
	DefaultRepositories            []string          `json:"defaultRepositories"`
	UngitBindIP                    string            `json:"ungitBindIp"`
	IsAnimate                      bool              `json:"isAnimate"`
	IsDisableProgressBar           bool              `json:"isDisableProgressBar"`
	GitBinPath                     *string           `json:"gitBinPath"`
	IsEnableNumStat                bool              `json:"isEnableNumStat"`

	// Runtime-only options.
	ForcedLaunchPath *string `json:"-"`
	CLIConfigOnly    bool    `json:"-"`
	ShowVersion      bool    `json:"-"`
}

func Default() Config {
	return Config{
		Port:                           DefaultPort,
		URLBase:                        DefaultURLBase,
		RootPath:                       "",
		LogRESTRequests:                true,
		LogGitCommands:                 true,
		Bugtracking:                    false,
		Authentication:                 false,
		Users:                          map[string]string{},
		ShowRebaseAndMergeOnlyOnRefs:   true,
		MaxConcurrentGitOperations:     4,
		LaunchBrowser:                  true,
		NoFFMerge:                      true,
		AutoFetch:                      true,
		LogLevel:                       "info",
		LogMaxSizeMB:                   50,
		LogMaxBackups:                  10,
		LogMaxAgeDays:                  30,
		LogCompress:                    true,
		AllowCheckoutNodes:             false,
		AllowedIPs:                     nil,
		AutoPruneOnFetch:               true,
		GitVersionCheckOverride:        false,
		UngitVersionCheckOverride:      false,
		AutoStashAndPop:                true,
		FileSeparator:                  string(os.PathSeparator),
		DisableDiscardWarning:          false,
		DisableDiscardMuteTime:         5 * 60 * 1000,
		LockConflictRetryCount:         3,
		AutoCheckoutOnBranchCreate:     false,
		AlwaysLoadActiveBranch:         false,
		MaxActiveBranchSearchIteration: -1,
		NumberOfNodesPerLoad:           25,
		MergeTool:                      false,
		DiffType:                       nil,
		Theme:                          "system",
		IgnoreWhiteSpaceDiff:           false,
		TabSize:                        nil,
		NumRefsToShow:                  5,
		IsForceGPGSign:                 false,
		DefaultRepositories:            []string{},
		UngitBindIP:                    DefaultBindIP,
		IsAnimate:                      true,
		IsDisableProgressBar:           false,
		GitBinPath:                     nil,
		IsEnableNumStat:                true,
	}
}

func Parse(args []string) (Config, error) {
	cfg := Default()
	cfg.CLIConfigOnly = hasFlag(args, "cliconfigonly")
	if !cfg.CLIConfigOnly {
		if err := loadUserRC(&cfg); err != nil {
			return Config{}, err
		}
	}

	fs := flag.NewFlagSet("ungit-go", flag.ContinueOnError)
	if hasHelpFlag(args) {
		fs.SetOutput(os.Stdout)
	} else {
		// main emits one concise bootstrap error; suppress flag's duplicate.
		fs.SetOutput(io.Discard)
	}

	var gitBinPath string
	var forcedLaunchPath string
	var forcedLaunchPathSet bool
	var usersJSON string
	var allowedIPsJSON string
	var defaultReposJSON string
	var mergeToolRaw string
	var diffTypeRaw string
	var tabSizeRaw string
	var logDirectory string
	var launchCommand string
	var autoShutdownTimeout int
	var autoShutdownSet bool

	fs.IntVar(&cfg.Port, "port", cfg.Port, "port Ungit-Go is exposed on")
	fs.StringVar(&cfg.UngitBindIP, "ungitBindIp", cfg.UngitBindIP, "IP address to bind")
	fs.StringVar(&cfg.URLBase, "urlBase", cfg.URLBase, "base URL used when launching the browser")
	fs.StringVar(&cfg.RootPath, "rootPath", cfg.RootPath, "URL root path")
	fs.BoolVar(&cfg.Authentication, "authentication", cfg.Authentication, "enable username/password authentication")
	fs.StringVar(&usersJSON, "users", "", "JSON username/password map")
	fs.BoolVar(&cfg.LogRESTRequests, "logRESTRequests", cfg.LogRESTRequests, "log REST requests")
	fs.BoolVar(&cfg.LogGitCommands, "logGitCommands", cfg.LogGitCommands, "log Git commands")
	fs.BoolVar(&cfg.LogGitOutput, "logGitOutput", cfg.LogGitOutput, "log Git output")
	fs.BoolVar(&cfg.Bugtracking, "bugtracking", cfg.Bugtracking, "enable anonymous bug reports")
	fs.BoolVar(&cfg.ShowRebaseAndMergeOnlyOnRefs, "showRebaseAndMergeOnlyOnRefs", cfg.ShowRebaseAndMergeOnlyOnRefs, "show rebase/merge only on refs")
	fs.IntVar(&cfg.MaxConcurrentGitOperations, "maxConcurrentGitOperations", cfg.MaxConcurrentGitOperations, "maximum concurrent Git operations")
	fs.BoolVar(&cfg.LaunchBrowser, "launchBrowser", cfg.LaunchBrowser, "launch the default browser")
	fs.BoolVar(&cfg.LaunchBrowser, "b", cfg.LaunchBrowser, "alias for launchBrowser")
	fs.BoolVar(&cfg.NoFFMerge, "noFFMerge", cfg.NoFFMerge, "use --no-ff for merges")
	fs.BoolVar(&cfg.AutoFetch, "autoFetch", cfg.AutoFetch, "automatically fetch remotes")
	fs.BoolVar(&cfg.Dev, "dev", cfg.Dev, "development/test mode")
	fs.StringVar(&cfg.LogLevel, "logLevel", cfg.LogLevel, "trace, debug, info, warn, or error")
	fs.IntVar(&cfg.LogMaxSizeMB, "logMaxSizeMB", cfg.LogMaxSizeMB, "maximum log file size before rotation in MB")
	fs.IntVar(&cfg.LogMaxBackups, "logMaxBackups", cfg.LogMaxBackups, "maximum rotated log files to retain")
	fs.IntVar(&cfg.LogMaxAgeDays, "logMaxAgeDays", cfg.LogMaxAgeDays, "maximum rotated log age in days")
	fs.BoolVar(&cfg.LogCompress, "logCompress", cfg.LogCompress, "compress rotated log files")
	fs.BoolVar(&cfg.AllowCheckoutNodes, "allowCheckoutNodes", cfg.AllowCheckoutNodes, "allow detached-head checkout")
	fs.BoolVar(&cfg.AutoPruneOnFetch, "autoPruneOnFetch", cfg.AutoPruneOnFetch, "prune on fetch")
	fs.BoolVar(&cfg.GitVersionCheckOverride, "gitVersionCheckOverride", cfg.GitVersionCheckOverride, "ignore Git version check")
	fs.BoolVar(&cfg.GitVersionCheckOverride, "o", cfg.GitVersionCheckOverride, "alias for gitVersionCheckOverride")
	fs.BoolVar(&cfg.UngitVersionCheckOverride, "ungitVersionCheckOverride", cfg.UngitVersionCheckOverride, "ignore Ungit-Go update check")
	fs.BoolVar(&cfg.AutoStashAndPop, "autoStashAndPop", cfg.AutoStashAndPop, "stash around checkout/reset/cherry-pick")
	fs.BoolVar(&cfg.DisableDiscardWarning, "disableDiscardWarning", cfg.DisableDiscardWarning, "disable discard warning")
	fs.IntVar(&cfg.DisableDiscardMuteTime, "disableDiscardMuteTime", cfg.DisableDiscardMuteTime, "discard warning mute duration in ms")
	fs.IntVar(&cfg.LockConflictRetryCount, "lockConflictRetryCount", cfg.LockConflictRetryCount, "Git index-lock retry count")
	fs.BoolVar(&cfg.AutoCheckoutOnBranchCreate, "autoCheckoutOnBranchCreate", cfg.AutoCheckoutOnBranchCreate, "checkout a newly-created branch")
	fs.BoolVar(&cfg.AlwaysLoadActiveBranch, "alwaysLoadActiveBranch", cfg.AlwaysLoadActiveBranch, "deprecated active branch loading mode")
	fs.IntVar(&cfg.MaxActiveBranchSearchIteration, "maxActiveBranchSearchIteration", cfg.MaxActiveBranchSearchIteration, "active-branch search limit")
	fs.IntVar(&cfg.NumberOfNodesPerLoad, "numberOfNodesPerLoad", cfg.NumberOfNodesPerLoad, "commit nodes per load")
	fs.StringVar(&cfg.Theme, "theme", cfg.Theme, "system, dark, or light")
	fs.BoolVar(&cfg.IgnoreWhiteSpaceDiff, "ignoreWhiteSpaceDiff", cfg.IgnoreWhiteSpaceDiff, "ignore whitespace in diffs")
	fs.IntVar(&cfg.NumRefsToShow, "numRefsToShow", cfg.NumRefsToShow, "refs displayed per commit")
	fs.BoolVar(&cfg.IsForceGPGSign, "isForceGPGSign", cfg.IsForceGPGSign, "force GPG signing")
	fs.BoolVar(&cfg.IsAnimate, "isAnimate", cfg.IsAnimate, "enable frontend animation")
	fs.BoolVar(&cfg.IsDisableProgressBar, "isDisableProgressBar", cfg.IsDisableProgressBar, "disable frontend progress bar")
	fs.BoolVar(&cfg.IsEnableNumStat, "isEnableNumStat", cfg.IsEnableNumStat, "enable status numstat")
	fs.BoolVar(&cfg.CLIConfigOnly, "cliconfigonly", cfg.CLIConfigOnly, "ignore rc files")
	fs.BoolVar(&cfg.ShowVersion, "version", false, "print version")
	fs.BoolVar(&cfg.ShowVersion, "v", false, "alias for version")
	fs.StringVar(&gitBinPath, "gitBinPath", "", "directory containing the git executable")
	fs.StringVar(&allowedIPsJSON, "allowedIPs", "", "JSON array of allowed IPs")
	fs.StringVar(&defaultReposJSON, "defaultRepositories", "", "JSON array of default repositories")
	fs.StringVar(&mergeToolRaw, "mergeTool", "", "merge tool: false, true, or tool name")
	fs.StringVar(&diffTypeRaw, "diffType", "", "textdiff or sidebysidediff")
	fs.StringVar(&tabSizeRaw, "tabSize", "", "tab size")
	fs.StringVar(&logDirectory, "logDirectory", "", "directory for logs")
	fs.StringVar(&launchCommand, "launchCommand", "", "custom command; %U is replaced by URL")
	fs.Func("autoShutdownTimeout", "shutdown after inactivity in milliseconds", func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("invalid autoShutdownTimeout %q", v)
		}
		autoShutdownTimeout, autoShutdownSet = n, true
		return nil
	})
	fs.Func("forcedLaunchPath", "repository path to open after startup", func(v string) error {
		forcedLaunchPath = v
		forcedLaunchPathSet = true
		return nil
	})

	normalized := normalizeBooleanNegations(args)
	if err := fs.Parse(normalized); err != nil {
		return Config{}, err
	}
	if fs.NArg() != 0 {
		return Config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return Config{}, fmt.Errorf("invalid port %d", cfg.Port)
	}
	if cfg.MaxConcurrentGitOperations < 1 {
		return Config{}, fmt.Errorf("maxConcurrentGitOperations must be at least 1")
	}
	if cfg.Theme != "system" && cfg.Theme != "dark" && cfg.Theme != "light" {
		return Config{}, fmt.Errorf("invalid theme %q", cfg.Theme)
	}
	if level := strings.ToLower(strings.TrimSpace(cfg.LogLevel)); level != "trace" && level != "debug" && level != "info" && level != "warn" && level != "warning" && level != "error" {
		return Config{}, fmt.Errorf("invalid logLevel %q", cfg.LogLevel)
	}
	if cfg.LogMaxSizeMB < 1 {
		return Config{}, fmt.Errorf("logMaxSizeMB must be at least 1")
	}
	if cfg.LogMaxBackups < 1 {
		return Config{}, fmt.Errorf("logMaxBackups must be at least 1")
	}
	if cfg.LogMaxAgeDays < 0 {
		return Config{}, fmt.Errorf("logMaxAgeDays cannot be negative")
	}
	if usersJSON != "" {
		if err := json.Unmarshal([]byte(usersJSON), &cfg.Users); err != nil {
			return Config{}, fmt.Errorf("invalid --users JSON: %w", err)
		}
	}
	if allowedIPsJSON != "" {
		if err := json.Unmarshal([]byte(allowedIPsJSON), &cfg.AllowedIPs); err != nil {
			return Config{}, fmt.Errorf("invalid --allowedIPs JSON: %w", err)
		}
	}
	if defaultReposJSON != "" {
		if err := json.Unmarshal([]byte(defaultReposJSON), &cfg.DefaultRepositories); err != nil {
			return Config{}, fmt.Errorf("invalid --defaultRepositories JSON: %w", err)
		}
	}
	if mergeToolRaw != "" {
		cfg.MergeTool = parseScalar(mergeToolRaw)
	}
	if diffTypeRaw != "" {
		cfg.DiffType = parseScalar(diffTypeRaw)
	}
	if tabSizeRaw != "" {
		cfg.TabSize = parseScalar(tabSizeRaw)
	}
	if logDirectory != "" {
		clean := filepath.Clean(logDirectory)
		cfg.LogDirectory = &clean
	}
	if launchCommand != "" {
		cfg.LaunchCommand = &launchCommand
	}
	if autoShutdownSet {
		cfg.AutoShutdownTimeout = &autoShutdownTimeout
	}
	cfg.RootPath = normalizeRootPath(cfg.RootPath)
	if gitBinPath != "" {
		p := gitBinPath
		cfg.GitBinPath = &p
	}
	if forcedLaunchPathSet {
		clean := forcedLaunchPath
		if forcedLaunchPath != "" && forcedLaunchPath != "null" {
			clean = filepath.Clean(forcedLaunchPath)
		} else {
			clean = ""
		}
		cfg.ForcedLaunchPath = &clean
	}
	if cfg.AlwaysLoadActiveBranch {
		cfg.MaxActiveBranchSearchIteration = 25
	}
	return cfg, nil
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "-help" {
			return true
		}
	}
	return false
}

func hasFlag(args []string, name string) bool {
	prefix := "--" + name
	for _, arg := range args {
		if arg == prefix || strings.HasPrefix(arg, prefix+"=") {
			return true
		}
	}
	return false
}

func normalizeBooleanNegations(args []string) []string {
	bools := map[string]bool{
		"launchBrowser": true, "authentication": true, "logRESTRequests": true,
		"logGitCommands": true, "logGitOutput": true, "bugtracking": true,
		"showRebaseAndMergeOnlyOnRefs": true, "noFFMerge": true, "autoFetch": true,
		"dev": true, "logCompress": true, "allowCheckoutNodes": true, "autoPruneOnFetch": true,
		"gitVersionCheckOverride": true, "ungitVersionCheckOverride": true,
		"autoStashAndPop": true, "disableDiscardWarning": true,
		"autoCheckoutOnBranchCreate": true, "alwaysLoadActiveBranch": true,
		"ignoreWhiteSpaceDiff": true, "isForceGPGSign": true, "isAnimate": true,
		"isDisableProgressBar": true, "isEnableNumStat": true,
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--no-b" {
			out = append(out, "--launchBrowser=false")
			continue
		}
		if strings.HasPrefix(arg, "--no-") && bools[strings.TrimPrefix(arg, "--no-")] {
			out = append(out, "--"+strings.TrimPrefix(arg, "--no-")+"=false")
			continue
		}
		out = append(out, arg)
	}
	return out
}

func parseScalar(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

func loadUserRC(cfg *Config) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".ungitrc"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read ~/.ungitrc: %w", err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("read ~/.ungitrc: %w", err)
	}
	// JSON null is semantically distinct from an absent forcedLaunchPath in
	// Ungit compatibility: null means open the home screen, while absence means open cwd.
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if value, ok := raw["forcedLaunchPath"]; ok {
			if strings.TrimSpace(string(value)) == "null" {
				empty := ""
				cfg.ForcedLaunchPath = &empty
			}
		}
	}
	return nil
}

func normalizeRootPath(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || root == "/" {
		return ""
	}
	if !strings.HasPrefix(root, "/") {
		root = "/" + root
	}
	return strings.TrimRight(root, "/")
}
