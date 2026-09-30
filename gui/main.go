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
	"unsafe"

	"github.com/jchv/go-webview2"
	"github.com/kardianos/service"
	"golang.org/x/sys/windows"
)

// --- window position restore across an update relaunch (Windows) ---
var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	pGetWindowRect = user32.NewProc("GetWindowRect")
	pSetWindowPos  = user32.NewProc("SetWindowPos")
	pSetForeground = user32.NewProc("SetForegroundWindow")
	pShowWindow    = user32.NewProc("ShowWindow")
)

type winRect struct{ Left, Top, Right, Bottom int32 }

func windowFile() string { return filepath.Join(dataDir(), "window.json") }

// saveWindowForRestore records the window's rectangle so the next launch (after
// an update relaunch) reopens it the same size and place. Written just before
// relaunching; consumed once on the next start.
func saveWindowForRestore(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	var r winRect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if r.Right-r.Left <= 0 || r.Bottom-r.Top <= 0 {
		return
	}
	b, _ := json.Marshal(map[string]int32{
		"x": r.Left, "y": r.Top, "w": r.Right - r.Left, "h": r.Bottom - r.Top, "restore": 1,
	})
	_ = os.MkdirAll(dataDir(), 0o755)
	_ = os.WriteFile(windowFile(), b, 0o644)
}

// restoreWindowIfPending applies a saved rectangle ONCE (only when the restore
// flag is set by an update relaunch), then clears it, so normal opens are
// unaffected. Also brings the window to the front.
func restoreWindowIfPending(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	b, err := os.ReadFile(windowFile())
	if err != nil {
		return
	}
	var m map[string]int32
	if json.Unmarshal(b, &m) != nil || m["restore"] == 0 {
		return
	}
	const swpNoZorder = 0x0004
	const swShow = 5
	pShowWindow.Call(hwnd, swShow)
	pSetWindowPos.Call(hwnd, 0,
		uintptr(uint32(m["x"])), uintptr(uint32(m["y"])),
		uintptr(uint32(m["w"])), uintptr(uint32(m["h"])), swpNoZorder)
	pSetForeground.Call(hwnd)
	_ = os.Remove(windowFile()) // one-shot
}

const serviceExe = "selfguard-svc.exe"
const appName = "SelfGuard"

// guiLog appends a line to gui.log so we can see what the GUI's self-update does
// on launch and during an install (the service can't observe the GUI process).
func guiLog(format string, a ...interface{}) {
	f, err := os.OpenFile(filepath.Join(dataDir(), "gui.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(time.Now().Format("2006-01-02 15:04:05") + "  " + fmt.Sprintf(format, a...) + "\r\n")
}

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

// applyStagedGuiOnLaunch makes "open the app" always current: if the service has
// staged a newer GUI (SelfGuard.new.exe), install it now and relaunch. It tries
// a direct rename first (works when the folder is writable); if that's refused
// (device locked down), it asks the SYSTEM service to do the swap, then relaunches.
// Either way the staged file is consumed, so this never loops.
// swapStagedGui installs a staged newer GUI (SelfGuard.new.exe) in place and
// returns true if it did. Direct rename when the folder is writable; otherwise
// it asks the SYSTEM service to do the swap (locked-down case).
func swapStagedGui() bool {
	self, err := os.Executable()
	if err != nil {
		guiLog("swapStagedGui: Executable() err=%v", err)
		return false
	}
	staged := filepath.Join(filepath.Dir(self), "SelfGuard.new.exe")
	if _, err := os.Stat(staged); err != nil {
		guiLog("swapStagedGui: no staged GUI (%s): %v", staged, err)
		return false // nothing staged
	}
	guiLog("swapStagedGui: staged GUI present, attempting direct rename")
	if e1 := os.Rename(self, self+".old"); e1 == nil { // folder writable
		if e2 := os.Rename(staged, self); e2 == nil {
			guiLog("swapStagedGui: direct swap OK")
			return true
		} else {
			guiLog("swapStagedGui: move new->self failed: %v", e2)
			_ = os.Rename(self+".old", self) // restore on failure
		}
	} else {
		guiLog("swapStagedGui: rename self->.old failed (locked?): %v", e1)
	}
	// Locked folder: let the SYSTEM service perform the swap.
	guiLog("swapStagedGui: handing off to service apply_gui")
	_, _ = sgCommand("apply_gui", "")
	time.Sleep(2 * time.Second)
	_, err = os.Stat(staged)
	guiLog("swapStagedGui: after apply_gui, staged gone=%v", err != nil)
	return err != nil // applied if the staged file is now gone
}

// relaunchSelf starts a fresh copy of this exe and exits the current process.
func relaunchSelf() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	_ = exec.Command(self).Start()
	os.Exit(0)
}

// applyStagedGuiOnLaunch runs at startup: if a newer GUI is staged, install it
// and relaunch so the app is always current when opened.
func applyStagedGuiOnLaunch() {
	guiLog("launch: GUI version %s starting; checking for staged update", version)
	if self, err := os.Executable(); err == nil {
		_ = os.Remove(self + ".old")
	}
	if swapStagedGui() {
		guiLog("launch: staged GUI applied, relaunching")
		relaunchSelf()
	} else {
		guiLog("launch: no staged GUI to apply")
	}
}

func main() {
	applyStagedGuiOnLaunch() // self-update the GUI if the service staged a newer one

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
	must("sgGuiVersion", func() string { return version })
	must("sgLogos", func() string { return logosJSON() })
	must("sgGuiStaged", func() bool {
		self, err := os.Executable()
		if err != nil {
			return false
		}
		_, e := os.Stat(filepath.Join(filepath.Dir(self), "SelfGuard.new.exe"))
		return e == nil
	})
	must("sgFinishUpdate", func() bool {
		guiLog("sgFinishUpdate: called (GUI %s)", version)
		saveWindowForRestore(uintptr(w.Window())) // remember size/place for the reopen
		if swapStagedGui() {
			guiLog("sgFinishUpdate: GUI swapped, relaunching")
			relaunchSelf() // process exits here; the new build takes over
			return true
		}
		guiLog("sgFinishUpdate: nothing to swap; returning false")
		_ = os.Remove(windowFile()) // no relaunch -> don't reposition on next open
		return false
	})
	must("sgRestartBrowser", func() error {
		// User-initiated only. Launches Chrome to chrome://restart, which makes a
		// running Chrome restart itself and drop cached pages. We never force-kill
		// the browser from the background.
		return exec.Command("cmd", "/c", "start", "", "chrome", "chrome://restart").Start()
	})

	if self, e := os.Executable(); e == nil {
		_ = os.Remove(self + ".old") // cleanup after a GUI self-update
	}

	restoreWindowIfPending(uintptr(w.Window())) // reopen same size/place after an update
	w.SetHtml(uiHTML)
	w.Run()
}
