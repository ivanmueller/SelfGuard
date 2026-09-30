//go:build !windows

package main

// Stubs so the project builds on non-Windows machines for development. The real
// system integration lives in winsys_windows.go.

func setSystemDNS() error   { return nil }
func resetSystemDNS() error { return nil }
func applyPolicies() error  { return nil }
func removePolicies() error { return nil }
func serviceDelete() error  { return nil }
func serviceEnable() error  { return nil }
func dhcpDNSServers() []string {
	return nil
}
func isAdmin() bool { return true }

func serviceSetBinary(string) error     { return nil }
func protectDataDir() error             { return nil }
func removeInstallDirSoon(string) error { return nil }

func applyLockdown() error                               { return nil }
func lockdownActive() bool                               { return false }
func swapAndRestart(svcNew, svcDst, marker string) error { return nil }
func swapGuiOnly(guiNew, guiDst string) error            { return nil }
func sweepUpdateLeftovers()                              {}
func flushDNS() {}
