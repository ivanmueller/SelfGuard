//go:build windows

package main

import (
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func outputCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, err := cmd.Output()
	return string(b), err
}

// Parses `netsh interface show interface`. Note: assumes an English-language
// Windows (the "Connected" state string is localized on other languages).
var ifaceRe = regexp.MustCompile(`(?m)^\s*\S+\s+(Connected|Disconnected)\s+\S+\s+(.+?)\s*$`)

// interfaces lists adapter names. onlyConnected=false also returns
// disconnected/disabled adapters (used when restoring DNS, so an adapter we
// pointed at 127.0.0.1 earlier isn't missed just because it's unplugged now).
func interfaces(onlyConnected bool) []string {
	out, err := outputCmd("netsh", "interface", "show", "interface")
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		m := ifaceRe.FindStringSubmatch(line)
		if m == nil || (onlyConnected && m[1] != "Connected") {
			continue
		}
		name := strings.TrimSpace(m[2])
		if name == "" || strings.HasPrefix(strings.ToLower(name), "loopback") {
			continue
		}
		names = append(names, name)
	}
	return names
}

func setSystemDNS() error {
	for _, ifc := range interfaces(true) {
		_ = runCmd("netsh", "interface", "ipv4", "set", "dnsservers", "name="+ifc, "static", "127.0.0.1", "primary")
		_ = runCmd("netsh", "interface", "ipv6", "set", "dnsservers", "name="+ifc, "static", "::1", "primary")
	}
	_ = runCmd("ipconfig", "/flushdns")
	return nil
}

// resetSystemDNS puts every adapter back on automatic (DHCP) DNS.
func resetSystemDNS() error {
	for _, ifc := range interfaces(false) {
		_ = runCmd("netsh", "interface", "ipv4", "set", "dnsservers", "name="+ifc, "dhcp")
		_ = runCmd("netsh", "interface", "ipv6", "set", "dnsservers", "name="+ifc, "dhcp")
	}
	_ = runCmd("ipconfig", "/flushdns")
	return nil
}

// dhcpDNSServers returns the IPv4 DNS servers the network handed out via DHCP
// (usually the router). Windows keeps these in DhcpNameServer even while a
// static DNS is set, so they're still known after we point adapters at
// 127.0.0.1. They're used as fallback upstreams: if the network blocks or
// can't reach 1.1.1.1, lookups still work through the router.
func dhcpDNSServers() []string {
	const base = `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces`
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, base, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer root.Close()
	subs, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var out []string
	for _, sub := range subs {
		k, err := registry.OpenKey(root, sub, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, err := k.GetStringValue("DhcpNameServer")
		k.Close()
		if err != nil {
			continue
		}
		for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' }) {
			ip := net.ParseIP(f)
			if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			out = append(out, net.JoinHostPort(ip.String(), "53"))
		}
	}
	return out
}

func setDword(k registry.Key, name string, val uint32) { _ = k.SetDWordValue(name, val) }

func applyPolicies() error {
	if k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Google\Chrome`, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("DnsOverHttpsMode", "off")
		setDword(k, "BuiltInDnsClientEnabled", 0)
		setDword(k, "IncognitoModeAvailability", 1)
		setDword(k, "ForceGoogleSafeSearch", 1)
		setDword(k, "ForceYouTubeRestrict", 1)
		k.Close()
	}
	if k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Microsoft\Edge`, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("DnsOverHttpsMode", "off")
		setDword(k, "BuiltInDnsClientEnabled", 0)
		setDword(k, "InPrivateModeAvailability", 1)
		setDword(k, "ForceGoogleSafeSearch", 1)
		setDword(k, "ForceYouTubeRestrict", 1)
		k.Close()
	}
	if k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\BraveSoftware\Brave`, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("DnsOverHttpsMode", "off")
		setDword(k, "BuiltInDnsClientEnabled", 0)
		setDword(k, "IncognitoModeAvailability", 1)
		setDword(k, "TorDisabled", 1) // Brave's built-in Tor windows skip DNS filtering
		setDword(k, "ForceGoogleSafeSearch", 1)
		setDword(k, "ForceYouTubeRestrict", 1)
		k.Close()
	}
	if k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Mozilla\Firefox\DNSOverHTTPS`, registry.SET_VALUE); err == nil {
		setDword(k, "Enabled", 0)
		setDword(k, "Locked", 1)
		k.Close()
	}
	return nil
}

