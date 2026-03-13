package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pchaganti/claude-session-manager/daemon/internal/ctlserver"
	"github.com/pchaganti/claude-session-manager/daemon/internal/ghostty"
	"github.com/pchaganti/claude-session-manager/daemon/internal/hookserver"
	"github.com/pchaganti/claude-session-manager/daemon/internal/notify"
	"github.com/pchaganti/claude-session-manager/daemon/internal/scanner"
	"github.com/pchaganti/claude-session-manager/daemon/internal/state"
)

const (
	plistLabel    = "com.csm.daemon"
	pluginDirName = "csm"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			install()
			return
		case "uninstall":
			uninstall()
			return
		case "version":
			fmt.Println("csm-daemon v0.1.0")
			return
		}
	}

	runDaemon()
}

func runDaemon() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("csm-daemon starting")

	st := state.New()

	// Start hook server.
	hookSrv, err := hookserver.New(hookserver.DefaultSocket, st)
	if err != nil {
		log.Fatalf("hook server: %v", err)
	}
	defer hookSrv.Close()
	go hookSrv.Serve()

	// Start control server.
	ctlSrv, err := ctlserver.New(ctlserver.DefaultSocket, st)
	if err != nil {
		log.Fatalf("control server: %v", err)
	}
	defer ctlSrv.Close()
	go ctlSrv.Serve()

	// Start scanner loop (fallback discovery).
	sc := scanner.New()
	stopScanner := make(chan struct{})
	go scanner.RunLoop(sc, st, 5*time.Second, stopScanner)

	// Start Ghostty correlation loop.
	go ghosttyLoop(st, stopScanner)

	// Start notification loop.
	notifier := notify.New()
	go notifyLoop(st, notifier, stopScanner)

	log.Println("csm-daemon ready")

	// Wait for signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("csm-daemon shutting down")
	close(stopScanner)
}

func ghosttyLoop(st *state.Manager, stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sessions := st.GetSessions()
			for _, s := range sessions {
				if s.CWD != "" && s.GhosttyTab == "" {
					tabName := ghostty.CorrelateTab(s.CWD)
					if tabName != "" {
						st.SetGhosttyTab(s.SessionID, tabName)
					}
				}
			}
		case <-stop:
			return
		}
	}
}

func notifyLoop(st *state.Manager, notifier *notify.Notifier, stop <-chan struct{}) {
	ch := st.Subscribe()
	defer st.Unsubscribe(ch)

	for {
		select {
		case <-ch:
			sessions := st.GetSessions()
			notifier.Check(sessions)
		case <-stop:
			return
		}
	}
}

// --- install / uninstall ---

func install() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}

	// 1. Install CC plugin.
	pluginDst := filepath.Join(home, ".claude", "plugins", pluginDirName)
	pluginSrc := findPluginSource()
	if pluginSrc == "" {
		log.Fatal("cannot find plugin/ directory — run install from repo root or daemon/ directory")
	}

	if err := os.MkdirAll(filepath.Dir(pluginDst), 0o755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	// Remove existing plugin.
	_ = os.RemoveAll(pluginDst)
	// Copy plugin directory.
	if err := copyDir(pluginSrc, pluginDst); err != nil {
		log.Fatalf("copy plugin: %v", err)
	}
	fmt.Printf("installed plugin to %s\n", pluginDst)

	// 2. Install launchd plist.
	daemonBin := daemonBinaryPath()
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	_ = os.MkdirAll(plistDir, 0o755)
	plistPath := filepath.Join(plistDir, plistLabel+".plist")

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>/tmp/csm-daemon.log</string>
    <key>StandardErrorPath</key><string>/tmp/csm-daemon.log</string>
</dict>
</plist>
`, plistLabel, daemonBin)

	if err := os.WriteFile(plistPath, []byte(plistContent), 0o644); err != nil {
		log.Fatalf("write plist: %v", err)
	}
	fmt.Printf("installed plist to %s\n", plistPath)

	// Load service.
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		log.Printf("warning: launchctl load failed: %v", err)
	} else {
		fmt.Println("loaded launchd service")
	}

	fmt.Println("installation complete")
}

func uninstall() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}

	// 1. Unload launchd service.
	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
	if _, err := os.Stat(plistPath); err == nil {
		_ = exec.Command("launchctl", "unload", plistPath).Run()
		_ = os.Remove(plistPath)
		fmt.Println("removed launchd plist")
	}

	// 2. Remove plugin.
	pluginDst := filepath.Join(home, ".claude", "plugins", pluginDirName)
	if _, err := os.Stat(pluginDst); err == nil {
		_ = os.RemoveAll(pluginDst)
		fmt.Println("removed plugin")
	}

	// 3. Clean up sockets.
	_ = os.Remove(hookserver.DefaultSocket)
	_ = os.Remove(ctlserver.DefaultSocket)

	fmt.Println("uninstallation complete")
}

func findPluginSource() string {
	// Try relative to executable.
	exe, _ := os.Executable()
	if exe != "" {
		dir := filepath.Dir(exe)
		// Check ../plugin/ (if binary is in daemon/ or bin/)
		for _, candidate := range []string{
			filepath.Join(dir, "..", "plugin"),
			filepath.Join(dir, "plugin"),
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}
	}

	// Try relative to cwd.
	cwd, _ := os.Getwd()
	for _, candidate := range []string{
		filepath.Join(cwd, "plugin"),
		filepath.Join(cwd, "..", "plugin"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

func daemonBinaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "/usr/local/bin/csm-daemon"
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return exe
	}
	return abs
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode()); err != nil {
			return err
		}
		return nil
	})
}
