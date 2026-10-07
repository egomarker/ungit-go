package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	ungitgoassets "github.com/egomarker/ungit-go"
	"github.com/egomarker/ungit-go/internal/browser"
	"github.com/egomarker/ungit-go/internal/config"
	"github.com/egomarker/ungit-go/internal/credentials"
	gitinfo "github.com/egomarker/ungit-go/internal/git"
	"github.com/egomarker/ungit-go/internal/observability"
	ungitserver "github.com/egomarker/ungit-go/internal/server"
)

type loggedOperationalError struct{ error }

func main() {
	if len(os.Args) > 1 && os.Args[1] == "credential-helper" {
		if err := credentials.RunHelper(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "ungit-go credential-helper:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		var logged loggedOperationalError
		if !errors.As(err, &logged) {
			// File logging may not exist yet for parse/bootstrap failures, or may
			// have failed while closing. Emit one concise emergency diagnostic.
			fmt.Fprintln(os.Stderr, "ungit-go:", err)
		}
		os.Exit(1)
	}
}

func run() (returnErr error) {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if cfg.ShowVersion {
		fmt.Println(packageVersion())
		return nil
	}

	versionText := packageVersion()
	logging, err := observability.Start(observability.Options{
		Directory:  cfg.LogDirectory,
		Level:      cfg.LogLevel,
		MaxSizeMB:  cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAgeDays: cfg.LogMaxAgeDays,
		Compress:   cfg.LogCompress,
		Version:    versionText,
	})
	if err != nil {
		return fmt.Errorf("initialize file logging: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			observability.Error(context.Background(), "process.panic", "unhandled process panic", fmt.Errorf("%v", recovered),
				"stack", string(debug.Stack()))
			returnErr = fmt.Errorf("panic: %v", recovered)
		}
		if closeErr := logging.Close(); closeErr != nil {
			returnErr = fmt.Errorf("close log file: %w", closeErr)
			return
		}
		if returnErr != nil {
			returnErr = loggedOperationalError{error: returnErr}
		}
	}()

	processCtx, processCancel := context.WithCancel(context.Background())
	defer processCancel()
	startupFields := append(observability.RuntimeFields(),
		"log_path", logging.Path(),
		"bind_ip", cfg.UngitBindIP,
		"port", cfg.Port,
		"root_path", cfg.RootPath,
		"working_directory", currentWorkingDirectory(),
		"log_level", cfg.LogLevel,
	)
	observability.Info(processCtx, "process.start", "Ungit-Go process starting", startupFields...)

	ctx, cancel := context.WithTimeout(processCtx, 5*time.Second)
	gitVersion := gitinfo.GetVersionInfo(ctx, cfg.GitBinPath)
	cancel()
	if gitVersion.Version == "unkown" {
		err := fmt.Errorf("can't run %q --version; Git may not be installed or available in PATH", gitinfo.Executable(cfg.GitBinPath))
		observability.Error(processCtx, "startup.git_version.failed", "Git version check failed", err)
		return err
	}
	if !gitVersion.Satisfied && !cfg.GitVersionCheckOverride {
		versionErr := errors.New(gitVersion.Error)
		observability.Error(processCtx, "startup.git_version.unsupported", "Git version is unsupported", versionErr,
			"git_version", gitVersion.Version)
		return versionErr
	}
	observability.Info(processCtx, "startup.git_version.completed", "Git version verified", "git_version", gitVersion.Version)

	app, err := ungitserver.New(cfg)
	if err != nil {
		observability.Error(processCtx, "startup.server.failed", "failed to initialize server", err)
		return err
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.UngitBindIP, cfg.Port))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) && cfg.Port != 0 {
			observability.Warn(processCtx, "startup.port.in_use", "configured port is already in use; reusing existing service",
				"bind_ip", cfg.UngitBindIP, "port", cfg.Port)
			launchConfigured(processCtx, cfg, browserURL(cfg, cfg.Port))
			return nil
		}
		observability.Error(processCtx, "startup.listen.failed", "failed to bind HTTP listener", err,
			"bind_ip", cfg.UngitBindIP, "port", cfg.Port)
		return err
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	if err := logging.MarkRunning(); err != nil {
		_ = listener.Close()
		observability.Error(processCtx, "process.run_state.write_failed", "failed to initialize process run state", err)
		return err
	}
	app.SetCredentialEndpoint(fmt.Sprintf("http://127.0.0.1:%d%s", actualPort, cfg.RootPath))

	handler := app.Handler()
	activity := make(chan struct{}, 1)
	if cfg.AutoShutdownTimeout != nil && *cfg.AutoShutdownTimeout > 0 {
		next := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case activity <- struct{}{}:
			default:
			}
			next.ServeHTTP(w, r)
		})
	}

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          observability.StandardLogger("http.server.error"),
	}
	serveErrors := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErrors <- err
	}()

	launchURL := browserURL(cfg, actualPort)
	listenURL := strings.SplitN(launchURL, "#", 2)[0]
	observability.Info(processCtx, "process.ready", "Ungit-Go server is listening",
		"url", listenURL, "git_version", gitVersion.Version, "port", actualPort)
	launchConfigured(processCtx, cfg, launchURL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	reason, serveErr := waitForStop(stop, activity, cfg.AutoShutdownTimeout, serveErrors)
	signal.Stop(stop)
	observability.Info(processCtx, "process.shutdown.started", "Ungit-Go shutdown started", "reason", reason)

	shutdownCtx, shutdownCancel := context.WithTimeout(processCtx, 5*time.Second)
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	shutdownCancel()
	if shutdownErr != nil {
		observability.Error(processCtx, "process.shutdown.failed", "HTTP server shutdown failed", shutdownErr)
		return shutdownErr
	}
	if serveErr != nil {
		observability.Error(processCtx, "http.server.failed", "HTTP server exited unexpectedly", serveErr)
		return serveErr
	}
	observability.Info(processCtx, "process.shutdown.completed", "Ungit-Go shutdown completed", "reason", reason)
	return nil
}

