# Contributing to SelfGuard

Read `AI-CONTEXT.md` first for the architecture and the safety invariants. This
file covers building, testing, and shipping changes.

## Prerequisites

- Go (1.22+). Node.js is handy for validating the GUI's embedded JavaScript.
- Building the real GUI requires Windows (it uses WebView2). The service and all
  tests build/run on any OS thanks to the `winsys_other.go` stubs.

## Build

    # service + GUI, Windows/amd64 (cross-compiles from Linux/macOS too)
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o selfguard-svc.exe .
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -H windowsgui -o SelfGuard.exe ./gui

`-H windowsgui` on the GUI hides the console window. Release builds also pass
`-ldflags "-s -w -X main.version=X.Y.Z"` to strip symbols and stamp the version.

## Test and vet

    go test ./...
    go vet ./...

### Validate the GUI JavaScript

`gui/ui.go` holds the whole UI as a Go raw-string literal, so Go will NOT catch
JS errors. After editing it, extract the `<script>` block and syntax-check it:

    # rough approach: pull the JS out of uiHTML and run:
    node --check that-script.js

Also confirm every `window.sgX(...)` used in the JS has a matching
`must("sgX", ...)` binding in `gui/main.go`.

## Making a change

1. Edit the relevant Go/JS. Keep the safety invariants in `AI-CONTEXT.md` intact.
2. `go test ./...` and `go vet ./...` must pass.
3. If you touched `gui/ui.go`, validate the JS as above and confirm the edit
   actually landed (grep for it) — string replaces occasionally no-op silently.
4. If you touched anything Windows-only (service control, lockdown, updater),
   remember it can only be truly verified on a real Windows machine — and, for
   lockdown/update changes, with the recovery tools in reach.

## Releasing (how updates reach installed machines)

Releases are automated by `.github/workflows/release.yml` on a version tag:

    git add -A
    git commit -m "1.8.0: <what changed>"
    git push origin main
    git tag v1.8.0
    git push origin v1.8.0

Use semantic-ish `vMAJOR.MINOR.PATCH` tags; the tag name becomes the version
stamped into the binaries. The workflow builds both exes, writes `manifest.json`
with their SHA-256 hashes, and publishes a GitHub Release. Installed apps detect
the newer release, download + verify it, and self-update (service via a SYSTEM
scheduled task, GUI on launch). You do not hand-deliver exes for existing
installs — pushing the tag is the release.

### If a release doesn't publish

- Check the Actions run at `github.com/<owner>/<repo>/actions`.
- Ensure Settings → Actions → General → Workflow permissions is **Read and write**
  (needed to create the Release).
- If a stale release exists for the tag, delete it, then re-push the tag:
  `git push origin :refs/tags/vX.Y.Z` then `git push origin vX.Y.Z`.

### Verify a release

    # PowerShell on the machine:
    Invoke-RestMethod https://github.com/<owner>/<repo>/releases/latest/download/manifest.json

The `version` field should be your new tag.

## First install on a new machine

There's nothing to self-update from yet, so install once by hand: download
`selfguard-svc.exe` and `SelfGuard.exe` from the latest Release into
`C:\SelfGuard\`, then run `& 'C:\SelfGuard\selfguard-svc.exe' install`. After
that, the machine auto-updates like any other.

## Recovery (keep this available)

Lockdown is intentionally hard to undo. Keep `RECOVERY-AND-MAINTENANCE.txt` and
`unlock-maintenance.bat` on a USB / with a trusted person. If a locked-down update
ever fails mid-swap, that is the way back in. Never remove the recovery path from
the code.

## Security note

Whoever can publish a release can run code on every installed machine. Protect the
repo/account with 2FA. Updates cannot loosen content rules (they only ship a newer
program and re-read the existing locked config), but it is still a code channel —
treat publish rights like a key.
