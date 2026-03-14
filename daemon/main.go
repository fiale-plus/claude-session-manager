package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
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

	// 3. Install Ghostty integration.
	installGhostty(home)

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

	// 3. Remove Ghostty integration.
	uninstallGhostty(home)

	// 4. Clean up sockets.
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

// --- Ghostty integration ---

const ghosttyConfigMarker = "# --- CSM (Claude Session Manager) ---"

func ghosttyConfigDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, ".config", "ghostty")
	}
	// Linux: XDG_CONFIG_HOME or ~/.config
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ghostty")
	}
	return filepath.Join(home, ".config", "ghostty")
}

func installGhostty(home string) {
	configDir := ghosttyConfigDir(home)
	configPath := filepath.Join(configDir, "config")

	// Check if Ghostty is installed by looking for its config dir or binary.
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		// Check if ghostty binary exists.
		if _, err := exec.LookPath("ghostty"); err != nil {
			fmt.Println("skipping Ghostty integration (Ghostty not detected)")
			return
		}
		// Ghostty binary exists but no config dir — create it.
		_ = os.MkdirAll(configDir, 0o755)
	}

	// Install CSM shader.
	shaderDir := filepath.Join(configDir, "shaders")
	_ = os.MkdirAll(shaderDir, 0o755)

	shaderSrc := findShaderSource()
	if shaderSrc != "" {
		shaderDst := filepath.Join(shaderDir, "csm-status.glsl")
		data, err := os.ReadFile(shaderSrc)
		if err == nil {
			if err := os.WriteFile(shaderDst, data, 0o644); err == nil {
				fmt.Printf("installed shader to %s\n", shaderDst)
			}
		}
	}

	// Append keybinds to Ghostty config (idempotent).
	if alreadyHasCSMConfig(configPath) {
		fmt.Println("Ghostty config already has CSM keybinds")
		return
	}

	snippet := fmt.Sprintf(`
%s
# Keybinds for CSM TUI (approve/reject/queue)
# These send escape sequences that csm-tui intercepts.
keybind = ctrl+shift+y=text:\x01csm:approve\n
keybind = ctrl+shift+n=text:\x01csm:reject\n
keybind = ctrl+shift+q=text:\x01csm:queue\n
%s
`, ghosttyConfigMarker, ghosttyConfigMarker+" END")

	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("warning: cannot write Ghostty config: %v", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(snippet); err != nil {
		log.Printf("warning: cannot append to Ghostty config: %v", err)
		return
	}

	fmt.Printf("added CSM keybinds to %s\n", configPath)
}

func uninstallGhostty(home string) {
	configDir := ghosttyConfigDir(home)
	configPath := filepath.Join(configDir, "config")

	// Remove shader.
	shaderPath := filepath.Join(configDir, "shaders", "csm-status.glsl")
	if _, err := os.Stat(shaderPath); err == nil {
		_ = os.Remove(shaderPath)
		fmt.Println("removed Ghostty shader")
	}

	// Remove CSM config block from Ghostty config.
	if !alreadyHasCSMConfig(configPath) {
		return
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	var lines []string
	inCSMBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ghosttyConfigMarker {
			inCSMBlock = true
			continue
		}
		if strings.TrimSpace(line) == ghosttyConfigMarker+" END" {
			inCSMBlock = false
			continue
		}
		if !inCSMBlock {
			lines = append(lines, line)
		}
	}

	cleaned := strings.Join(lines, "\n")
	// Remove trailing blank lines left by the block removal.
	cleaned = strings.TrimRight(cleaned, "\n") + "\n"

	if err := os.WriteFile(configPath, []byte(cleaned), 0o644); err != nil {
		log.Printf("warning: cannot clean Ghostty config: %v", err)
		return
	}
	fmt.Println("removed CSM keybinds from Ghostty config")
}

func alreadyHasCSMConfig(configPath string) bool {
	f, err := os.Open(configPath)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == ghosttyConfigMarker {
			return true
		}
	}
	return false
}

func findShaderSource() string {
	exe, _ := os.Executable()
	if exe != "" {
		dir := filepath.Dir(exe)
		for _, candidate := range []string{
			filepath.Join(dir, "..", "ghostty", "csm-status.glsl"),
			filepath.Join(dir, "ghostty", "csm-status.glsl"),
		} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	cwd, _ := os.Getwd()
	for _, candidate := range []string{
		filepath.Join(cwd, "ghostty", "csm-status.glsl"),
		filepath.Join(cwd, "..", "ghostty", "csm-status.glsl"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
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