func removePolicies() error {
	targets := []struct {
		path string
		vals []string
	}{
		{`SOFTWARE\Policies\Google\Chrome`, []string{"DnsOverHttpsMode", "BuiltInDnsClientEnabled", "IncognitoModeAvailability", "ForceGoogleSafeSearch", "ForceYouTubeRestrict"}},
		{`SOFTWARE\Policies\Microsoft\Edge`, []string{"DnsOverHttpsMode", "BuiltInDnsClientEnabled", "InPrivateModeAvailability", "ForceGoogleSafeSearch", "ForceYouTubeRestrict"}},
		{`SOFTWARE\Policies\BraveSoftware\Brave`, []string{"DnsOverHttpsMode", "BuiltInDnsClientEnabled", "IncognitoModeAvailability", "TorDisabled", "ForceGoogleSafeSearch", "ForceYouTubeRestrict"}},
	}
	for _, t := range targets {
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, t.path, registry.SET_VALUE); err == nil {
			for _, v := range t.vals {
				_ = k.DeleteValue(v)
			}
			k.Close()
		}
	}
	_ = registry.DeleteKey(registry.LOCAL_MACHINE, `SOFTWARE\Policies\Mozilla\Firefox\DNSOverHTTPS`)
	return nil
}

func serviceDelete() error { return runCmd("sc", "delete", appName) }

// serviceSetBinary points an existing service at path (used when upgrading an
// install that ran from the download folder).
func serviceSetBinary(path string) error {
	return runCmd("sc", "config", appName, "binPath=", `"`+path+`" run`)
}

// protectDataDir ensures SYSTEM and Administrators always have full control of
// the data folder, and creates it if missing. It is ADDITIVE (no /inheritance:r,
// no deny ACEs), so it can never lock the service out of its own files — an
// earlier, more aggressive version did exactly that and stopped the service from
// reading config.json. Note we deliberately do NOT make the config read-only to
// standard users: a direct edit of config.json only takes effect on a service
// restart, and restarting the service already requires administrator rights, so
// the lockdown bought no real security while risking a bricked service.
func protectDataDir() error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
	// Additive grants using well-known SIDs (works on non-English Windows).
	// If a prior bad ACL denied SYSTEM, running this AS SYSTEM repairs it.
	_ = runCmd("icacls", dataDir(), "/grant", "*S-1-5-18:(OI)(CI)F", "/T", "/C", "/Q")     // SYSTEM
	_ = runCmd("icacls", dataDir(), "/grant", "*S-1-5-32-544:(OI)(CI)F", "/T", "/C", "/Q") // Administrators
	return nil
}

// removeInstallDirSoon deletes the install folder a few seconds after this
// process exits (a running exe can't delete itself). Done right away rather
// than at reboot, so an immediate reinstall isn't wiped out by a pending delete.
func removeInstallDirSoon(dir string) error {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
		CmdLine:    `cmd.exe /c ping -n 5 127.0.0.1 >nul & rmdir /s /q "` + dir + `"`,
	}
	return cmd.Start()
}

// serviceEnable sets the service back to automatic start (e.g. after it was
// disabled during a manual recovery), so "install" can bring it back.
func serviceEnable() error { return runCmd("sc", "config", appName, "start=", "auto") }

// isAdmin uses the classic trick: only elevated processes can open this device.
func isAdmin() bool {
	f, err := os.Open(`\\.\PHYSICALDRIVE0`)
	if err == nil {
		f.Close()
		return true
	}
	return false
}
