# GoSSMS: SqlPackage Manager and DACPAC/BACPAC integration

**Status:** implementation research and design proposal  
**Scope:** Windows, Linux, macOS; use Microsoft's SqlPackage, not a Go DacFx reimplementation.  
**Goal:** GoSSMS discovers an existing SqlPackage or installs its own copy on demand, reports its version and location, supports upgrade, and enables database menu actions only when a usable executable is available.

**Codebase review:** Section 13 adds implementation-specific findings against the current
goSSMS source and verified Microsoft documentation. Review corrections are integrated
throughout the proposal; code snippets remain illustrative, not production implementations.

**Responsibility boundary:** goSSMS supplies the selected connection, package path and
action. SqlPackage validates package/SQL-target compatibility, SQL permissions and
destination constraints. goSSMS does not maintain a SQL target matrix or inspect DACPAC
contents to decide whether an operation is supported.

## 1. Product requirements

1. **Discover or install:** GoSSMS detects an existing system SqlPackage and reuses its exact path and version. Alternatively, the user selects **Install SqlPackage** and GoSSMS installs a private copy without requiring terminal interaction.
2. **Show status:** Display detected/managed origin, executable path, installed version, OS/architecture, health, and update availability (when online).
3. **Gate operations:** Offer **Export DACPAC**, **Deploy DACPAC**, **Export BACPAC**, **Import BACPAC** in database menus. Only execute when a healthy SqlPackage is resolved. Disabled menu items should explain how to install/configure it.
4. **Upgrade:** Provide **Check for Updates** and **Upgrade SqlPackage**. Upgrade GoSSMS-managed copies without modifying user/system installations by default.

### Non-goals

- Implementing DacFx, DACPAC/BACPAC parsing, or schema diff in Go.
- Shipping SqlPackage inside the main GoSSMS executable.
- Requiring administrator rights, global PATH modifications, a background service, or a visible console window.
- Guaranteeing all four operations on every SQL target; support is action- and target-dependent.
- Classifying SQL targets or duplicating DacFx compatibility and package validation rules.

## 2. Recommended architecture

A small Go package (for example `internal/sqlpackage`) owns discovery, installation, version probing, upgrade and execution. Menu handlers call this package; they do not construct shell commands themselves.

```text
GoSSMS UI
  Tools > SqlPackage Manager
    Status / path / version / source
    Detect / Select executable / Install / Check updates / Upgrade
  Database > Tasks
    Export DACPAC / Deploy DACPAC / Export BACPAC / Import BACPAC
              |
       SqlPackageManager
       - Discover() / Resolve()
       - ProbeVersion()
       - Install() / Upgrade()
       - RunAction()
              |
     resolved absolute executable path
              |
        os/exec child process
```

**Important:** A background child process fits the terminal TUI workflow. Capture its
output rather than letting it write over the screen. Use `exec.CommandContext` with an
argument array, never `sh -c`, `cmd /c`, or interpolated shell text.

## 3. Resolution and discovery

Resolve asynchronously on startup and explicit Refresh/executable selection. Opening
or evaluating a menu reads cached status only. Resolve candidates in this order:

1. **Explicit user-selected executable**, if configured. If unavailable, stop resolution
   and offer alternatives; do not silently fall back to a different installation.
2. **GoSSMS-managed active installation**, if healthy.
3. **Existing system installation** via `exec.LookPath("sqlpackage")` (also Windows `.exe` handling), followed by a health probe.
4. **Additional known locations**, only as optional platform-specific heuristics: .NET global tool directory (for example `~/.dotnet/tools` or `%USERPROFILE%\\.dotnet\\tools`), configured installation folders, and known Microsoft tool locations. Do not assume that SSMS or Visual Studio contains a runnable, supported SqlPackage CLI at a fixed path.
5. If no healthy candidate exists, return **Not installed** and enable the Install action.

Always normalize to an **absolute path**; persist origin (`user-selected`, `managed`, `system`) and the selected path. Never infer availability from a file's mere existence. Probe it by invoking the CLI version action with a short timeout and parsing the exit status and reported version. A candidate that cannot start is **Unavailable**, not Installed.

If multiple versions are discovered, show a selector and make the selected candidate explicit. Prefer user choice over silent switching. Revalidate the selected path before each operation because a system installation may change or disappear.

### Probe and execution contract

Probe only candidates from the selected path, the managed directory, PATH or documented
optional locations. Do not search the current directory implicitly or override Go's
`exec.ErrDot` protection. Health probing executes a program with the user's privileges;
it establishes usability, not trust or package compatibility.

Use the absolute executable path, `/Version`, no stdin, a short timeout and bounded
stdout/stderr. Set the working directory explicitly to the resolved tool directory.
Define the inherited runtime/authentication environment; do not inject database passwords,
tokens or connection strings into it. Do not repeatedly probe while a menu is open.

At job start, resolve and probe once, capture the executable path/version and connection
inputs, then launch that exact path. Managed versions remain in immutable directories.
For system/user-selected tools, an external replacement can race with probing; detection
is best effort, not a guarantee that verified bytes are executed. Report launch/version
errors without silently selecting another tool. No health probe requires a SQL connection.

### Example status model

```go
type Origin string
const (
    OriginSystem Origin = "system"
    OriginManaged Origin = "managed"
    OriginSelected Origin = "user-selected"
)

type Installation struct {
    Path    string
    Version string
    Origin  Origin
    Healthy bool
    Error   string
}

type Manager interface {
    Discover(ctx context.Context) ([]Installation, error)
    Resolve(ctx context.Context) (Installation, error)
    Install(ctx context.Context) (Installation, error)
    CheckUpdate(ctx context.Context) (string, error)
    Upgrade(ctx context.Context) (Installation, error)
    Run(ctx context.Context, action Action, opts ActionOptions) error
}
```

Keep a single active installation in settings, but expose other candidates in the manager dialog.

## 4. Installation strategy

**Recommended default: private, portable SqlPackage installation downloaded on demand.** Microsoft's official installation documentation provides standalone self-contained ZIP downloads for Windows, Linux and macOS as an alternative to the `dotnet tool` method. This avoids making a .NET SDK installation a prerequisite for GoSSMS's one-click install.

### Installation algorithm

1. Detect `runtime.GOOS` and `runtime.GOARCH`.
2. Load the trusted release-bundled **version/asset manifest** with approved host assets,
   versioned Microsoft URLs and verified SHA-256 digests. A signed hosted feed is future
   scope; do not discover approved versions from a moving latest URL or scraped page.
