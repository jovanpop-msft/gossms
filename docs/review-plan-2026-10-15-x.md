# Review plan — 2026-10-15 (X series)

A bug hunt across both repositories, at gossms `38d371a` / gosmo `8488a41`,
after the Z-series plan of the same day had closed. The author waived gosmo
backward compatibility for this pass, so items may change gosmo's API; each
names its gossms call sites.

**Item IDs are `X1…X15`** and belong to this plan only. `X` does not appear in
any commit message or document of either repository.

Like every earlier review plan this file is scratch. Delete it once the last
item lands; anything left undone moves to `docs/open-threads.md` (gossms) or
`~/go/gosmo/OPEN-THREADS.md` (gosmo). Every item was checked against
`docs/decisions.md` and the earlier plans: none is a settled question.

---

## How the findings were made

- `go build`, `go vet`, `go test -count=1 ./...`: green on both repositories at
  the start. `nilness`, `unusedwrite` and `shadow` (x/tools v0.51.0) found
  nothing that is a defect.
- **Probes**: throwaway `zz_probe_test.go` files in the package under test,
  run once and deleted (none is left in the tree). X4 and X6 were confirmed
  this way.
- **Live, read-only** on `win10cli` (17.0.1145.4): X6 reproduced in the built
  binary under a scratch `XDG_CONFIG_HOME` (`SELECT … WHERE 1 = 0`, click the
  empty grid, Ctrl+C → `gossms panicked: runtime error: index out of range [0]
  with length 0`, stack through `DataGrid.selectedCellsText` ←
  `App.copySelection`). X10's clerk names and X13's wait totals were read from
  the server's DMVs. Every sp_configure, database scoped configuration and
  Database Mail parameter name gossms hard-codes was cross-checked against the
  live catalog the same way; all matched.
- Not yet run live (they need throwaway objects, so they wait for the
  implementation pass): X5, X7, X9, X12, X14 — each item's **Live** step says
  what to run.

Severity: **High** = crash or data loss; **Medium** = a feature that fails or
shows wrong data in ordinary use; **Low** = a narrow case or a misleading
number.

---

## High

### X6 — Ctrl+C on an empty results grid crashes gossms (gossms, tuikit) — High

`DataGrid.selectedRun` (`internal/tuikit/controls/datagrid.go:543`) answers
`0, 0, true` on a grid with no rows — `selectionBounds` returns the cursor cell
(0,0) whether or not row 0 exists. `selectedCellsText`
(`internal/tuikit/controls/datagrid_overlay.go:14`) then calls
`g.rows.Row(0)` and indexes past the end. `App.copySelection`
(`internal/tui/clipboard.go:112`) calls it for any focused results grid, so a
query that returns a result set with no rows (`SELECT … WHERE 1 = 0`), a click
in the empty grid and Ctrl+C take the whole process down (the UI goroutine has
no recover; `main.run` restores the terminal and exits 1). The cell menu's
**Copy** and **Show Value** (`openViewer`, same file :103, `g.rows.Row(g.selRow)`)
hit the same index from Ctrl+Space; the Always On dashboard's grids reach it
through the same `copySelection` branch. `SelectedRows` also reports `[0]` for
an empty grid; its two hosts happen to bounds-check.

1. In `selectedRun`, return `ok == false` when `g.rows.Len() == 0`, and clamp
   `r1` to `Len()-1`; `selectedRowCount` follows from it.
2. Guard `selectedCellsText` and `openViewer` on `selRow < g.rows.Len()` as
   well, so a stale cursor after a shrinking `SetSourcePreservingView` cannot
   reach the same index.
3. Tests: `SelectedCellsText`, `SelectedRows` and the menu's Copy/Show Value
   on an empty grid return ""/nil/no-op; an `App`-level test that
   `copySelection` with a focused zero-row results grid neither panics nor
   writes the clipboard. Mutation-check by reverting step 1.
4. Live: the reproduction above, before and after (A/B against a kept pre-fix
   binary, per `docs/testing.md`).

### X4 — Line actions take one line too many when a selection ends at column 0 (gossms, tuikit) — High

