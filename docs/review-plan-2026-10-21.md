# Review plan — 2026-10-21

The fix list from a whole-repo review of gossms and gosmo. Both repos are clean
under `go vet`, `staticcheck` and `govulncheck`; `dupl` (90 tokens) finds only
small clones; every `deadcode` hit is already settled in `docs/decisions.md` §
By design. The findings below come from reading code against the rules in
`CLAUDE.md`, `~/go/gosmo/CLAUDE.md` and `docs/decisions.md`. Nothing listed as
settled there is re-raised. One exception: R7 gives a *new* reason to revisit
the closed `internal/tui` restructuring, and says so.

`docs/decisions.md` § No top-level plan document still holds. This file exists
only because it was asked for. Delete it when the work is done; move anything
left over into `docs/open-threads.md` or `~/go/gosmo/OPEN-THREADS.md`.

Breaking gosmo's API is allowed for this pass. **Approved breaks (2026-10-21):** S1, S2, R2 and R3, all four. Every gosmo change still follows
`~/go/gosmo/CLAUDE.md`:

- build and test in gosmo first;
- add a write-statement test for every changed write;
- run `go vet -tags livedb ./...` after any rename;
- run `TestLiveVersionSweep` on the oldest instance (win10cli\SQL2016) after
  any query change.

IDs: **B** bug, **C** consistency, **S** simplification, **O** optimisation,
**R** refactor or architecture, **D** docs or repo hygiene.

---

## Bugs

### B1 — gosmo: AG replica lag and seeding mode are blanked on Azure and on an unknown version

