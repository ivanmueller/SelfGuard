//go:build windows

package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

// ---- device lockdown (moved out of the old install-hardening.bat) ----

// hardenedSDDL restricts the service so an interactive administrator can't stop
// or delete it, while SYSTEM keeps full control (so the service can still update
// and restart itself, and the documented recovery command can restore this).
// This is the exact descriptor used by the previous hardening script and paired
// with the restore command in RECOVERY-AND-MAINTENANCE.txt.
const hardenedSDDL = "D:(D;;DCLCSWRPWPDTLOCRRC;;;BA)(A;;CCLCSWRPWPDTLOCRRC;;;SY)(A;;CCLCSWLOCRRC;;;BA)"

func lockdownMarker() string { return filepath.Join(dataDir(), "lockdown.on") }

func lockdownActive() bool {
	_, err := os.Stat(lockdownMarker())
	return err == nil
}

// applyLockdown runs as SYSTEM (the service account). It restricts the service
// against interactive-admin stop/delete, sets auto-restart on unexpected
// termination, and locks the install folder to read-only for admins and users
// (SYSTEM keeps full control so updates still work). It does NOT upload any key
// anywhere and does NOT delete any recovery notes — removal stays possible with
// the recovery command you keep offline.
func applyLockdown() error {
	// 1) Service protection SDDL, then VERIFY it actually took.
	if err := runCmd("sc", "sdset", appName, hardenedSDDL); err != nil {
		return fmt.Errorf("service protection (sdset) failed: %w", err)
	}
	if out, _ := outputCmd("sc", "sdshow", appName); !strings.Contains(out, "(D;") {
		return fmt.Errorf("service protection did not apply")
	}
	// 2) Auto-restart on unexpected termination (best-effort).
	_ = runCmd("sc", "failure", appName, "reset=", "86400",
		"actions=", "restart/2000/restart/5000/restart/10000")

	// 3) Lock the install folder using the .NET ACL method (Get-Acl/Set-Acl with
	// SetAccessRuleProtection) rather than icacls. icacls /inheritance:r silently
	// no-ops when run from the service's session (it "succeeds" but leaves the
	// inherited entries in place); the .NET path reliably strips inheritance and
	// sets an explicit ACL — this is the exact method that worked before.
	dir := installDir()
	if err := lockFolderDotNet(dir); err != nil {
		return fmt.Errorf("folder lock failed: %w", err)
	}
	// VERIFY the folder is actually locked before recording success.
	if !folderLocked(dir) {
		return fmt.Errorf("folder lock did not take effect")
	}

	// Only now is it truly locked — record it.
	if err := os.WriteFile(lockdownMarker(), []byte("on"), 0o644); err != nil {
		return err
	}
	return nil
}