3. Present version, approximate download size, source, and installation destination. Request user confirmation.
4. Download over HTTPS into a temporary staging directory, with progress, cancellation and timeouts; reject unexpected redirects/hosts as appropriate.
5. Verify archive integrity against a trusted digest or signature from an independently trusted manifest. A checksum fetched alongside a potentially compromised archive without authentication is not sufficient.
6. Safely extract ZIP, rejecting path traversal, absolute paths and unsafe symlinks. Preserve executable permissions; on Unix ensure the launcher can execute.
7. Run `sqlpackage /Version` from staging and check its exit code and reported version.
   Also verify runtime dependencies actually load on the host machine.
8. Activate through validated whole-pointer replacement, retaining prior metadata/version
   for rollback. Verify filesystem replacement/recovery behavior; do not assume universal
   crash atomicity from `os.Rename`, especially on Windows.
9. Record the active executable path/version in managed metadata. Persist selection
   preferences separately, and refresh cached UI status and menu enabled state.
10. On failure, delete staging files and leave the previously working version active.

Use the OS-specific per-user application-data directory (`os.UserConfigDir` or `os.UserCacheDir` where appropriate; avoid assuming `~/.gossms` on every OS). Example layout:

```text
<GoSSMS user data>/tools/sqlpackage/
    versions/
        170.5.96/<executable and dependencies>
        <older-version>/...
    active.json
    downloads/   # temporary, cleaned up
```

**Architecture caveat:** Cross-platform does not imply every OS/CPU combination has a matching downloadable ZIP. Resolve actual supported assets from the authoritative release, then show **Unsupported platform** with a manual executable-selection option if needed. Linux native dependencies and macOS execution/security restrictions require testing.

**Observed assets, not an approved manifest:** On 2026-11-22, HTTP HEAD checks of the
three official ZIP links returned 200 and the following versioned filenames/lengths.
The Learn page lists 170.5.96, but these archive names say 170.4.83.3. HEAD verifies
availability, not contents, launcher version, runtime health or trusted digests.

| Host asset | Versioned Microsoft URL | Compressed bytes |
|---|---|---|
| Windows x64 | https://download.microsoft.com/download/46a13f8c-5548-42fb-b547-7e69ebc3fcca/sqlpackage-win-x64-en-170.4.83.3.zip | 47,660,905 |
| Linux x64 | https://download.microsoft.com/download/18a5e51e-8332-4cbe-bb50-6d3a50c704c5/sqlpackage-linux-x64-en-170.4.83.3.zip | 47,207,632 |
| macOS x64 | https://download.microsoft.com/download/34f373a2-6ab5-496e-87a0-4dc7725d400b/sqlpackage-osx-x64-en-170.4.83.3.zip | 46,231,476 |

Release approval must download the exact asset, establish provenance, compute its digest,
probe its actual version and smoke-test it. Do not label these files 170.5.96 or publish
unverified hash values. ARM64 assets remain unverified; do not infer native ARM64 support
from these x64 URLs or invent download links. This is a host-installation inventory,
not a SQL-target support matrix.

### Alternative: .NET global tool

Microsoft also supports:

```bash
# Illustrative commands, not commands the user must type
 dotnet tool install -g microsoft.sqlpackage
 dotnet tool update -g microsoft.sqlpackage
 dotnet tool uninstall -g microsoft.sqlpackage
```

This is a useful **fallback for an already installed global tool**, but a GoSSMS-driven installation via `dotnet tool` requires an installed compatible .NET SDK and may alter a global tool owned by the user. Do not silently install or upgrade globally. If supporting this method, display the prerequisite and require explicit opt-in. A private ZIP install is simpler for the intended UX.

## 5. Version display and upgrade

### Version probe

Execute the resolved binary with `/Version` under the probe contract and parse output.
Show:

| UI field | Meaning |
|---|---|
| Status | Installed / Not installed / Unavailable / Installing / Upgrade available |
| Source | Managed by GoSSMS / System PATH / User-selected |
| Executable | Full absolute path (copyable) |
| Installed version | From running the executable, not a hard-coded string |
| Latest vetted version | Highest approved version in the trusted goSSMS manifest, not necessarily Microsoft's newest release |
| OS / architecture | Current host |
| Update status | Up to date for this manifest / Vetted update available / Unknown (manifest or probe unavailable); offline alone is not Unknown |

For context, Microsoft's documentation listed **170.5.96** as a release dated September 15, 2026; **do not hard-code it as the forever-latest version**.

### Upgrade policy

- **Managed installation:** download a newer vetted ZIP to a new versioned directory, verify and probe, atomically switch, retain old version for rollback. Do not replace an executable while an export/import is running; allow active jobs to finish using their captured executable path.
- **System or user-selected installation:** report available updates, but do **not** overwrite or uninstall it. Offer **Install managed version** or instructions to update through its owner/package manager. If user explicitly requests a system-wide update, treat that as a separate future feature.
- Check for vetted updates locally when the manager opens or the user requests Refresh.
  The bundled manifest requires no network and works offline. Downloading an approved
  ZIP is a separate, user-confirmed network operation; do not block core SQL editing.
- Prefer stable releases over previews. Any tool-version minimum concerns the runner's
  documented CLI/auth contract, not a goSSMS-maintained SQL-target support matrix.
- Version ordering should use parsed numeric components, not lexical string comparison.

### Manifest ownership and publication

The proposed initial source is a manifest bundled with each goSSMS release, owned by
the goSSMS release maintainers. For each approved SqlPackage version, the release process
verifies official provenance/download terms, records exact OS/architecture assets and
SHA-256 digests, and smoke-tests the unpacked tool before publishing the manifest through
the trusted goSSMS distribution. Moving Microsoft links are not an automatic update feed.

Check for Updates compares the installed version with the highest vetted manifest entry.
Show **Up to date for this manifest**, not a claim that the Microsoft release is current.
Initially, new approvals arrive through a goSSMS update. A separately signed hosted feed
is a future extension requiring its own publication owner, trust root and key lifecycle.
Do not scrape latest links or replace a digest automatically when a download changes.
Only that future feed would need periodic network refresh, short timeouts and stale-feed
status. Keep goSSMS's own application update check separate from SqlPackage's local
manifest comparison. A missing/invalid manifest disables managed install/update, not
use of an otherwise healthy user/system executable.