`availability_database.go:73-77` and `availability_replica.go:80-84` gate with a
bare `if major < int(SQLServer2016)`. `serverMajorVersion()` returns **0** on
every Azure edition and on a Server whose version was never read, and 0 < 13.
So on a Managed Instance (engine 18, link or failover group) and on
`major == 0`, `secondary_lag_seconds` reads as `CAST(0 AS bigint)` and
`seeding_mode_desc` as `''`. Every other gate in gosmo treats 0 as newest
(`version_gate.go`'s documented convention). These are the only two gates that
don't.

**Fix:** delete both gates. 2016 is the floor (`MinimumServerVersion`), so the
column always exists. Also delete the four `colSince(major, SQLServer2016, …)`
calls in `availability_group.go:105-108`; they are dead for the same reason.
**Test:** extend the version-gate golden test (`testdata/version_gates/`) so
both reads at major 0 name the real columns. Mutation check: put the gate back
and confirm the test fails.

### B2 — gossms: no warning when connecting to an instance older than SQL Server 2016

`MinimumServerVersion` (gosmo `types.go:42`) is documented and never enforced.
A 2014 or older instance connects fine, then reads fail one by one with
"Invalid column name", which looks like a gossms bug. gossms never checks
`VersionMajor` against it.

**Fix:** in `internal/tui/app_connections.go` (after `info := sc.Server.Info()`,
~line 77), when `!info.IsAzure() && info.VersionMajor < int(gosmo.MinimumServerVersion)`:

- show a one-time `dialogs` notice: "SQL Server <ver> is older than 2016 SP1,
  the oldest version goSSMS supports; some folders and dialogs will fail";
- keep the connection.

**Decided:** also add `ServerInfo.Supported() bool` in gosmo (Azure and an
unread version count as supported), so the rule lives in one place and gossms
calls it. Don't refuse the connection: a read-only look at an old instance
still mostly works.

### B3 — gossms: Shrink Database's unticked "Reorganize" is worded (or scripted) wrong

Unticked, `shrink_database_dialog.go` sends `ShrinkDatabaseOptions{}`, which is
a bare `DBCC SHRINKDATABASE (N'db')`. That form **moves pages**: it is the heavy
operation.

- The page's own note (`:157`, "Selecting this option may affect performance")
  implies the unticked form is the light one.
- Microsoft's documentation of the SSMS dialog says that clearing the box equals
  `TRUNCATEONLY`.
- gosmo's `ShrinkDatabaseOptions` doc says the zero value is "the bare statement
  SSMS scripts with Reorganize unticked".

At least one of these three is wrong.

**Fix:**

1. Script the dialog both ways from SSMS 21 against win10cli. This needs the
   user: SSMS runs on Windows.
2. If SSMS scripts `TRUNCATEONLY`, set `Truncate: gosmo.ShrinkTruncateOnly`
   when unticked, and correct gosmo's doc comment on `ShrinkDatabaseOptions`.
3. If SSMS scripts the bare form, rewrite the notes at `:157`/`:159`. Say that
   both forms move pages and that the box only adds a free-space target.

Record the answer in `docs/decisions.md`.

### B4 — gosmo: doubled `gosmo:` prefix on AG replica refusals

`availability_replica.go:256-258`: `modifyReplica` returns
`invalidf("gosmo: modify replica …")`. Every caller (`:272`, `:309`, `:323`, …)
wraps it again with `"gosmo: set …: %w"`. The result reads
`gosmo: set primary role connections of replica "x": gosmo: modify replica "x": …`.

**Fix:** drop the prefix inside `modifyReplica` (the shared-helper convention:
the caller wraps).
**Guard:** add a test helper in `invalid_request_test.go` that fails when an
error string holds `gosmo:` twice, and run it over that file's table. It will
likely find other sites; fix those the same way.

---

## Consistency

### C1 — gosmo: five spellings of one version gate

In use today:

- `colSince(major, v, …)`
- `hasColumnSince`
- `major == 0 || major >= X`
- `major != 0 && major < X`
- `major > 0 && major < 15` (`filesystem.go:163`, a magic number)
- `ServerInfo.Supports*` (`types.go`)
- `shrinkStatement`'s own check (`shrink.go:185`), which duplicates
  `SupportsShrinkLowPriority` (`types.go:571`)

B1 is what that variety costs.

**Fix (with R1):** one pair of helpers on `*Server`, `atLeast(v ServerVersion)
bool` and `before(v) bool`, with the "0 = newest" rule in exactly one place.
`colSince` calls `atLeast`. Then:

- `shrinkStatement` calls `s.info.SupportsShrinkLowPriority()`;
- `filesystem.go:163` names `SQLServer2019`;
- `query_store.go:204`'s double `serverMajorVersion()` call goes.

Add a source-scan test, like `write_through_read_test.go`, that fails on
`serverMajorVersion()` compared with `<`/`>=` outside `version_gate.go`.
Check while there: `sys.dm_os_enumerate_fixed_drives` is documented from 2017
(14), but the gate sends 14 to `xp_fixeddrives`. Run `FixedDrives` live on
win10cli\SQL2017. If the DMV answers there, lower the gate to `SQLServer2017`.

### C2 — gosmo: `Server.exec` and `Database.exec` differ in shape

`script.go:308` is `(*Server).exec(ctx, stmt) error`, with no args. `database.go:140`
is `(*Database).exec(ctx, q, args...) (sql.Result, error)`. Callers moving
between scopes rewrite the call and its error handling. Internal only.

**Fix:** give `Server.exec` the same `(ctx, q, args...) (sql.Result, error)`
shape, so `bindScriptArgs` covers server-scope writes too. Mechanical. The
existing write tables pin the statements.

### C3 — gossms: Shrink File throws away the handle it already loaded

The file list is already in hand, from `DatabaseRef(db).Files`, each entry with
its `Type`. Even so, `shrink_files_dialog.go:333` applies through
`DatabaseRef(db).FileRef(name)`, which has no `Type`. gosmo's FILESTREAM refusal
can't fire, so `:299` copies it into gossms.

**Fix:** have `preflight` keep the selected `*gosmo.DatabaseFileInfo`, call
`f.ShrinkWith`, and delete the gossms copy of the FILESTREAM check. (The
`DatabaseRef` read is correct: it carries the database name for the `USE`.
`CLAUDE.md` § `Ref` rule.)

---

## Simplifications

### S1 — gosmo: one shrink-file method

`DatabaseFileInfo.Shrink(ctx, targetKB)` and `ShrinkWith(ctx, ShrinkFileOptions)`
overlap. They also disagree on target 0:

- `Shrink` floors it to 1 MB;
- `ShrinkWith` sends it verbatim, and gossms floors it itself
  (`shrink_files_dialog.go`, "0 goes as 1").

**Fix (breaking):** keep one method,
`Shrink(ctx, ShrinkFileOptions) (ShrinkResult, error)`. `ShrinkResult` holds the
DBCC row: `CurrentKB`, `MinimumKB`, `UsedPages`, `EstimatedPages`, zero under
`WithScript`. It takes `TargetMB *int64` with one documented rule: a target of 0
is sent as 1, and an explicit opt-out field covers anyone who wants SHRINKFILE's
own 0. Delete gossms's own floor.

### S2 — gosmo: per-partition options as parallel slices

`Index` carries `DataCompression`, `PartitionCompression []DataCompression`,
`XMLCompression` and `PartitionXMLCompression []bool` (`index.go:45-56`). They
are two parallel slices plus two "first partition" copies.
`tableScriptOptions` repeats the shape for heaps (`scripter_table.go:136-142`).
The next per-partition option (an online-rebuild state, a filegroup) adds two
more fields.

**Fix (breaking):**

- `Partitions []PartitionOptions{Number int; DataCompression DataCompression;
  XMLCompression bool}`;
- keep `DataCompression`/`XMLCompression` only as **methods** that derive the
  first partition's value (a derivation: allowed by the "fields, not accessors"
  rule);
