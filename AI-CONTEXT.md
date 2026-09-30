# AI-CONTEXT.md — read this first

This file orients an AI assistant (or a new human contributor) to the SelfGuard
codebase so changes can be made safely. Read it fully before editing.

## What SelfGuard is

A self-hosted **Windows DNS content filter**, written in Go. Its purpose is
self-control: it blocks adult content, social media, and encrypted-DNS (DoH)
bypass endpoints at the DNS layer, forces SafeSearch, and can lock itself down so
it is hard to remove in a moment of weakness. It is meant to restrict the person
who installed it, on purpose, while always keeping a documented recovery path.

Two executables are built from this one repo:

- **`selfguard-svc.exe`** — a Windows service running as `NT AUTHORITY\SYSTEM`. It
  is the engine: runs a DNS server on `127.0.0.1:53`, points the machine's DNS at
  itself, answers `NXDOMAIN` for blocked domains, forces SafeSearch, blocks DoH,
  re-applies settings on a loop, and self-updates. Fails **open** (if it can't
  resolve for ~2 minutes it resets DNS so the machine keeps working).
- **`SelfGuard.exe`** — a WebView2 desktop GUI. It renders an HTML/CSS/JS UI and
  talks to the service over a local HTTP API. It has no filtering logic itself.

## File map

Service (package `main`, repo root):
- `main.go` — service entry, the `Engine`, the command handler (`handleCommand`),
  `status()`, and the run loop (`refreshLists`, `dnsLoop`, `reconcileLoop`,
  `updateLoop`).
- `dns.go` — the DNS server and the core `blocked(host, now)` decision.
- `config.go` — `Config`, the per-site rule **levels**, schedules, and the
  strictness helpers (`blockedSet`, `stricterOrEqual`).
- `lists.go` — downloading/refreshing the blocklists.
- `api.go` — the local HTTP API and the `statusResp` shape the GUI reads.
- `updater.go` — self-update: manifest fetch, download + SHA-256 verify + stage,
  version compare (`isNewer`), `checkForUpdate`, `doCheckUpdate`,
  `updateReadyVersion`, `fileSHA`.
- `nettime.go` — trusted network time (used for schedules and fail-open).
- `winsys_windows.go` — all Windows integration: `runCmd`/`outputCmd`, service
  install/control, setting adapter DNS, DoH policy, and the lockdown +
  self-update machinery (`applyLockdown`, `folderLockScript`, `lockFolderDotNet`,
  `folderLocked`, `swapAndRestart`, `swapGuiOnly`, `flushDNS`, `hardenedSDDL`,
  `restoreSDDL`).
- `winsys_other.go` — no-op stubs of the Windows functions so the package builds
  on non-Windows for `go test`/`go vet`.

GUI (package `main`, `gui/`):
- `gui/main.go` — the WebView2 host, the Go↔JS **bridge** bindings, GUI
  self-update on launch (`applyStagedGuiOnLaunch`, `swapStagedGui`,
  `relaunchSelf`), window-position restore across an update, and `guiLog`.
- `gui/ui.go` — **the entire UI is one Go raw-string literal** named `uiHTML`
  containing all the HTML, CSS, and JavaScript. See the gotcha below.

Other:
- `.github/workflows/release.yml` — builds both exes, computes hashes, writes
  `manifest.json`, and publishes a GitHub Release on a `v*` tag.
- `*_test.go` — unit/behaviour tests (run on any OS via the stubs).

## On-disk locations (Windows, at runtime)

- Program: `C:\SelfGuard\selfguard-svc.exe` and `C:\SelfGuard\SelfGuard.exe`.
- Data: `C:\ProgramData\SelfGuard\` — `config.json`, `api.port` (the API port,
  chosen randomly per start), `selfguard.log`, `gui.log`, `update.ready`,
  `lockdown.on`, `window.json`, and staged updates (`*.new.exe` in the program
  folder). None of these ship in the repo.

## How the GUI talks to the service

The service writes its randomly-chosen API port to `C:\ProgramData\SelfGuard\api.port`.
The GUI reads that and calls `http://127.0.0.1:<port>/status` and `/command`.
These are exposed to the UI's JavaScript as **bridge functions** bound in
`gui/main.go`: `sgStatus`, `sgCommand`, `sgServiceRunning`, `sgInstall`,
`sgGuiVersion`, `sgGuiStaged`, `sgFinishUpdate`, `sgRestartBrowser`. If you add a
JS call `window.sgFoo(...)`, you must add a matching `must("sgFoo", ...)` binding,
or it fails at runtime.