## 6. Database menu actions

Map UI verbs to the **actual** SqlPackage actions:

| GoSSMS menu item | SqlPackage action | Input | Output / effect |
|---|---|---|---|
| Export DACPAC | `Extract` | Existing database | `.dacpac` file, primarily schema |
| Deploy DACPAC | `Publish` | `.dacpac` + target connection/database | Applies package schema, scripts and any packaged data as handled by SqlPackage |
| Export BACPAC | `Export` | Existing database | `.bacpac` schema + data |
| Import BACPAC | `Import` | `.bacpac` + new/empty target | Creates/imports database schema + data |

**Terminology:** Do not call DACPAC deployment “Import DACPAC” in implementation; the SqlPackage operation is `Publish`. BACPAC is not a SQL Server native backup/restore replacement.

Menu enablement reads cached tool readiness and normal UI state, not SQL engine edition
or package compatibility. The following is pseudocode; it performs no probe or SQL read:

```go
ready := cachedStatus.Healthy && connectionSelected && !busy
setEnabled("Export DACPAC", ready)
setEnabled("Deploy DACPAC", ready)
setEnabled("Export BACPAC", ready)
setEnabled("Import BACPAC", ready)
```

If not ready, disable the operations and expose **Install / Locate SqlPackage** as a one-click route. Even if the menu was enabled earlier, `Run` must resolve/validate again and return a friendly error if the binary has disappeared.

### Command construction examples

These examples use placeholder connection strings; build the real arguments as separate Go strings:

```text
sqlpackage /Action:Extract /SourceConnectionString:<source> /TargetFile:<file.dacpac>
sqlpackage /Action:Publish /SourceFile:<file.dacpac> /TargetConnectionString:<target>
sqlpackage /Action:Export /SourceConnectionString:<source> /TargetFile:<file.bacpac>
sqlpackage /Action:Import /SourceFile:<file.bacpac> /TargetConnectionString:<target>
```

The import target must be **new or empty** according to SqlPackage's Import semantics;
SqlPackage checks that requirement. goSSMS does not run a target-specific preflight or
create/drop objects to satisfy it. Publish can modify schema and data; show the selected
destination and a clear confirmation, with an optional SqlPackage-generated preview.

### Go runner skeleton

```go
func runSqlPackage(ctx context.Context, executable string, args []string, onLine func(string)) error {
    cmd := exec.CommandContext(ctx, executable, args...)
    // Avoid cmd.CombinedOutput for large operations: stream stdout/stderr.
    stdout, err := cmd.StdoutPipe()
    if err != nil { return err }
    stderr, err := cmd.StderrPipe()
    if err != nil { return err }
    if err := cmd.Start(); err != nil { return err }

    var wg sync.WaitGroup
    wg.Add(2)
    go func() { defer wg.Done(); streamLines(stdout, onLine) }()
    go func() { defer wg.Done(); streamLines(stderr, onLine) }()
    wg.Wait()
    return cmd.Wait()
}
```

`streamLines` is an application helper to implement with `bufio.Reader` or a scanner
with an enlarged buffer. Add proper error propagation, redaction, cancellation and UI
dispatch in production. Avoid blocking the UI thread. This skeleton is not the production
runner: its concurrent callbacks and shutdown path require the contract in section 13.3.
Use ordinary piped execution in the terminal TUI; do not add Windows-specific files,
build tags or unconditional `SysProcAttr` fields.

### Credentials and security

- **Do not log or persist secrets in plain text.** SqlPackage command-line connection strings may expose passwords through process arguments and diagnostic logs.
- Prefer integrated/Entra authentication or a verified token flow when available. Test
  the adapter's mapping to SqlPackage; let SqlPackage handle SQL-side authentication
  acceptance without adding engine-specific authentication gates in goSSMS.
- If credentials must be supplied, carefully consider process-list exposure; use supported secure authentication approaches rather than assuming environment variables or stdin are accepted for every option.
- Redact credentials in UI logs and error messages; never include full connection strings in telemetry.
- Restrict file permissions on exported artifacts. BACPAC data is compressed, **not encrypted**.
- Keep downloaded executables and update manifests in trusted locations; prevent unsafe extraction and path injection.

## 7. Compatibility belongs to SqlPackage

SqlPackage supports SQL Server and Azure SQL-family scenarios, but not every package,
action or feature works with every destination. **These are SqlPackage/DacFx rules, not
a support matrix to reproduce in goSSMS.** Treat DACPAC/BACPAC files as opaque inputs.

Submit the selected action, connection/database, file and explicit user options to
SqlPackage. It validates the package, SQL platform, permissions and destination state.
Display sanitized failures and warnings; do not interpret SQL engine editions or
hostname heuristics to hide actions or reject a destination before the tool runs.

goSSMS still validates local inputs, tool usability and connection-setting translation,
asks before writes/overwrites and applies its CLI safety/audit policy when relevant.
It must not automatically weaken TLS, enable incompatible-platform/data-loss overrides,
retry a rejected deployment or modify the destination to make a package fit.

Vendor documentation covers SQL Server, Azure SQL Database, Managed Instance and Fabric
SQL Database scenarios with different constraints. Fabric Warehouse is a distinct product.
Those distinctions belong in SqlPackage/vendor guidance and integration test coverage,
not in application dispatch, menu gates or a new classifier.

## 8. Suggested UX

### Tools > SqlPackage Manager

```text
SqlPackage
Status: Installed
Source: System PATH
Version: 170.x
Location: /absolute/path/to/sqlpackage
Update: Vetted update available / Up to date for this manifest / Unknown

[Refresh] [Select Executable] [Install Managed Copy]
[Check for Updates] [Upgrade Managed Copy]
```

- If the system binary is healthy, GoSSMS uses it without any installation.
- If none exists, **Install** downloads a managed copy in the background with a progress dialog.
- If a system version is old, the user can keep it or install a newer managed copy. No silent mutation of global tools.
- For managed versions, **Upgrade** is enabled. For system versions, replace it with **Install New Managed Version** or guidance.
- Error dialogs show a friendly summary plus expandable sanitized logs.

### Database > Tasks

- Export DACPAC...
- Deploy DACPAC...
- Export BACPAC...
- Import BACPAC...

Use file pickers, target database selection, progress, cancel, and completion/error dialogs. Warn before Publish changes and before Import creates a database. Preserve context of the selected connection and database, but require a destination name for Import.