- `partitionCompressionList` decodes into the one slice;
- `compressionOptions`/`xmlCompressionOptions` take `[]PartitionOptions`;
- gossms `rebuild.compressionRows()` (index/key/table props) reads it.

### S3 — gosmo: one generic module-list reader

`viewsWhere`, `systemViewsWhere`, `storedProceduresWhere`,
`systemStoredProceduresWhere` and the two function readers (`view.go:42-98`,
`procedure.go:186-292`, `function.go:61-121`) are one query and one scan with a
different `FROM`/type filter.

**Fix:** a `moduleListWhere[T]` taking the source (`sys.views v` / `sys.all_objects
o WHERE o.type IN (…) AND o.schema_id = 4`) and a constructor. The system
variants also drop `SCHEMA_NAME(o.schema_id) = 'sys'` for `o.schema_id = 4`,
which is sargable (`sys` is schema 4 in every database). Same results; the
golden SQL updates.

### S4 — gossms: the two apply pipelines share their tail by copy

`newObjectDialog.runPipeline` (`new_object_dialog.go:331-389`) and
`PropDialog.runPipeline` (`prop_apply.go:320-378`) repeat the same sequence:

- `StartApplying` and the spinner;
- `watchBlocking`, then `safegoRepair`;
- `runApplySteps`, then the post back.

They differ only in labels, the failure handler and `applyNotes`. Only
Properties attaches `applyNotes`, so a `noteApply` in a New dialog's step is
silently dropped. No such step exists today; `database_props_files.go:595` is
the only caller.

**Fix:** extract `runApplyPipeline(host, label, ctx, fns, onDone func(progress,
err, notes))` into `prop_apply.go`, and have both call it with notes always
attached.

### S5 — gossms: one SQL lexer-state cache per editor

`Editor.sqlStarts` (`controls/editor.go`, added in d241914 for `SQLLineState`)
lexes the document a second time. The SQL highlighter's prefix-state cache, in
its closure, already does that. On a large script both caches run after every
edit.

**Fix:**

- move the highlighter's cache onto the `Editor`, as one
  `prefixStates[sqltext.State]` filled on demand;
- have `SQLHighlighter` read it through an interface the editor passes;
- leave an XML editor with no cache, as today.

Keep `TestFormat…Selection…` and the highlighter tests green.

---

## Refactors and architecture

### R1 — gosmo: version gating as one small API (do with C1)

Beyond C1's helpers:

- make `colSince`'s `zero` argument derivable from a typed column descriptor
  where it is repeated often (the `CAST(0 AS bit)` majority);
- delete every gate at or below `SQLServer2016` (B1, `availability_group.go`,
  the `QSMetric… SQLServer2016` entries in `query_store_report_spec.go:65-73`,
  which can use 0).

The version-gate inventory test (`version_gate_inventory_test.go`) becomes the
list of gates.

### R2 — gosmo: typed string enums for catalog states and kinds (breaking)

Many exported fields are free strings compared against literals on both sides.
gosmo has 37 `Type|State|Status|Kind|Mode string` fields, for example:

- `Database.State`;
- `DatabaseFileInfo.Type`/`.State`;
- `Endpoint.State`, `AvailabilityDatabase.State`, `ServerAudit.Status`.

gossms has 21 raw comparisons, such as `node.data.DBState != "ONLINE"` and
`f.Type == "FILESTREAM"`. A typo compiles.