func waitForStop(stop <-chan os.Signal, activity <-chan struct{}, timeoutMS *int, serveErrors <-chan error) (string, error) {
	if timeoutMS == nil || *timeoutMS <= 0 {
		select {
		case signal := <-stop:
			return "signal:" + signal.String(), nil
		case err := <-serveErrors:
			return "http-server-exit", err
		}
	}
	duration := time.Duration(*timeoutMS) * time.Millisecond
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case signal := <-stop:
			return "signal:" + signal.String(), nil
		case err := <-serveErrors:
			return "http-server-exit", err
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(duration)
		case <-timer.C:
			return "inactivity-timeout", nil
		}
	}
}

func launchConfigured(ctx context.Context, cfg config.Config, launchURL string) {
	if cfg.LaunchCommand != nil && *cfg.LaunchCommand != "" {
		command := strings.ReplaceAll(*cfg.LaunchCommand, "%U", launchURL)
		go func() {
			started := time.Now()
			if err := browser.RunCommand(command); err != nil {
				observability.Error(ctx, "browser.command.failed", "custom launch command failed", err,
					"duration_ms", time.Since(started).Milliseconds())
				return
			}
			observability.Info(ctx, "browser.command.completed", "custom launch command completed",
				"duration_ms", time.Since(started).Milliseconds())
			if cfg.LaunchBrowser {
				if err := browser.Open(launchURL); err != nil {
					observability.Error(ctx, "browser.launch.failed", "failed to launch browser", err)
				}
			}
		}()
		return
	}
	if cfg.LaunchBrowser {
		if err := browser.Open(launchURL); err != nil {
			observability.Error(ctx, "browser.launch.failed", "failed to launch browser", err)
		}
	}
}

func currentWorkingDirectory() string {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "<unavailable>"
	}
	return workingDirectory
}

func packageVersion() string {
	data, err := ungitgoassets.FS.ReadFile("package.json")
	if err != nil {
		return "unknown"
	}
	var p struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &p) != nil || p.Version == "" {
		return "unknown"
	}
	return p.Version
}

func browserURL(cfg config.Config, actualPort int) string {
	base := strings.TrimRight(cfg.URLBase, "/")
	if base == "" {
		base = "http://localhost"
	}
	result := fmt.Sprintf("%s:%d%s/", base, actualPort, strings.TrimRight(cfg.RootPath, "/"))
	launchPath := ""
	if cfg.ForcedLaunchPath == nil {
		launchPath, _ = os.Getwd()
	} else {
		launchPath = *cfg.ForcedLaunchPath
	}
	if launchPath != "" {
		result += "#/repository?path=" + encodePathLikeNode(launchPath)
	}
	return result
}

func encodePathLikeNode(value string) string {
	// encodeURIComponent, preserving Ungit-compatible encoded forward
	// slashes so repository paths remain readable in the hash URL.
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("-_.!~*'()", rune(c)) || c == '/' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