`affectedLineRange` (`internal/tuikit/controls/editor_actions.go:20`) returns
the selection's last row even when the selection ends at column 0 of it —
which is what Shift+Down from a line start, or a mouse drag to the start of a
line, always produces. Every line action then acts on the following line too.
Probe: text `a\nb\nc\nd`, Shift+Down twice (selected text `"a\nb\n"`), Ctrl+K,C
→ `-- a\n-- b\n-- c\nd`. The same extra line is indented/dedented (Tab,
Shift+Tab), duplicated, moved, and **deleted** (`DeleteLines` removes a line
the user did not select). Visual Studio, SSMS and Notepad++ exclude that row.

1. In `affectedLineRange`, for a linear (not block) selection whose end row is
   past its start row and whose end column is 0, return `er-1`.
2. `IndentLines`/`DedentLines`/`ToggleLineComments` shift `cursorCol` and
   `selAnchorCol` on the rows they touch: an end at column 0 of the excluded
   row must stay at column 0, not move by the indent.
3. Tests: one per action (comment, indent, dedent, duplicate, delete, move up,
   move down), each asserting the text and the selection afterwards, for a
   selection made with Shift+Down and for one made backwards (anchor below).
   Mutation-check step 1.
4. Live: tmux, the probe's keystrokes in a query window.

---

## Medium

### X5 — Delete Database cannot drop a RESTORING or OFFLINE database (gosmo, gossms) — Medium