**Fix:** `type DatabaseState string` with constants (`DatabaseOnline`, …) and
`type FileType string` (`FileRows`, `FileLog`, `FileStream`), as
`RecoveryModel` already is. Start with the fields gossms compares. Comparisons
with untyped constants keep compiling, so the break is limited to assignments
from `string`. Move gossms to the constants in the same change.

### R3 — gosmo: collapse `X` / `XFiltered` / `XOfKind` list pairs (breaking)

There are nine `…Filtered` methods. Tables alone has four entry points: `Tables`,
`TablesFiltered`, `TablesOfKind` and `TablesOfKindFiltered`.

**Fix:**

- one method per family taking the filter: `Views(ctx, ObjectFilter{})`, where
  the zero value means all;
- for tables, `Tables(ctx, TableQuery{Kind, Filter})`;
- update gossms's explorer loaders.

This removes about 9 exported methods with no capability lost: narrowing is
forbidden, merging is not. Check `~/go/gosmo/CLAUDE.md` § This is a library
before starting, and say in `CHANGELOG.md` at release time how to migrate.

### R4 — gossms: dialogs built on demand, not 55 eager App fields (declined for now)

`App` has about 100 fields. 55 of them are dialogs, all built in `buildUI`
(`app.go:5xx`) and registered in `allDialogs`. `syncDialogStack`
(`dialog_stack.go:35`) then polls every one's `Visible()` twice per event-loop
pass. `TestEveryAppDialogFieldIsRegisteredInAllDialogs` exists only because the
field and the registration can drift apart.

**Option:** keep the fields, but construct each on first use through
`lazyDialog[T]`, and register at construction. Startup cost and the polling
both drop to the dialogs actually opened. Low value; do it only if startup time
or `App`'s size starts to hurt. It must not change behaviour: the dialogs are
deliberately singletons (state such as `setDatabaseItems`'s kept selection
lives on them).

### R5 — gossms: a frontend-neutral layer before the CLI or GUI work (parked)

**Decided 2026-10-21: not yet.** The item stays here, gated on a decision to build
the CLI or GUI. Don't start it.

`todo/todo.txt` plans two new frontends:

- an "agentic" command line (`gossms get property maxdop -conn …`, text
  output);
- a gogui GUI, "developed and maintained in parallel".

Today 226 of the 321 `internal/tui` files import gosmo directly, with 623
`sc.Server.` calls. Database reads, permission gating (`gate` is already
separate), apply pipelines and script generation are woven into tcell
widgets. A second frontend would copy them.

`docs/decisions.md` § By design closed the `internal/tui` split, on compile
time and package boundaries. A second frontend is a new reason, not the old
one re-raised. **Decide first; don't start this unprompted.** If the CLI goes
ahead:

1. Create `internal/ops` (no tcell import, enforced by a test like
   `TestImportsOnlyStdlib`). It holds per-feature read models and apply plans
   (`applyPlan`, `runApplySteps`, `pending_edits`, and each property page's
   load/diff/apply minus its widgets).
2. Move features one at a time, starting with what the CLI needs first:
   server properties (MAXDOP), database properties, scripting.
3. Leave `internal/tui` holding only widgets plus the binding to `ops`.

### R6 — gossms: `internal/format/sql_clauses.go` is over 900 lines

`sql_clauses.go` is now 905 lines. Per `CLAUDE.md` § Coding conventions, split
it along its section comments the next time it is touched:

- extract by exact line range;
- diff byte for byte;
- add an `ARCHITECTURE.md` § Package map row only if the new file is in
  `internal/tui`, which this one isn't.

---

## Optimisations

There are few: the hot paths (results arena, completion caches, Databases-folder
fan-out) are already measured and settled.

- **O1** — S3's `o.schema_id = 4` (system object folders).
- **O2** — S5's single lexer cache (editing a large script).
- **O3** — the 5 MB showplan format target stays where it is tracked,
  `docs/open-threads.md` § Format Document. Not duplicated here.

---

## Docs and repo hygiene

- **D1** — `CLAUDE.md` § Coding conventions says "Go 1.26+ in use". `go.mod` is
  1.27, and `docs/decisions.md` says both repos track the latest Go. Change it to
  "Go 1.27 (tracks the latest release)".