## 9. Implementation milestones

### Phase 1: Detection and actions (MVP)

- Implement `Discover`, `Resolve`, `ProbeVersion`, persistent selection.
- Build a basic SqlPackage status dialog.
- Implement the typed connection/auth adapter, credential exposure policy and redaction
  before enabling database operations. Unsupported local auth mappings fail explicitly.
- Implement the bounded runner, serialized UI delivery, cancellation and child reaping.
- Add application-owned job shutdown for normal quit, loop failure/panic and signal exit;
  do not rely on screen teardown or configuration flushing to stop external work.
- Add file-based export staging/promotion and direct Deploy/Import execution-guard tests.
- Wire four menu actions with local validation, confirmation and background execution.
  Delegate package/SQL-target validation to SqlPackage and surface its sanitized errors.
- Test with user-provided SqlPackage installations on all three OSes, including rejected
  destinations/packages. No automatic install/upgrade is promised by this milestone.

### Phase 2: Managed install

- Establish a trusted release manifest with exact OS/architecture asset URLs and digests.
- Resolve the observed page/archive version discrepancy by approving actual downloaded
  and probed assets, not by copying the page's release number.
- Assign manifest publication to the goSSMS release process, vet assets and define the
  approval/update workflow before enabling managed downloads.
- Download, verify, safely extract, probe and activate a portable ZIP.
- Implement progress, isolated concurrent staging, short publication and failure rollback.
- Test clean machines without .NET SDK and restricted user permissions.

### Phase 3: Upgrade and hardening

- Version comparison and update check against approved manifest entries.
- Side-by-side managed upgrade, atomic switch, rollback.
- Multiple-installation selection and migration from system to managed.
- Broader auth/runtime coverage and large-file tests build on Phase 1's required safety.
  SQL compatibility errors remain owned by SqlPackage, not a goSSMS capability matrix.

## 10. Test matrix and acceptance criteria

| Test | Expected outcome |
|---|---|
| SqlPackage absent | Actions disabled; Install offered |
| SqlPackage on PATH | Absolute path/version displayed; actions enabled from cached readiness and selected connection, without SQL-target classification |
| Explicit path outside PATH | Selected binary used and persisted |
| Broken or outdated path | Clear Unavailable state; no crash; Install/Select offered |
| Managed install | No admin rights or global PATH changes; version probe passes |
| Network failure / checksum mismatch | Existing installation preserved; useful error |
| Update while action running | Existing action completes against old binary; new jobs use new one after switch |
| System installation found | No automatic global modification |
| Windows, Linux, macOS | Detect, install (where assets supported), version and operation smoke tests |
| Wrong OS/CPU or missing native dependency | Helpful unsupported/unavailable status |
| Import into nonempty database | SqlPackage rejects it; goSSMS reports the sanitized error without target preflight or destructive repair |
| Package incompatible with SQL destination | Request reaches SqlPackage without an edition/hostname gate; its sanitized compatibility error is displayed |
| Probe process | No stdin, explicit working directory, bounded output/timeout and no implicit current-directory discovery |
| Update manifest lags Microsoft release | UI reports the latest vetted version, not Microsoft's latest; no unapproved download occurs |
| Offline local update check | Valid bundled manifest and healthy tool still produce a local comparison; no network request or false offline Unknown |
| Normal quit with a running job | Confirm cancel/quit, stop launches, cancel and reap before completing quit; cleanup timeout keeps normal quit from silently abandoning work |
| Loop failure/panic or signal exit | Application-owned cleanup runs without UI callbacks; watchdog/save/drain budgets are coordinated, with forced-termination limits explicit |
| Export promotion fails | Original destination remains intact; no whole-artifact memory load or delete-then-rename fallback |
| Deploy/Import guard refuses | Direct invocation cannot spawn the helper process, regardless of menu label or Note |
| Publish potentially destructive changes | Explicit warning/preview before running |
| Password in connection settings | No password in logs, UI, telemetry; security exposure of CLI arguments addressed |
| Offline startup | Core GoSSMS unaffected; cached installed version remains usable |

**Definition of done (Phases 1–3):** A user can detect an existing SqlPackage or install
a private copy with one click, see its actual path/version, submit all four database
actions and receive SqlPackage results, and upgrade a managed copy to a vetted release
without manually maintaining it. SQL compatibility is validated by SqlPackage, not
guaranteed by goSSMS. Phase 1 alone delivers neither managed installation nor upgrade.

## 11. Decisions to make before coding

1. **Distribution source:** confirm the proposed release-bundled manifest + official ZIP
   path, with goSSMS release maintainers owning approvals/publication. A hosted signed
   feed and global-tool management remain separate future scope.
2. **Trust and licensing:** verify redistribution/download terms and trusted hashes for selected assets before implementation.
3. **Supported architectures:** establish verified OS/architecture matrix rather than assume all GoSSMS builds have SqlPackage binaries.
4. **Authentication:** decide which GoSSMS connection types can be safely passed to SqlPackage; avoid leaking passwords through process arguments.
5. **Delegation contract:** SQL compatibility and destination validation belong to
   SqlPackage. Define sanitized error/warning delivery without adding goSSMS target gates.
6. **Preview:** decide whether SqlPackage Script/DeployReport is optional or required
   before Publish; no goSSMS-side package inspection or schema diff is introduced.

## 12. Authoritative references

