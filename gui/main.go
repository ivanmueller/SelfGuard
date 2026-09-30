package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jchv/go-webview2"
	"github.com/kardianos/service"
)

const serviceExe = "selfguard-svc.exe"
const appName = "SelfGuard"

// version is stamped at build time via -ldflags "-X main.version=1.6.0".
var version = "dev"

// dataDir mirrors the service: C:\ProgramData\SelfGuard.
func dataDir() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = os.Getenv("PROGRAMDATA")
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), "SelfGuard-data")
	}
	return filepath.Join(base, appName)
}

// apiBase reads the port the running service recorded and returns its API URL.
func apiBase() (string, error) {
	b, err := os.ReadFile(filepath.Join(dataDir(), "api.port"))
	if err != nil {
		return "", err
	}
	port := strings.TrimSpace(string(b))
	if port == "" {
		return "", fmt.Errorf("no port yet")
	}
	return "http://127.0.0.1:" + port, nil
}

// serviceRunning asks Windows directly whether the service is up.
func serviceRunning() bool {
	s, err := service.New(nil, &service.Config{Name: appName})
	if err != nil {
		return false
	}
	st, err := s.Status()
	return err == nil && st == service.StatusRunning
}

func sgStatus() (string, error) {
	base, err := apiBase()
	if err != nil {
		return "", err
	}
	c := &http.Client{Timeout: 8 * time.Second}
	resp, err := c.Get(base + "/status")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func sgCommand(kind, payload string) (string, error) {
	base, err := apiBase()
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]string{"kind": kind, "payload": payload})
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Post(base+"/command", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b), nil
}

// sgInstall relaunches the service exe elevated (UAC) to install and start it.
func sgInstall() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p := filepath.Join(filepath.Dir(exe), serviceExe)
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("%s not found next to this app", serviceExe)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		fmt.Sprintf("Start-Process -FilePath %q -ArgumentList 'install' -Verb RunAs", p))
	return cmd.Run()
}

func main() {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  "SelfGuard",
			Width:  1000,
			Height: 720,
			IconId: 0,
			Center: true,
		},
	})
	if w == nil {
		fmt.Println("Could not start the SelfGuard window. The Microsoft Edge WebView2 Runtime may be missing.")
		fmt.Println("Install it from https://developer.microsoft.com/microsoft-edge/webview2/ and try again.")
		os.Exit(1)
	}
	defer w.Destroy()

	must := func(name string, fn interface{}) {
		if err := w.Bind(name, fn); err != nil {
			fmt.Println("bind", name, "failed:", err)
		}
	}
	must("sgStatus", sgStatus)
	must("sgCommand", sgCommand)
	must("sgServiceRunning", func() bool { return serviceRunning() })
	must("sgInstall", func() error { return sgInstall() })

	if self, e := os.Executable(); e == nil {
		_ = os.Remove(self + ".old") // cleanup after a GUI self-update
	}

	w.SetHtml(uiHTML)
	w.Run()
}