- **D2** (declined for now: leave as is) — `docs/linkedin/` holds `proposal_01_veil.png` twice (byte-identical
  at the top level and in `res_v0.1.0/`), plus stale copies of the v0.1.0
  README and RELEASE in `res_v0.1.0/`. That release material is marketing
  scratch, so move it under `todo/` or out of the repo. Ask before deleting.
- **D3** — when B3 and C1 land, record the answers in `docs/decisions.md` (B3)
  and in `~/go/gosmo/CLAUDE.md` § Conventions (C1: "gate through
  `atLeast`/`before` only"). Each needs a one-line rule where it will load
  again, per `CLAUDE.md` § Self learning.

---

## Recommended implementation order

Small, safe and bug-fixing first. Breaking gosmo changes next, in an order where
each leaves both repos building. Optional and decision-gated last. gosmo before
gossms within each step, with `replace` active.

| # | Item | Repo | Why here |
|---|------|------|----------|
| 1 | **B1** AG gates + **C1/R1** gate helpers | gosmo | **Done 2026-10-21** (uncommitted). FixedDrives gate lowered to 2017 after the live check; D3's C1 rule is in gosmo `CLAUDE.md`. |
| 2 | **B4** doubled prefix + the guard test | gosmo | **Done 2026-10-21** (uncommitted). Five more sites fixed (`scriptExecProc`, `bindScriptArgs`, `parseEntraConfig`, and two exported-to-exported wraps); static guard `TestNoWrapOfAnAlreadyPrefixedError`; rule added to gosmo `CLAUDE.md`. |
| 3 | **B2** pre-2016 warning | gosmo `Supported()` → gossms | **Done 2026-10-21** (uncommitted). `ServerInfo.Supported()` also checks the SP1 build (13.0.4001), as `MinimumServerVersion` documents; Object Explorer connects only. Driven live on win10cli\SQL2016 SP3 (no warning); no pre-2016 instance exists to drive the warning itself, so `TestWarnIfUnsupported` covers it. |
| 4 | **B3** verify against SSMS 21, then fix | gossms (+ gosmo doc) | **Done 2026-10-21** (uncommitted). SSMS 21 scripts the bare form unticked and `(N'db', N)` ticked, so option 3: the note under the box now says both forms move pages; gosmo unchanged. Recorded in `docs/decisions.md` § By design. |
| 5 | **S1** one shrink-file method, then **C3** | gosmo → gossms | **Done 2026-10-22** (uncommitted). `DatabaseFileInfo.Shrink(ctx, ShrinkFileOptions) (ShrinkResult, error)` replaces both methods; 0 is sent as 1 unless `SendZeroTarget`; a no-row answer is a zero result. gossms's floor and FILESTREAM copy are gone; Shrink File shrinks the loaded file. Live tests pass on 2025 and SQL2016; both dialogs driven in tmux. |
| 6 | **C2** `Server.exec` shape | gosmo | **Done 2026-10-22** (uncommitted). `Server.exec(ctx, q, args...)`, `execSecret` and `execPasswords` now return `(sql.Result, error)` like `Database`'s, through a `Server.execShown` that binds args via `bindScriptArgs` for capture and observation; callers in 58 files adjusted mechanically. New tests `TestWithScriptBindsServerScopeParameters` and `TestStatementObserverServerScopeBindsArguments` (mutation-checked). No statement changed, so no live run. |
| 7 | **S2** `Partitions []PartitionOptions` | gosmo → gossms | **Done 2026-10-22** (uncommitted). `Index.Partitions` replaces `DataCompression`/`PartitionCompression`/`XMLCompression`/`PartitionXMLCompression`; `DataCompression()` (NONE with no partition, as the read set it) and `XMLCompression()` derive the first partition's. `tableScriptOptions.HeapPartitions` replaces the three heap fields; `compressionOptions`/`xmlCompressionOptions` take `[]PartitionOptions`. gossms's two `index_props.go` reads call the method (no `rebuild.compressionRows()` exists). No SQL changed; the partition, XML-compression, SetIncludedColumns and by-name live tests pass on win10cli. |
| 8 | **R2** typed state/kind enums | gosmo → gossms | **Done 2026-10-22, both cuts** (uncommitted). *First cut:* New `DatabaseState` (`Database.State`, `DatabaseSnapshot.State`, `AvailabilityDatabaseCandidate.State`, `AvailabilityDatabase.DatabaseState`), `FileType` (`DatabaseFileInfo.Type`, `DatabaseFileSpec.Type`, `TempDBFile.Type`) and `FileState` (`DatabaseFileInfo.State`) in `types.go`; both endpoint structs' `State` is now the existing `EndpointState` (`DatabaseMirroringEndpoint.setState` takes it too). gosmo's and gossms's literal comparisons on these fields moved to the constants; display sites convert with `string()`; gossms's holders (`nodeData.DBState`, `directoryEntry.state`, `detachPrefetch.state`, `agDBCandidate.State`, the Shrink File picker) carry the typed value; the three `strings.EqualFold(ep.State, "STARTED")` checks are now `== gosmo.EndpointStarted`. Watch: a comparison through `any` keeps compiling and goes false (gosmo's `TestScriptedSetterDoesNotMirrorOntoTheReceiver` did). Live reads of every retyped field pass on win10cli; Databases folder, Detach, Shrink File and Endpoints driven in tmux. *Second cut:* AG state — `ReplicaRole`, `ReplicaOperationalState`, `ReplicaConnectedState`, `RecoveryHealth`, `SynchronizationHealth` (group, replica and database) and `SynchronizationState` (`"NOT SYNCHRONIZING"` has a space); `AuditStatus` (`ServerAuditStatus.Status`; only `AuditStarted` — a stopped audit has no DMV row, so the old "STARTED / STOPPED" comment was wrong, checked live); `BackupFileType` (`BackupFile.Type`); `FileGroupType` (the three existing `…FileGroup` constants, now typed); one `PrincipalType` for both scopes' type_desc (`Login.LoginType`, `User.UserType`, `RoleMember.Type`, every permission entry's `PrincipalType`); and `PermissionState` (+ read-only `PermissionGrantWithGrantOption`) on `ServerPermissionEntry`/`DatabasePermissionEntry`/`PrincipalSecurable.State` and `LoginDetails.ConnectSQLState`. gossms's permission editor now carries `gosmo.PermissionState` end to end (`perm_state.go`, `permEntry`, `permEdit`, `securableEdit`, `columnEdit`); `titleWord` is generic over `~string`. **Bug found and fixed on the way:** New Login → Securables cycles through Grant With Grant but applied it as `PermissionVerb("GRANT_WITH_GRANT_OPTION")`, which gosmo refuses *after* the login is created; it now goes through `applyPermChange` (`TestNewLoginSecurablesGrantWithGrantIsAGrant`, mutation-checked). Also fixed two `livedb` compile breaks (`live_endpoint_test.go`, `live_filestream_test.go` from the first cut; `live_propfinders_test.go`'s `tbl.Name` from `HEAD`). Verified: gosmo live suites on win10cli plus `TestLiveAvailabilityGroupRead` on ubusql1; in tmux, AG tree labels and dashboard on ubusql1, and New Login/Login Properties (General, Securables, Status, Server Roles) on win10cli with a throwaway login, checked against `sys.server_permissions`. **Not typed (deliberately left):** the rest of the 37 are free text or have no caller comparing them (`WaitType`, `BaseType`, `ValueType`, `Level*Type`, XE/replication/mail/full-text kinds, `Endpoint.Type`, `AvailabilityListener.State`, `Table.TemporalType`/`LedgerType`, …); type one when a comparison appears. |
| 9 | **S3/O1** generic module-list reader | gosmo | Internal; golden SQL updates. |
| 10 | **R3** collapse list-method pairs | gosmo → gossms | Largest gosmo break; after S3 so the readers are already one. |
| 11 | **S4** shared apply pipeline | gossms | Internal; covered by the existing pipeline tests. |
| 12 | **S5/O2** one lexer cache | gossms (tuikit) | Touches editor and highlighter; needs a tmux drive. |
| 13 | **D1–D3** docs | both | With or after the items they describe. |
| 14 | **R6** split `sql_clauses.go` | gossms | Only when next edited. |
| 15 | **R4** lazy dialogs | gossms | Declined for now; skip. |
| 16 | **R5** frontend-neutral layer | gossms | Parked; skip. |

Each step is done only when:

- `go build`, `go test ./...`, `gofmt` and `go vet` are clean in both repos
  (`-tags livedb` vet in gosmo after any rename);
- the live checks named in the item have run;
- for a user-visible change, the built binary has been driven in tmux, per
  `docs/testing.md`.