## Per-site rule levels

Each social site has one level (in `config.go`):
`allowed`, `noimages`, `novideo`, `text`, `blocked`. The GUI's three toggles
(Block site / Hide images / Hide videos) map to these. Sites split media by host
lists (`ImageHosts`/`VideoHosts`); TikTok is intentionally block-or-allow only.

## Commands (`sgCommand(kind, payload)`)

Handled in `main.go` `handleCommand`. User-facing kinds:
`set_site_level`, `set_site_schedule`, `set_schedule` (category), `enable_category`,
`disable_category`, `block_domain`, `unblock_custom`, `allow_domain`,
`request_site`, `set_delay`, `cancel_pending`, `uninstall`, `lock_down`,
`apply_gui`, `apply_update`, `check_update`. (CLI-only kinds like `install`,
`run`, `status` are for the exe on the command line.)

The **delay lock**: tightening changes (more blocking) apply immediately;
loosening changes are queued to wait out `DelayHours`. In setup mode
(`delay_hours == 0`) everything is immediate. `stricterOrEqual` in `config.go`
decides which is which.

## The self-update pipeline (important)

1. Push a git tag `vX.Y.Z`. GitHub Actions builds both exes, computes their
   SHA-256, writes `manifest.json`, and publishes them as the latest Release.
   The version is stamped into the binaries via `-ldflags "-X main.version=X.Y.Z"`.
2. The running service checks the manifest (on start + periodically), and
   downloads/stages a binary if its version is older **or** its on-disk hash
   doesn't match the manifest (this is how a lagging GUI self-heals).
3. Installing swaps the service binary via a one-shot **SYSTEM scheduled task**
   (`swapAndRestart`), and the GUI applies its own staged copy on launch or via
   `sgFinishUpdate` (`swapStagedGui`). Downloads are verified against the
   manifest hash before anything is swapped.

To ship a change: edit source → commit → push a new `vX.Y.Z` tag. Installed apps
update themselves. You never hand-deliver exes for an existing install.

## SAFETY INVARIANTS — do not break these

This is a self-control tool with a recovery path. Preserve these properties:

1. **An update only ships a newer *program*, never a looser *config*.** After an
   update the service re-reads the user's existing (delay-locked) config
   unchanged. Do not add update logic that rewrites settings or unblocks content.
   This is what stops the update channel from becoming a bypass of the lock.
2. **Fail open.** The service must never leave the machine unable to resolve DNS.
   Keep the fail-open safety in the run loop.
3. **Keep the recovery path.** Never add anything that deletes the user's recovery
   notes or uploads the unlock key off the machine. Lockdown is meant to be *hard*
   to undo, not impossible.
4. **Lockdown updates re-seal.** `swapAndRestart` temporarily relaxes only
   service-control (never the folder — SYSTEM writes through it), then re-applies
   the SDDL and folder lock. Any change here must re-seal on every path.

## Gotchas that will bite you

- **`gui/ui.go` is JavaScript inside a Go string.** Go does not type-check it. A
  broken selector or dropped brace compiles fine and fails at runtime. After
  editing, extract the `<script>` block and run `node --check` on it. Keep
  `window.sgX` calls matched to `must("sgX", ...)` bindings.
- **Windows-only code can't be tested off-Windows.** Service control, folder
  lockdown, SDDL, and the update swap only run on real Windows. An AI can write
  this code but cannot confirm it works — treat anything touching lockdown or
  updates as "must be tested on the machine with the recovery key in reach,"
  not "done because it compiles."
- **Editing tools sometimes silently skip a string replace.** After any edit to
  `gui/ui.go` or `winsys_windows.go`, grep to confirm the change actually landed
  before building.

## Build and test

From the repo root:

    # cross-compile from any OS (or native on Windows)
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o selfguard-svc.exe .
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -H windowsgui -o SelfGuard.exe ./gui

    go test ./...        # runs on any OS via the winsys stubs
    go vet ./...

See `CONTRIBUTING.md` for the release/versioning workflow.