Object Explorer's Delete always calls `Database.Drop(ctx,
TerminationRollbackImmediate)` (`internal/tui/explorer_object_ops.go:96`),
which runs `exclusiveBatch` (`~/go/gosmo/server.go:239`):
`ALTER DATABASE … SET SINGLE_USER WITH ROLLBACK IMMEDIATE`, and the `DROP`
only `IF @closed = 1`. SQL Server refuses that ALTER on a database that is
RESTORING (Msg 5052, "not permitted while a database is in the Restoring
state"), OFFLINE, RECOVERY_PENDING or SUSPECT, so `@closed` stays 0, the DROP
never runs, and the ALTER's error is the whole answer. A database left
RESTORING by an interrupted restore, or taken OFFLINE, can't be deleted from
the tree — exactly when deleting it is the usual next step. `restore.go`
already gates its own SINGLE_USER on `restoreOnline`; `Drop` does not.

1. gosmo: build the forced drop as `IF <online and read-write> <exclusiveBatch>
   ELSE DROP DATABASE …` server-side (reuse `restoreOnline`), so a state
   change between a read and the batch can't reintroduce the failure. A
   STANDBY database takes the kill-sessions branch, as the restore batch does.
2. gosmo: say on `Drop` that an OFFLINE database's files stay on disk (SQL
   Server does not delete them); gossms's Delete confirmation adds that line
   when the node's state is OFFLINE.
3. Check `Detach` with `DropConnections` and `Rename` with force against the
   same states; apply the same gate where the ALTER is the blocker.
4. Tests: the rendered batch for each branch (`exclusiveBatch` table test).
5. Live (17): a throwaway database restored `WITH NORECOVERY`, and another set
   OFFLINE; Delete each from the tree; both gone. Drop them by hand if not.

### X14 — Script As → File / Clipboard carries no `USE [db]` (gossms, gosmo) — Medium

`scriptDestinations` (`internal/tui/scripting.go:577`) opens a New Query
Editor Window in the object's database, but **File...** and **Clipboard**
write the bare object script. SSMS starts every such script with
`USE [db]` / `GO`. Without it the script runs wherever the reader's window
is: a CREATE pasted into a window on master lands in master, and a
**DROP** script for `[dbo].[Orders]` copied from one database and run in
another drops that database's `[dbo].[Orders]`.

1. gosmo: a `ScriptOptions.IncludeDatabaseContext` (default on for
   database-scoped verbs) that prefixes `USE <QuoteName(db)>;\nGO\n`;
   server-scoped objects (logins, server roles, endpoints, Agent) take none.
   Script Database keeps its own `USE [master]`.
2. gossms: pass it for all three destinations (the New Query window then
   simply restates the database it opened in, as SSMS's does).
3. Tests: one per destination that the text starts with the USE; one that a
   server-scoped object's does not.
4. Live: Script Table as → DROP → Clipboard, paste into a window on master,
   check the first line.

### X7 — New Availability Group always fails on SQL Server 2016 (gosmo, gossms) — Medium

gosmo's support floor is 2016 SP1 (major 13), and `CreateAvailabilityGroupRequest.createStatement`
(`~/go/gosmo/availability_group_create.go:192`) writes:

- `CLUSTER_TYPE = …` whenever `ClusterType` is set — 2017+ syntax;
- `REQUIRED_SYNCHRONIZED_SECONDARIES_TO_COMMIT = n` whenever it is ≥ 0 — also
  2017+, and the zero value of the field writes it;
- `CONTAINED` — 2022+.

New Availability Group (`internal/tui/new_ag_pages.go:78`) defaults Cluster
type to EXTERNAL, always sends the required-sync number
(`internal/tui/new_ag_dialog.go:248`, "the dialog always has a number, so it is
always written") and offers Contained everywhere. On a 2016 WSFC instance every
create is a syntax error; on 2017/2019 ticking Contained is one. The reads are
version-gated (`colSince` in `availability_group.go`); the write is not.

1. gosmo (breaking): `RequiredSynchronizedSecondariesToCommit *int` instead of
   the negative sentinel, and `CreateAvailabilityGroup` checks the server's
   major — below 14 it omits `CLUSTER_TYPE` for WSFC/empty, refuses EXTERNAL
   and NONE, omits a nil/zero required-sync and refuses a positive one; below
   16 it refuses `Contained`. `ag_props.go`'s required-sync setter gets the
   same < 14 refusal.
2. gossms: on major < 14 the General page shows Cluster type as a static
   "WSFC" and hides Required sync secondaries; Contained only on ≥ 16.
3. Tests: `createStatement` per major (13, 14, 16) — exact text and refusals.
4. Live: no 2016 replica can exist here, so parse instead — run the rendered
   statement under `SET PARSEONLY ON` on `win10cli\SQL2016` (major 13):
   fails before, parses after; and the same on 17 with Contained.

### X10 — Activity Monitor never shows Columnstore or Query Grants memory (gossms) — Medium

`clerkGroups` (`internal/activity/memory.go:43-44`) keys two groups by clerk
types that do not exist: `MEMORYCLERK_COLUMNSTOREOBJEC` and
`MEMORYCLERK_SQLQERESERVATION`. Live on 17, `sys.dm_os_memory_clerks` reports
`CACHESTORE_COLUMNSTOREOBJECTPOOL` and `MEMORYCLERK_SQLQERESERVATIONS`. Both
categories are therefore always empty (hidden, since zero-MB rows are dropped)
and their memory is counted under Other.

1. Correct both keys.
2. A unit test that both groups fill from those type names, and a live test
   (`-tags`/`live` convention as the other activity live tests) asserting
   every non-Other key of `clerkGroups` is a type the server reports — so a
   misspelling fails rather than silently reading zero.

### X11 — The Waits chart hides HADR_SYNC_COMMIT (gossms) — Medium

`benignFamilies` (`internal/activity/waits.go:56`) drops every wait starting
`HADR_`. Most HADR waits are idle background waits, but `HADR_SYNC_COMMIT` is
the primary's wait for synchronous secondaries to harden its log — the
dominant wait of a commit-heavy AG primary — and `HADR_DATABASE_FLOW_CONTROL`
/ `HADR_TRANSPORT_FLOW_CONTROL` are real throttling. On exactly the servers
the Always On features are for, the chart shows nothing where the time goes.

1. Replace the `HADR_` family with the idle HADR waits by name
   (`HADR_CLUSAPI_CALL`, `HADR_FILESTREAM_IOMGR_IOCOMPLETION`,
   `HADR_LOGCAPTURE_WAIT`, `HADR_NOTIFICATION_DEQUEUE`, `HADR_TIMER_TASK`,
   `HADR_WORK_QUEUE`, and any further idle ones found on ubusql1/2).
2. `categorize`: `HADR_SYNC_COMMIT` and the flow-control waits to Log (commit
   latency), unless a separate "Always On" bucket is wanted — author's call;
   Log keeps `waitCategoryCount` and the chart shape unchanged.
3. Tests: `isBenignWait`/`categorize` rows for each name.
4. Live: on the AAG1 primary (ubusql1) under a small insert loop,
   `HADR_SYNC_COMMIT` shows in the Log bar.

---

## Low

### X9 — The TempDB tab shows tempdb's log as always empty (gosmo, gossms) — Low

`Server.TempDBFiles` (`~/go/gosmo/tempdb_usage.go:79`) takes each file's used
space from `LEFT JOIN tempdb.sys.dm_db_file_space_usage`, which has rows for
data files only, so the LOG file's `UsedMB` is always 0. `tempdbFileBars`
(`internal/tui/activity_monitor_tempdb.go:153`) draws every file, the log
included, as used/free — a full tempdb log, the classic tempdb incident, looks
empty.

1. gosmo: read used space as `FILEPROPERTY(name, 'SpaceUsed')` for every file,
   run in tempdb's own context (`Database("tempdb")`'s query path), so data
   and log agree.
2. gossms: label the log bar "(log)".
3. Live (17): `BEGIN TRAN` plus a large insert into a `#temp` table in a query
   window, and the log bar fills; roll back.