// lockFolderDotNet applies the read-only lockdown via the .NET ACL API, the same
// approach that worked from the previous script: disable inheritance (converting
// inherited rules to none), set SYSTEM as owner with Full control, and grant
// Administrators and Users read & execute only. Applied recursively.
func lockFolderDotNet(dir string) error {
	ps := fmt.Sprintf(`$ErrorActionPreference='Stop'
$dir='%s'
$acl = Get-Acl $dir
$acl.SetAccessRuleProtection($true,$false)          # disable inheritance, drop inherited rules
foreach($r in @($acl.Access)){ [void]$acl.RemoveAccessRule($r) }   # clear any explicit rules too
$sys = New-Object System.Security.Principal.SecurityIdentifier('S-1-5-18')
$adm = New-Object System.Security.Principal.SecurityIdentifier('S-1-5-32-544')
$usr = New-Object System.Security.Principal.SecurityIdentifier('S-1-5-32-545')
$acl.SetOwner($sys)
$acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule($sys,'FullControl','ContainerInherit,ObjectInherit','None','Allow')))
$acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule($adm,'ReadAndExecute','ContainerInherit,ObjectInherit','None','Allow')))
$acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule($usr,'ReadAndExecute','ContainerInherit,ObjectInherit','None','Allow')))
Set-Acl -Path $dir -AclObject $acl
Get-ChildItem $dir -Recurse -Force | ForEach-Object { try { Set-Acl -Path $_.FullName -AclObject $acl } catch {} }`, dir)
	return runCmd("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)
}

// folderLocked confirms the install folder no longer inherits permissions and no
// longer grants write to ordinary accounts — i.e. the lock genuinely applied.
func folderLocked(dir string) bool {
	out, err := outputCmd("icacls", dir)
	if err != nil {
		return false
	}
	if strings.Contains(out, "Authenticated Users") {
		return false // still writable by everyone
	}
	if strings.Contains(out, "(I)") {
		return false // inheritance not stripped
	}
	return true
}

// swapAndRestart installs a staged, hash-verified update from within the service
// (as SYSTEM). A running exe can't replace itself, so it spawns a DETACHED
// SYSTEM helper that waits for the service to stop, moves the new binaries into
// place (keeping .old backups), and starts the service again. Because the helper
// runs as SYSTEM, this works even when the service is locked against admin stop.
// Each staged file was already checked against the release manifest's SHA-256, so
// only a genuine build is ever swapped in; the .old copies remain for recovery.
func swapAndRestart(svcNew, svcDst, guiNew, guiDst, marker string) error {
	// The swap script: stop the service, copy the new binaries over the old
	// (keeping .old backups so a failure can be rolled back), start it again,
	// restore the old binary if it doesn't come back, then remove its own task.
	swap := fmt.Sprintf(`Start-Sleep -Seconds 2
sc.exe stop %[1]s | Out-Null
$i=0; while((Get-Service %[1]s).Status -ne 'Stopped' -and $i -lt 30){ Start-Sleep -Seconds 1; $i++ }
Remove-Item '%[3]s.old','%[5]s.old' -Force -ErrorAction SilentlyContinue
try{
  if(Test-Path '%[2]s'){
    Copy-Item '%[3]s' '%[3]s.old' -Force -ErrorAction SilentlyContinue
    Copy-Item '%[2]s' '%[3]s' -Force
    Remove-Item '%[2]s' -Force -ErrorAction SilentlyContinue
  }
  if(Test-Path '%[4]s'){
    Copy-Item '%[5]s' '%[5]s.old' -Force -ErrorAction SilentlyContinue
    Copy-Item '%[4]s' '%[5]s' -Force
    Remove-Item '%[4]s' -Force -ErrorAction SilentlyContinue
  }
}catch{}
Remove-Item '%[6]s' -Force -ErrorAction SilentlyContinue
sc.exe start %[1]s | Out-Null
Start-Sleep -Seconds 2
if((Get-Service %[1]s).Status -ne 'Running'){
  if(Test-Path '%[3]s.old'){ Copy-Item '%[3]s.old' '%[3]s' -Force }
  sc.exe start %[1]s | Out-Null
}
Unregister-ScheduledTask -TaskName '%[1]sUpdate' -Confirm:$false -ErrorAction SilentlyContinue`,
		appName, svcNew, svcDst, guiNew, guiDst, marker)

	dir := dataDir()
	_ = os.MkdirAll(dir, 0o755)
	ps1 := filepath.Join(dir, "apply-update.ps1")
	if err := os.WriteFile(ps1, []byte(swap), 0o644); err != nil {
		return err
	}

	// Register a one-shot SYSTEM scheduled task to run the swap and start it now.
	// This is the same Register-ScheduledTask-as-SYSTEM mechanism the recovery
	// doc uses. It runs in its own session (a detached child of a service is
	// blocked in session 0, which is why the previous approach silently failed),
	// survives the service stopping, and runs as SYSTEM so it can stop even a
	// hardened service and write the locked folder.
	boot := fmt.Sprintf(`$a=New-ScheduledTaskAction -Execute 'powershell.exe' -Argument '-NoProfile -ExecutionPolicy Bypass -File "%s"';`+
		`$p=New-ScheduledTaskPrincipal -UserId 'NT AUTHORITY\SYSTEM' -LogonType ServiceAccount -RunLevel Highest;`+
		`Register-ScheduledTask -TaskName '%sUpdate' -Action $a -Principal $p -Force | Out-Null;`+
		`Start-ScheduledTask -TaskName '%sUpdate'`, ps1, appName, appName)
	if err := runCmd("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", boot); err != nil {
		return fmt.Errorf("could not schedule the update task: %w", err)
	}
	return nil
}

// sweepUpdateLeftovers clears incomplete downloads and stale swap files on start,
// so a previous failed update self-heals rather than colliding next time.
func sweepUpdateLeftovers() {
	dir := installDir()
	for _, pat := range []string{"*.partial", "*.new.exe"} {
		if matches, err := filepath.Glob(filepath.Join(dir, pat)); err == nil {
			for _, m := range matches {
				_ = os.Remove(m)
			}
		}
	}
}
