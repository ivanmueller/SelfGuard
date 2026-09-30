# SelfGuard

> **Contributing or making changes (incl. with an AI assistant)?** Read
> [`AI-CONTEXT.md`](AI-CONTEXT.md) first (architecture, file map, safety
> invariants), then [`CONTRIBUTING.md`](CONTRIBUTING.md) (build, test, release).


A local DNS content filter for Windows. A service (running as `NT AUTHORITY\SYSTEM`)
points the machine's DNS at `127.0.0.1`, answers `NXDOMAIN` for blocked domains,
forces SafeSearch, and blocks encrypted-DNS (DoH) endpoints. A WebView2 GUI lets
you set per-site rules and a delay lock that makes "looser" changes wait.

## Repository layout
- `main.go`, `dns.go`, `config.go`, `lists.go`, `api.go`, `updater.go` — the service.
- `winsys_windows.go` / `winsys_other.go` — OS integration (Windows real / stubs elsewhere).
- `gui/` — the WebView2 desktop app (`main.go` host + bridge, `ui.go` the HTML/JS).
- `*_test.go` — unit and behaviour tests.
- `.github/workflows/release.yml` — builds both exes and publishes a Release on a version tag.

## Build locally
    go build -o selfguard-svc.exe .
    go build -H windowsgui -o SelfGuard.exe ./gui
(Windows/amd64. Cross-compile from Linux with `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`.)

## Auto-update
The service checks the latest GitHub Release, downloads the newer build, and
**verifies its SHA-256 against the release manifest before keeping it**. It stages
the verified binaries beside the running ones and flags the version. The GUI shows
"Update <ver> downloaded — Install & restart"; installing swaps the binaries and
restarts the service.

Design guarantee: **an update only ever replaces the program.** It never rewrites
your settings and never loosens your rules. After an update the service re-reads
your existing config with the delay lock intact and processes any pending
requests as before — so the update channel cannot be used to bypass the delay.

### Cutting a release
1. Set a version and push a tag:
       git tag v1.6.1
       git push origin v1.6.1
2. The workflow builds `selfguard-svc.exe` + `SelfGuard.exe`, computes their
   hashes, writes `manifest.json`, and publishes them as the latest Release.
3. Installed apps on version < 1.6.1 detect it within ~6 hours (or on next start),
   download, verify, and offer to install.

The update source is hardcoded to `github.com/ivanmueller/SelfGuard`; change the
constants in `updater.go` (`updateBase`) and `gui/main.go` if the repo moves.

## Local locks (unchanged by hosting on GitHub)
GitHub is only a download source. The `C:\SelfGuard` folder ACLs, the service
SDDL, the standard-account setup, and the delay lock are all enforced on the PC.
Updates are written by the SYSTEM service, which stays above the folder lock, so
updating never requires loosening it.
