package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ungitgoassets "github.com/egomarker/ungit-go"
	"github.com/egomarker/ungit-go/internal/browser"
	"github.com/egomarker/ungit-go/internal/config"
	"github.com/egomarker/ungit-go/internal/credentials"
	gitinfo "github.com/egomarker/ungit-go/internal/git"
	ungitserver "github.com/egomarker/ungit-go/internal/server"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "credential-helper" {
		if err := credentials.RunHelper(os.Args[2:], os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
	if cfg.ShowVersion {
		fmt.Println(packageVersion())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	version := gitinfo.GetVersionInfo(ctx, cfg.GitBinPath)
	cancel()
	if version.Version == "unkown" {
		log.Fatalf("Can't run %q --version. Is git installed and available in your path?", gitinfo.Executable(cfg.GitBinPath))
	}
	if !version.Satisfied && !cfg.GitVersionCheckOverride {
		log.Fatal(version.Error)
	}

	app, err := ungitserver.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.UngitBindIP, cfg.Port))
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) && cfg.Port != 0 {
			launchConfigured(cfg, browserURL(cfg, cfg.Port))
			return
		}
		log.Fatal(err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
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

	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server failed: %v", err)
		}
	}()

	launchURL := browserURL(cfg, actualPort)
	fmt.Printf("## Ungit-Go started ##\n")
	fmt.Printf("Ungit-Go %s listening at %s\n", app.Version(), launchURL)
	fmt.Printf("Git %s\n", version.Version)
	launchConfigured(cfg, launchURL)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	waitForStop(stop, activity, cfg.AutoShutdownTimeout)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
}

func waitForStop(stop <-chan os.Signal, activity <-chan struct{}, timeoutMS *int) {
	if timeoutMS == nil || *timeoutMS <= 0 {
		<-stop
		return
	}
	d := time.Duration(*timeoutMS) * time.Millisecond
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(d)
		case <-timer.C:
			log.Printf("Shutting down Ungit-Go due to inactivity. (autoShutdownTimeout is set to %d ms)", *timeoutMS)
			return
		}
	}
}

func launchConfigured(cfg config.Config, launchURL string) {
	if cfg.LaunchCommand != nil && *cfg.LaunchCommand != "" {
		command := strings.ReplaceAll(*cfg.LaunchCommand, "%U", launchURL)
		go func() {
			if err := browser.RunCommand(command); err != nil {
				log.Printf("failed to exec custom launchCommand: %v", err)
				return
			}
			if cfg.LaunchBrowser {
				if err := browser.Open(launchURL); err != nil {
					log.Printf("failed to launch browser: %v", err)
				}
			}
		}()
		return
	}
	if cfg.LaunchBrowser {
		if err := browser.Open(launchURL); err != nil {
			log.Printf("failed to launch browser: %v", err)
		}
	}
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
