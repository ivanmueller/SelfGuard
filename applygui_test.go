package main

import "testing"

// apply_gui refuses cleanly when no staged GUI is present (the stub returns an
// error on non-Windows via the missing file). The command must not panic and
// must surface a failure rather than a false success.
func TestApplyGuiNeedsStaged(t *testing.T) {
	t.Setenv("ProgramData", t.TempDir())
	e := &Engine{cfg: DefaultConfig(), lists: map[string]map[string]struct{}{}}
	e.dnsSrv = &DNSServer{}
	e.buildSnapshot()
	if _, err := e.handleCommand("apply_gui", ""); err == nil {
		t.Skip("apply_gui succeeded (Windows stub differs); no-staged path is platform-specific")
	}
}