### X12 — Login Properties: mapping a database and setting its default schema breaks Script Changes (gossms) — Low

`login_props.go:566-593`: a newly mapped database is created by
`l.MapToDatabase(ctx, e.dbName, e.user, "")` — no schema — and the second
loop then reads the user back with `d.UserByName` to call `SetDefaultSchema`.
Under Script Changes the CREATE USER is only collected, the read is real, and
the user does not exist: the script fails "database user … not found". New
Login (`new_login_pages.go`) passes the schema to `MapToDatabase` and has no
such read — Login Properties is the outlier. The real Apply also pays a
needless round trip.

1. Pass `e.schema` to `MapToDatabase` for a new mapping and skip the separate
   set for it; for an already mapped user use `d.UserRef(e.user)` (the
   statement needs only the name — `CLAUDE.md`'s `Ref` rule).
2. Test: Script Changes on map + schema + role over the fake driver yields
   `CREATE USER … WITH DEFAULT_SCHEMA = …` and the `ALTER ROLE`, no error.

### X8 — The AG database picker offers databases an AG refuses (gossms) — Low

`agEligibleDatabases` (`internal/tui/ag_add_database_dialog.go:74`), shared by
New Availability Group and Add Database, promises that ineligible databases
"are listed below rather than offered", but checks only system / already in a
group / FULL recovery / ONLINE / log chain. A read-only database and a
database snapshot (and AUTO_CLOSE, non-MULTI_USER, mirrored databases) are
offered and fail at Apply.

1. Exclude `IsReadOnly` and `IsSnapshot()` from what `Databases` already
   returns; add `user_access_desc`, `is_auto_close_on` and a mirroring check to
   gosmo's database list (or a dedicated eligibility read) for the rest.
2. Tests: one excluded line per reason.

### X13 — PVS_PREALLOCATE is not treated as benign (gossms) — Low

The benign list (`internal/activity/waits.go:42`) lacks `PVS_PREALLOCATE`,
the 2019+ accelerated database recovery background wait for the persistent
version store. win10cli has 23.6 M ms of it (from when ADR was on in a test
database), all of which lands in Other while ADR is enabled anywhere.

1. Add it (and diff the list against the current sqlskills benign list for
   other 2019–2025 background waits).
2. Live: enable ADR on a throwaway database, watch the Other bar with and
   without the fix, drop the database.

### X1 — `TempDBObjects` multiplies row counts by index count (gosmo) — Low

`~/go/gosmo/tempdb_usage.go:133` sums `ps.row_count` over every
`dm_db_partition_stats` row of each object — one per partition of *every*
index — so a temp table with a clustered and two nonclustered indexes reports
three times its rows. (Reserved/used pages are right to sum over all
indexes.) gossms does not draw `Rows` today, but it is gosmo API.

1. `SUM(CASE WHEN ps.index_id < 2 THEN ps.row_count ELSE 0 END)`.
2. Live test: a `#temp` with two nonclustered indexes and N rows reports N.

### X2 — Table Properties' Owner is the schema's owner (gosmo, gossms) — Low

`Table.detailSelect` (`~/go/gosmo/table.go:122`) reads the owner of the
table's schema; `table_props.go:78` shows it as "Owner". A table given its
own owner with `ALTER AUTHORIZATION ON OBJECT::` has `sys.tables.principal_id`
set, and the page names the wrong principal.

1. gosmo (breaking): `TableDetail.Owner` = `USER_NAME(COALESCE(t.principal_id,
   s.principal_id))` plus `OwnedBySchemaOwner bool`; drop `SchemaOwner`.
2. gossms: show Owner; note "(schema owner)" when inherited.
3. Live test: a throwaway table re-owned to a `WITHOUT LOGIN` user.

### X3 — Table space double-counts nonclustered LOB pages (gosmo) — Low

`Table.SpaceUsed` and the database-wide space read (`~/go/gosmo/partition.go`,
the `lob_kb`/`index_kb` columns at :394 and :446) count LOB allocation units
of nonclustered indexes (INCLUDEd `varchar(max)` columns) in both `IndexKB`
(index_id > 1, any type) and `LOBKB` (type 2, any index), so the parts sum
past `ReservedKB`.

1. Restrict `index_kb` to allocation types 1 and 3, keeping LOB in `lob_kb`
   (or the reverse — say which on `TableSpaceInfo`).
2. Live test: a table whose nonclustered index includes a filled
   `nvarchar(max)` column; `DataKB + IndexKB + LOBKB + UnusedKB == ReservedKB`.

### X15 — gossms.log stays world-readable from older builds (gossms) — Low

`OpenLogFile` (`internal/config/config.go:458`) opens with mode 0600, which
applies only when the file is created; its comment says the log is 0600
because it records server names, logins and error text. A log created by an
earlier build keeps its mode — on this machine `~/.config/gossms/gossms.log`
is 0644 in a 0755 directory, readable by every local user. Rotation renames
it with the same mode.

1. After opening, narrow an existing file to `mode & 0600` (`f.Chmod`), the
   cap `fileutil`'s `modeFor` already applies to WriteAtomic targets. Windows
   needs no branch: Chmod there only toggles read-only.
2. Test: a pre-created 0644 log comes back 0600 (skip on Windows).

---

## Recommended implementation order

Small, self-contained, high-impact first; items sharing a file are batched.
gosmo changes go in first and are built and tested inside `~/go/gosmo`
(`replace` is active), per `CLAUDE.md` § Changing gosmo.

1. **X6** — the crash; tuikit only, an hour with tests. **DONE 2026-10-15**:
   `selectedRun` clips to the rows that exist; `selectedCellsText`/`openViewer`
   read through `Row` (nil past the end); the menu's Copy and `copySelection`
   skip a zero-row selection (new `DataGrid.SelectedRowCount`), so the
   clipboard is not blanked. Tests `datagrid_empty_test.go` and
   `TestCopyFromEmptyResultsGridLeavesClipboardAlone`, each mutation-checked
   (clip, `Row` guard, both copy gates). Live A/B on win10cli 17: old binary
   panics as described, new one copies nothing and stays up.
2. **X4** — silent extra-line edits (Delete Lines loses text); tuikit only.
   **DONE 2026-10-15**: `affectedLineRange` drops the end row of a linear
   multi-row selection ending at column 0 (block selections keep it), so step
   2's column shifts follow without code of their own. Two knock-ons fixed:
   `MoveLinesDown` pins a selection end that would land past the buffer (block
   moved to the bottom) to the last line's end, and `DuplicateLines` puts a
   cursor on the excluded row on the copy's last line. Tests
   `editor_linerange_test.go`: every action, forwards and backwards, text plus
   anchor/cursor/SelectedText, and the rows that must stay included (end past
   column 0, single row, block); mutation-checked (the `er--`, the block guard,
   the duplicate `min`, both move-down pins). Live A/B in tmux on a query
   window (win10cli 17, scratch config): `a b c d`, Shift+Down ×2 from the top
   — old binary comments/indents/dedents/duplicates/deletes/moves three lines,
   new one two. Toggle comment is Ctrl+/ in gossms (not Ctrl+K,C).
3. **X10** — two map keys; trivial, live-confirmed already.
4. **X11 + X13** — the benign-wait list, one file, one review.
5. **X5** — gosmo batch change plus a live run; unblocks a common cleanup.
6. **X14** — gosmo option + gossms wiring; removes a wrong-database DROP risk.
7. **X7 + X8** — the Availability Group dialogs together (gosmo breaking:
   `*int`); verify X7 with `PARSEONLY` on the 2016 instance.
8. **X9 + X1** — both in `~/go/gosmo/tempdb_usage.go`; one live pass.
9. **X12** — Login Properties Script Changes.
10. **X2 + X3** — table detail and space reads (gosmo breaking for X2).
11. **X15** — log file mode.

Each item ends with `gofmt -w .`, `go vet ./...` and `go test ./...` in every
repository it touched, the live step it names, and its line here marked
**DONE** with the date and what was verified.