1. Microsoft Learn, [Download and install SqlPackage](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/sqlpackage-download?view=sql-server-ver17): OS support, ZIP and dotnet tool distribution, install/update commands, runtime requirements.
2. Microsoft Learn, [SqlPackage CLI reference](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/cli-reference?view=sql-server-ver17): action names, command-line parameters and version action.
3. Microsoft Learn, [SqlPackage overview](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/sqlpackage?view=sql-server-ver17): DACPAC vs BACPAC semantics, security notes.
4. Microsoft Learn, [SqlPackage release notes](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/release-notes-sqlpackage?view=sql-server-ver17): version changes and supported releases.
5. Microsoft Learn, [SqlPackage for SQL database in Microsoft Fabric](https://learn.microsoft.com/en-us/fabric/database/sql/sqlpackage): Fabric SQL Database support and constraints.
6. NuGet, [Microsoft.SqlPackage](https://www.nuget.org/packages/Microsoft.SqlPackage): official .NET tool distribution and package versions.
7. Microsoft Learn, [SqlPackage Import](https://learn.microsoft.com/en-gb/sql/tools/sqlpackage/sqlpackage-import?view=sql-server-2017): new/empty database requirement.

---

**Bottom line:** Keep GoSSMS as a cross-platform Go application. Treat SqlPackage as an optional, discoverable external engine. Use a verified portable ZIP for user-triggered private installation and upgrade, preserve existing system installations, and centralize all four DACPAC/BACPAC menu actions behind one Go manager.

## 13. Codebase feasibility and required adaptations

### 13.1. Verdict and scope

**Feasible without implementing DacFx or changing the database driver.** The integration
can use Go's standard library for process execution, HTTP, hashing and ZIP extraction.
Existing goSSMS dialogs, background tasks and configuration persistence cover the UI
integration. No gosmo or tuikit capability needs to be removed or replaced.

The architectural boundary matters: gosmo continues to serve existing connection/catalog
UI and post-operation refreshes. SqlPackage opens its own connections and owns package,
SQL compatibility, permissions and destination validation. The integration adds no
edition/hostname classifier or target preflight. An existing `*gosmo.Server` cannot be
passed to the child process as its connection.

The original requirement explicitly defines TUI menu actions. **CLI parity is an
additional scope decision**, not an existing implementation or an automatic consequence
of adding the manager. Keep the shared package usable from both entry points, but do not
claim CLI support until its command contracts and safety behavior are implemented.

Phase 1, as defined above, supports an existing installation only. It does not meet the
complete one-click install/upgrade definition of done: managed installation and updates
remain necessary. Authentication, redaction and safe cancellation are prerequisites for
the first usable operations, not work that can all wait until Phase 3.

### 13.2. Concrete integration points

Source anchors below refer to the reviewed baseline, commit `abe5a4c`; line numbers may
move during implementation. New package names are proposals, not files already present.

| Area | Existing source / mechanism | Required adaptation |
|---|---|---|
| Shared manager | Proposed `internal/sqlpackage`; package boundaries in `ARCHITECTURE.md` | Own typed requests, discovery, probing, runner and managed assets. Do not import TUI, tcell or tuikit. Start with concrete functions/types; the broad interface in section 3 is illustrative. |
| Tools entry | `internal/tui/menu.go:148`, `App.buildMenus` | Add SqlPackage Manager without requiring a database connection. Menu callbacks read cached status; never start a process or network request inside `Enabled`. |
| Database actions | `internal/tui/explorer_databases.go:334`, `databaseMenuItems`; folder menu at line 314 | Add the four database actions through existing menu builders. A new Tasks submenu is a UI choice: the current menu is flat. Consider the database folder for Import into a new database. |
| File selection | `internal/tui/app_dialogs.go:57`, `newAppFileDialog`; `app_panel_actions.go:76` | Reuse the local `fileDialog().ShowOpen/ShowSave` workflow for executable selection and package files. Do not reuse backup/restore's server filesystem browser. |
| Background jobs | `internal/tui/backup_dialog_actions.go:143`; `internal/tui/tasks.go:33` | Follow `tasks.Start/Progress/Done`, `safegoRepair` and the existing Hide/Cancel lifecycle. Deliver UI changes through `postAndWake`. |
| Application shutdown | `internal/tui/app.go:468`, `quit`; `cmd/gossms/main.go:53`, `FlushSaves` call | Add owned cancellation and bounded process drain; neither existing path cleans up external jobs. Maintain worker completion independently of the closed UI queue. |
| Export promotion | `internal/fileutil/atomic.go:33`, `WriteAtomic`; line 58, `CreateStream` | Add a file-based staging/commit helper. Existing byte/stream writers do not expose an external producer's output-path contract; keep them for metadata and their current callers. |
| Connection translation | `internal/config/config.go:186`, `Connection`; `internal/db/connection.go:189`, `toGosmoOptions` | Build a separate DacFx/SqlClient adapter. Reuse resolved input settings, not the Go driver's connection-string output. |
| Preferences | `internal/config/profile.go:231`, `ConfigDir`; `config_save.go:58`, `BeginSave` | Store only user preferences in Config, using the existing split save lifecycle. Keep managed installation metadata under the configured data directory. |
| Execution guards | Existing menu/dialog guards; CLI policy in `internal/cli/cmd/write.go` | Gate local readiness, validated settings and confirmation/safety policy. SqlPackage determines SQL permissions and compatibility; add no DacFx-specific capability probes. |
| Optional CLI | `internal/cli/tree.go:23`; `internal/cli/cmd/write.go:93`, `applyWrite` | Add command-tree entries and injected runner dependencies only if CLI scope is accepted. External operations need explicit safety, output and audit integration. |

Any new TUI file must be listed in `ARCHITECTURE.md`'s Package map. Reuse modal, focus,
theme and mouse conventions from `docs/ui-rules.md`; do not copy widget mechanics into
the manager. User-facing documentation belongs in README/help, and any CLI additions
also require `docs/cli.md` and the agent guide to describe their actual behavior.

### 13.3. Execution and probe contract

**Version probing:** Microsoft's CLI reference documents `SqlPackage /Version` as the
utility invocation. Use that as the primary probe, with a short timeout and bounded
output. The reference also lists Version among actions; do not assume the alternative
`/Action:Version` and `/a:Version` examples are invalid, but test them before relying on
them across supported installations. Normalize the reported numeric build components.

**Windows process attributes:** Use ordinary piped child execution in supported terminals.
goSSMS is a terminal TUI, not a GUI application. Preserve the repository's no-build-tags/
no-GOOS-suffixed-files policy; GOOS-specific branches are confined to the version package.
Do not introduce a Windows-only runner file or unconditional `SysProcAttr` fields.
Any genuinely necessary platform behavior requires an explicit policy/design resolution.

Select download assets through manifest data keyed by OS/architecture. That does not
justify adding arbitrary OS-specific execution branches to the manager.

**Menu enablement:** Section 6 reads cached readiness only. Discovery/probing is asynchronous
and publishes a snapshot through latest-only result delivery when Refresh or executable
selection supersedes an earlier probe. Explain an unavailable explicit selection; do not
switch silently. Do not derive menu readiness from SQL engine edition or target support.

**Probe isolation:** Follow section 3's no-stdin, explicit-directory, bounded-output/
timeout contract and defined environment. Capture the exact tool for each job. Managed
asset verification is distinct from best-effort health checks of user/system binaries;
neither a version response nor a healthy status proves SQL compatibility.

**Runner skeleton:** The two readers call `onLine` concurrently and discard read errors.
It is not safe to mutate widgets from that callback. Production code needs bounded
buffers, serialized or explicitly thread-safe event delivery, both-stream error handling
and a bounded shutdown path. Waiting indefinitely for EOF before `cmd.Wait` can hang if
a descendant retains a pipe. Define cancellation and pipe-close/WaitDelay behavior and
test it with a helper process. `CommandContext` alone does not promise process-tree
termination or server-side rollback.

**Application lifetime:** Give the manager an application-owned cancellable job lifetime.
Database jobs additionally follow the selected connection's lifetime; probes/installers
must work without a database. `runWithProgress` currently requires `job.sc.Server`, so
use a client-scoped task/progress adapter rather than passing a nil connection.

Explicit Object Explorer Disconnect must not silently cancel an active package job.
Extend the disconnect path to offer confirmed cancel/disconnect or keeping the connection.
After confirmation, cancel/drain its package jobs before closing the selected server;
on cleanup timeout, keep the connection open and report failure. This preserves the
connection-bound task convention without mistaking OE disconnect for harmless UI cleanup.
An unexpected connection failure can still cancel the job; report possible partial state.

Stop accepting jobs once shutdown starts. Register worker-owned completion before launch,
cancel active jobs, then wait under a bounded drain budget for process `Wait` and bounded
pipe cleanup, or for an installer/probe to finish cleanup. Completion must not depend on
`tasks.Done` being adopted by a UI queue that has already closed.

Normal quit with active jobs asks whether to cancel and exit or stay. Keep the event
loop available while cancel/drain runs; if the budget expires, report cleanup failure
and remain open instead of silently abandoning a deployment. A main-level cleanup path
must also cover every `Run` return, recovered panic and signal-driven exit, using the
same owned job supervisor before the process returns. This is new integration work:
`App.quit` currently closes the wake/screen, and main only flushes configuration saves.

Coordinate the signal watchdog with the total bounded save/drain budget. Abrupt OS kill
or a forced watchdog exit cannot guarantee process-tree cleanup or SQL rollback; state
that limit rather than promising recovery. Cancellation must not trigger a deployment
retry or destination deletion. Keep process accounting/drain below the TUI boundary so
CLI can use the same runner lifetime without importing widgets.

### 13.4. Connection and authentication adapter

Capture connection settings on the UI goroutine before starting work. `ServerConn.Opts`
contains the settings and the plaintext in-memory password; `CloneOpts` can adjust a
login database that the pool has left. In either case, set the operation's source or
target database explicitly from the user's selection. Never assume that the pool's
current/login database is the requested package destination.

`db.BuildConnectionString` is built through gosmo for the Go SQL driver. SqlPackage
expects Microsoft.Data.SqlClient connection semantics. Implement a typed, allowlisted
translation rather than passing this string or `ExtraProperties` through unchanged.

The adapter must preserve server/instance/port precedence, database name, authentication,
tenant/client identity, Optional/Mandatory/Strict encryption, certificate trust and host
name validation. Unsupported settings must produce a specific error, not a fallback to
SQL authentication, weaker TLS or an unintended database. Test IPv6 and named instances.
If constructing a connection string, correctly quote values containing semicolons,
quotes or equals signs; an argument array prevents shell injection, not connection-string
parameter injection. Do not mix a connection-string parameter with conflicting individual
source/target parameters; the Export reference specifies exclusive source-string use.

| Existing authentication family | SqlPackage adaptation / acceptance condition |
|---|---|
| SQL authentication | Map user/password through documented parameters or a valid SqlClient string. Process-argument exposure remains a security decision; config encryption does not protect a running child's arguments. |
| Windows authentication | Verify the actual user identity and integrated-auth behavior on each supported OS. Do not assume Go's Windows/Kerberos path and SqlClient's path are interchangeable. |
| Entra interactive / device code | SqlPackage is a separate process and cannot automatically share goSSMS's process-local Entra cache. Define independent sign-in/prompt handling or a verified token handoff before enabling these modes. |
| Entra managed identity / service principal / password | Verify the local runtime/version's documented authentication mapping and required fields. Let SqlPackage validate SQL-side acceptance; secrets retain the same exposure/redaction policy. |
| Entra default chain / Azure CLI | Do not assume SqlClient reproduces the Go credential chain or uses the same signed-in account. Require an independently verified path, otherwise report unsupported authentication. |

`internal/db/entra.go` keeps a process-wide cache and dispatches Go device-code prompts;
it is not a child-process authentication bridge. `/AccessToken` is documented, but using
it requires a deliberate token-acquisition/lifetime design and still exposes a token in
arguments. The reviewed CLI contract does not establish a general password/token-stdin
or environment-variable mechanism. Do not invent one or present a publish profile as a
universal secret channel for all four actions.

Sanitize arguments, errors and streamed output before UI/log/audit delivery. Never use
`cmd.String()` or a raw connection string as an error message. Sanitize terminal control
sequences as well. Diagnostic files/packages and copied command previews must follow
the same policy; disable them by default unless their handling is explicitly designed.
Windows file confidentiality requires actual ACL/inheritance behavior, not a claim that
`Chmod(0600)` implements Unix permissions there.

### 13.5. Operation validation and user-visible semantics

**Local files:** SqlPackage runs on the goSSMS client, so package paths are on that
machine, not the SQL Server host. This differs from native backup/restore and its
`ShowOpenOn/ShowSaveOn` server browser. Validate input files and destination directories,
resolving relative paths against the caller's directory before building absolute arguments.
An explicit tool working directory must not change which package/output path is selected.
Ask before replacing an export. Microsoft's Export defaults to overwriting files;
set an explicit policy rather than relying on that default. Prefer a temporary output
beside the destination and promote it only on success, preserving an existing export.
For a new export, same-directory promotion does not duplicate the output. Replacing an
existing export temporarily retains old and new files, plus any SqlPackage working files.
Handle insufficient space and failed promotion without deleting the old destination.
Use the existing atomic-write machinery where applicable; validate replacement behavior
on Windows rather than assuming a successful rename over a protected/open file.

**File-based promotion contract:** `WriteAtomic` accepts `[]byte`; its `Stream` is an
application writer, not a SqlPackage output-path API. Add a dedicated staging/commit
helper for externally produced files; do not read a multi-GB BACPAC into memory to reuse
`WriteAtomic`, or perform a second whole-file copy solely to reuse `CreateStream`.

Create a private, uniquely owned staging location on the destination filesystem and give
SqlPackage an absolute output filename with the proper extension. On success, close/reap
the producer, verify the output exists and is readable, then commit through a validated
replacement path. Respect existing destination/symlink/read-only rules before production
and again at commit. Failure/cancellation aborts only this job's staging, not the old file.
Never delete the destination first or fall back to truncating it after replacement fails.

Same-filesystem rename avoids another artifact copy, but Go documents that `os.Rename`
is not atomic on non-Unix platforms. Do not promise universal crash-atomic replacement.
Verify normal success/error preservation on Windows, including open/read-only targets;
report promotion failures explicitly. Reuse `WriteAtomic` for small pointer metadata,
with its platform behavior validated separately, not as proof of a universal guarantee.

**Orphan staging policy:** Name the owned directory with a distinct goSSMS SqlPackage
job prefix and record an owner/job marker without credentials. Show/log the absolute
staging path before starting the producer. A forced exit may leave this directory and
large output behind; existing `RemoveStaleTemps` does not cover this naming contract.
Initially leave such leftovers for documented manual cleanup after confirming no
producer uses them. Do not automatically delete by age, PID or a directory-name match:
another process may still be writing, including a child surviving forced termination.
Normal abort removes only this job's registered staging; never scan/delete arbitrary
destination directories. Automatic orphan cleanup requires a separately verified
cross-process ownership/liveness design and is not part of the initial helper.

**Import:** Empty means no user-defined schema objects, not merely no rows. Never
silently truncate/drop objects to make a destination acceptable. SqlPackage checks the
destination; goSSMS does not query its schema or branch on engine type before dispatch.
Show the requested database name and pass it unchanged. Creating a database, using an
existing empty one or requiring external provisioning depends on the tool/platform.
Vendor guidance may require precreation, as in Fabric; that is not a goSSMS workflow branch.

**Publish:** A DACPAC can contain table data and pre/post-deployment scripts. Microsoft's
Publish documentation describes data replacement when deploying a package with data.
Section 6 therefore describes package application, not a promise of schema-only effects.
Default Export DACPAC to schema-only, confirm the requested destination and retain
conservative deployment properties. Do not inspect a DACPAC to classify its SQL target
or silently enable incompatible-platform/data-loss overrides.

Preview is a separate read/planning action, not a guarantee that a later deployment
will be identical. If using Script/DeployReport, run it with the same package, target and
options and mark it as non-applying. Revalidate local inputs/confirmation before Publish;
SqlPackage validates the SQL target for each invocation. Decide whether preview is
mandatory before exposing deployments. Do not parse the preview to implement a goSSMS
target matrix or infer that arbitrary packages are harmless from their extension.

**Export BACPAC:** Microsoft's Export documentation requires no concurrent writes or a
transactionally consistent database copy for a consistent export. Warn accordingly;
do not call it a native backup or promise point-in-time consistency merely because the
process returns zero.

**Gating:** Gate cached executable health, selected connection, supported local adapter
inputs and busy/confirmation state. Keep CLI ceilings/audit/confirmation when applicable.
SqlPackage checks SQL permissions, database state and package compatibility. Neither
existing gosmo classification nor new edition/hostname rules belong in these action gates.
Keep legacy SQL permission-gate coverage intact and add focused external-process guards.
`menuWriteVerbs` currently excludes Deploy/Import, so `TestEveryWriteMenuItemIsGated`
does not automatically cover the proposed labels. Do not add fabricated permission
Notes or weaken the legacy test merely to make these entries pass.

Test Deploy DACPAC and Import BACPAC by invoking their real handler/action with an
injected runner that records calls. Missing tool/connection, invalid local auth mapping,
busy state and declined confirmation must yield zero spawns. Exercise direct callbacks,
not just the rendered menu. For CLI, ceiling/allow/confirmation or audit refusal must
also yield zero spawns. These are local safety checks, not SQL-target classification.

**Completion/cancellation:** Keep the task visible until the child is reaped and output
is drained or deliberately closed. A canceled Import/Publish may leave partial state;
report that possibility and refresh the target catalog. Do not automatically retry a
failed/canceled deployment or drop its destination as cleanup. Never imply that gosmo's
repair logic ran for an operation executed outside gosmo.

### 13.6. Configuration and managed installation

Use `config.ConfigDir()` so the tool follows goSSMS's configured storage location.
Persist explicit selection/update preferences, not credentials or health as durable
truth. Keep the managed active pointer authoritative for installed versions; do not
maintain conflicting path/version truth in both Config and `active.json`.

Config saving already merges another process's changes. Its settings snapshot covers
new exported fields by reflection (`config.go:690`). Reuse `BeginSave`, background `Run`
and UI-thread `EndSave`; do not marshal the whole Config from a worker. CLI loads are
read-only by default, so a future CLI installer/selector needs an explicit writable
management path, not an incidental save from discovery.

The fetched Microsoft download page confirms the proposal's `170.5.96` example and
self-contained ZIP option. It lists Windows/Linux x64 and specific minimum operating
systems; this does not prove matching ARM64 ZIPs for every goSSMS build. A self-contained
runtime still has native OS dependencies. Do not install system packages, modify PATH,
request admin privileges or disable macOS Gatekeeper automatically.
Section 4 records the actual x64 URLs observed during review and the unresolved
170.5.96-page/170.4.83.3-filename discrepancy. Their HEAD responses are not release
approval; the manifest still requires downloaded-content verification and real probes.

The official download page provides moving latest links, not the signed, versioned
SHA-256 manifest this design needs. A pinned goSSMS manifest remains work to establish:
asset provenance, architecture, immutable URL/digest pairing, maximum size, trust root,
update authenticity and licensing review. A redirecting latest URL alone cannot establish
the expected version or checksum. Probe the extracted executable against the manifest.
Section 5 assigns approval/publication to the goSSMS release process and defines the
initial release-bundled source. Dependency updates require newly vetted assets; never
equate an unchanged bundled manifest with proof that Microsoft has no newer fixes.

Extraction must reject traversal through either slash style, drive/absolute paths,
symlinks and excessive decompressed sizes/file counts. Validate the managed destination
and launcher; reject any attempt to escape staging or an existing managed directory.
Keep the entire dependency tree, not just the launcher. Stage and activate on the same
filesystem so directory/pointer promotion is not accidentally a cross-volume rename.

**Important lock limitation:** `fileutil.WithLock` waits two seconds and treats a lock
older than ten seconds as abandoned (`internal/fileutil/lock.go:14`). It is designed for
short read/merge/write sections, not a download or package operation lasting minutes.
Never hold it around the entire install/export/import: a live holder could be superseded.
Concurrent installers need isolated staging and a short, validated publication section.
Existing Windows lock failures observed during earlier testing also need to be resolved
or bounded by the installation design before claiming safe concurrent activation.

`fileutil.WriteAtomic` provides existing small-file replacement machinery, not a complete
installer transaction or cross-process job lease. Capture an immutable executable path
per running job. Preserve old version directories while any process might still use them;
initially keeping previous versions is safer than unverified automatic garbage collection.
Switch the pointer only after verification/probing, and preserve the old selection on
download, promotion or activation failure.
Retain last-known-good pointer metadata for recovery and validate a pointer before use.
The installer must document/test replacement and recovery behavior on supported host
filesystems; a helper named WriteAtomic is not a cross-platform crash-atomicity proof.

### 13.7. Optional CLI integration: do not bypass safety

If accepted, a dedicated `sqlpackage` command group with Extract/Publish/Export/Import
verbs would avoid confusing packages with existing tabular `data import/export` or native
`maint backup/restore`. Exact command names/options remain a product decision. Status
and explicit tool management can be separate local commands; database commands reuse
`internal/cli/connect` profile/override resolution.

Register working commands in the existing command tree with classes, examples and
handlers so help and agent schema stay aligned. Inject process execution for tests.
Send structured results through `output.Result`; stream sanitized diagnostics separately,
never child stdout directly into a JSON/table result envelope. Preserve goSSMS exit/error
codes; Microsoft's contract only guarantees zero versus nonzero, not SQL error details.

`applyWrite` implements dry-run using `gosmo.WithScript`. **This does not intercept a
SqlPackage child process.** Do not call Publish/Import inside that wrapper and label it a
dry-run. Either implement an honest Publish preview through Script/DeployReport with a
defined result contract, or reject unsupported dry-run requests. Import has no equivalent
preview established by the reviewed action contract.

Before spawning a write, apply profile ceilings, `--allow`, required confirmation and
audit-writability checks. Because Publish can execute arbitrary package scripts and data
changes, a conservative administrative ceiling is a proposal until a narrower validated
classification exists. Do not advertise it as harmless DDL. Record target, action,
package identity, tool version and outcome without secrets; use `AuditOmit` for sensitive
options. Do not let raw extra arguments override validated targets or safety properties.

### 13.8. Verification required before implementation is called complete

| Layer | Focused evidence needed |
|---|---|
| Discovery/probe | PATH absent, spaces in paths, explicit selection failure, wrong executable/version, no stdin, explicit directory/environment, output/timeout bounds, replacement after discovery and stale async responses. |
| Runner | Helper-process tests for simultaneous streams, long/no-newline output, read failure, cancellation, inherited pipes, nonzero exit, shutdown without a live UI queue and credential/control-sequence sanitization. |
| Adapter | Named instance/port precedence, IPv6, quoting, every advertised auth family, TLS Strict/certificate host name, unsupported properties and explicit source/target database selection. |
| Installation | Page/archive/probe version mismatch, trusted digest mismatch, hostile ZIP paths/limits, missing native dependency, concurrent staging, pointer recovery, old-version retention and offline local comparison independent of downloads. |
| TUI | Focused zero-spawn Deploy/Import tests, unchanged legacy permission-gate tests, no blocking probe/edition gate, local dialogs, stale selection, Hide/Cancel, panic repair and quit/disconnect with an active job. |
| Artifact promotion | Large externally produced files without ReadAll/copy-for-helper reuse, existing/read-only/open destinations, missing output after success, failed replacement and job-scoped abort preserving the old file. |
| Shutdown | Normal cancel/quit and timeout refusal, loop failure/panic, signal/watchdog budget interaction and UI-independent completion; no promise for uncatchable forced termination. |
| Disconnect | Active package jobs require confirmed cancel/drain before server close; declined confirmation or cleanup timeout keeps the connection open. |
| Orphan staging | Owner marker and path recorded before launch; forced termination may leave files for documented manual cleanup, with no age/PID-only deletion of another producer's staging. |
| Optional CLI | Guard refusal before spawn, broken audit log refusal, ceiling/confirmation, dry-run never applying, secret omission and stable machine-readable output. |
| Real integration | Four actions on disposable databases, schema/data round-trip, SqlPackage nonempty/incompatible-destination errors propagated without app preflight, Publish warning, canceled-state inspection and failed export promotion preserving the old file. |
| Platform acceptance | Drive the built TUI on Windows/Linux/macOS, including clean machines without the SDK where a managed ZIP is supported; do not infer success from Go unit tests. |

Use the smallest relevant commands first, following `docs/testing.md`. Cross-goroutine
changes also need the prescribed race checks in an environment with the required toolchain.
External-tool integration needs an opt-in real SqlPackage smoke suite; normal unit tests
should not download tools or require a live SQL Server.

Remaining decisions before coding are concrete: accepted TUI/CLI scope, host asset/auth
mapping coverage, credential exposure policy, confirmation of the release-owned manifest,
Publish preview/confirmation policy and concurrent installation lifecycle. SQL target
classification/compatibility is not a goSSMS decision. Detection and UI scaffolding are
straightforward; local execution and distribution contracts are the release blockers.

### 13.9. Additional verified vendor references

- [SqlPackage Export](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/sqlpackage-export?view=sql-server-ver17): consistency requirements, overwrite default and source connection parameters.
- [SqlPackage Publish](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/sqlpackage-publish?view=sql-server-ver17): permissions, package data deployment and documented authentication examples.
- [SqlPackage Import](https://learn.microsoft.com/en-us/sql/tools/sqlpackage/sqlpackage-import?view=sql-server-ver17): no user-defined schema objects in an existing destination.

The download, CLI and Fabric references in section 12 were also checked during this
review. These are vendor contracts, not evidence that the proposed goSSMS implementation
already exists or that every authentication/platform combination has been exercised.
