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

// serviceExePath is the fixed installed location of the service binary.
func serviceExePath() string {
	sd := os.Getenv("SystemDrive")
	if sd == "" {
		sd = "C:"
	}
	return sd + `\SelfGuard\selfguard-svc.exe`
}

// applyStagedScript installs the binaries the service already downloaded and
// verified. %[1]s=result, %[2]s=staged svc, %[3]s=installed svc,
// %[4]s=staged gui, %[5]s=installed gui.
const applyStagedScript = `$ErrorActionPreference='Stop'
$res='%[1]s'
try{
  if(Test-Path '%[2]s'){
    sc.exe stop SelfGuard | Out-Null
    $i=0; while((Get-Service SelfGuard).Status -ne 'Stopped' -and $i -lt 20){ Start-Sleep -Milliseconds 500; $i++ }
    if((Get-Service SelfGuard).Status -ne 'Stopped'){ 'ERROR: could not stop the service (hardened against admin stop?). Restore the SDDL first.' | Out-File -Encoding utf8 $res; exit }
    Move-Item '%[2]s' '%[3]s' -Force
    sc.exe start SelfGuard | Out-Null
  }
  if(Test-Path '%[4]s'){
    if(Test-Path ('%[5]s'+'.old')){ Remove-Item ('%[5]s'+'.old') -Force -ErrorAction SilentlyContinue }
    if(Test-Path '%[5]s'){ Move-Item '%[5]s' ('%[5]s'+'.old') -Force }
    Move-Item '%[4]s' '%[5]s' -Force
  }
  'OK: update installed. Close this window and reopen SelfGuard.' | Out-File -Encoding utf8 $res
}catch{ ('ERROR: '+$_) | Out-File -Encoding utf8 $res }
`

// sgApplyUpdate installs the already-downloaded, hash-verified staged update in
// one elevated step. No picker, no download — the service did that. It never
// touches config, so your rules and delay lock carry over unchanged.
func sgApplyUpdate() (string, error) {
	dir := filepath.Dir(serviceExePath())
	stagedSvc := filepath.Join(dir, "selfguard-svc.new.exe")
	self, _ := os.Executable()
	// GUI staged file lives beside the service; install it over the running GUI.
	stagedGui := filepath.Join(dir, "SelfGuard.new.exe")

	tmp, err := os.MkdirTemp("", "sgapply")
	if err != nil {
		return "", err
	}
	res := filepath.Join(tmp, "result.txt")
	ps1 := filepath.Join(tmp, "apply.ps1")
	body := fmt.Sprintf(applyStagedScript, res, stagedSvc, serviceExePath(), stagedGui, self)
	if err := os.WriteFile(ps1, []byte(body), 0o644); err != nil {
		return "", err
	}
	run := exec.Command("powershell", "-NoProfile", "-Command",
		"Start-Process powershell -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','"+ps1+"' -Verb RunAs -Wait")
	if err := run.Run(); err != nil {
		return "", fmt.Errorf("install cancelled (admin prompt declined)")
	}
	b, _ := os.ReadFile(res)
	msg := strings.TrimSpace(strings.TrimPrefix(string(b), "\ufeff"))
	if msg == "" {
		msg = "Update installed."
	}
	return msg, nil
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
	must("sgApplyUpdate", sgApplyUpdate)

	if self, e := os.Executable(); e == nil {
		_ = os.Remove(self + ".old") // cleanup after a GUI self-update
	}

	w.SetHtml(uiHTML)
	w.Run()
}
