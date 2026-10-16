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
3. **X10** — two map keys; trivial, live-confirmed already. **DONE
   2026-10-15**: keys now `CACHESTORE_COLUMNSTOREOBJECTPOOL` and
   `MEMORYCLERK_SQLQERESERVATIONS`. Tests `memory_test.go` (every group fills
   from its real type name) and livedb `TestLiveEveryClerkGroupKeyExists`
   (every `clerkGroups` key is a type the server reports); both fail with the
   old keys. All twelve keys confirmed present on 13, 14 and 17 (livedb green
   on all three); on 17 the two groups hold 0.03 MB / 1 MB, so both slices now
   draw instead of going to Other.
4. **X11 + X13** — the benign-wait list, one file, one review. **DONE
   2026-10-15**: the `HADR_` family is gone; the six idle HADR waits are
   listed by name, and `HADR_SYNC_COMMIT` plus both flow-control waits go to
   Log (no new bucket). Added from the sqlskills list, each seen accumulating
   live: `PVS_PREALLOCATE` (23.6 M ms on win10cli),
   `PREEMPTIVE_SP_SERVER_DIAGNOSTICS` (~500 ms/sec, forever, on both AG
   replicas — the AG health check), `PREEMPTIVE_XE_DISPATCHER` (39 M ms on
   ubusql1), `AZURE_IMDS_VERSIONS`, plus `PREEMPTIVE_HADR_LEASE_MECHANISM`,
   `UCS_SESSION_REGISTRATION`, `VDI_CLIENT_OTHER`. Tests in `waits_test.go`
   (categorize and benign/real rows), mutation-checked (family restored, Log
   mapping dropped, PVS removed). Live: 20 s idle deltas on ubusql1/2,
   win10cli and SQL2016 leave no background wait above ~25 ms/sec in Other;
   A/B on the AAG1 primary (ubusql2) under a single-row commit loop, Sample
   tab — old binary's Log bar ~260 ms/sec (WRITELOG alone), new ~540 ms/sec
   (HADR_SYNC_COMMIT 8 s of every 20 s). Left for the author: on Linux the log
   flush also shows as `PREEMPTIVE_OS_FLUSHFILEBUFFERS` (8 s/20 s under the
   same loop), which lands in Other; it is commit latency there, but on
   Windows the wait comes from file creation and backups, so it was not moved
   to Log.
5. **X5** — gosmo batch change plus a live run; unblocks a common cleanup.
   **DONE 2026-10-15**: probed on 17 first, which corrected the finding —
   only RESTORING (Msg 5052) and RECOVERY_PENDING (Msg 5181) refuse SET
   SINGLE_USER; OFFLINE, EMERGENCY and STANDBY take it, and the old forced
   drop already deleted those. `exclusiveBatch` (used by Drop and Detach) now
   tests the state in the batch: ONLINE read-write or EMERGENCY → SINGLE_USER
   as before; STANDBY → kill its sessions (the restore's branch), no access
   mode touched; anything else → op as it is. The CATCH releases only a
   database the batch closed (`@closed`). Detach with DropConnections of a
   RESTORING database now detaches (probed). Rename is out of reach: MODIFY
   NAME itself refuses RESTORING (Msg 927) and RECOVERY_PENDING (Msg 945), so
   no gate helps. Also probed: SQL Server keeps an OFFLINE database's files,
   an EMERGENCY one's log and a STANDBY one's undo file on drop — said on
   `Drop`, and gossms's Delete Database confirmation adds "It is OFFLINE, so
   its data and log files stay on disk." (or the EMERGENCY line) from a new
   `nodeData.DBState`. Tests: gosmo `TestExclusiveBatchTakesSingleUserOnlyWhereTheStateAcceptsIt`
   plus the reshaped `assertExclusiveBatch`, mutation-checked (ELSE branch,
   `@closed` guard, `state IN (0, 5)`); livedb
   `TestLiveForcedDropDeletesADatabaseInAnyState` (RESTORING, STANDBY with a
   reader connected, OFFLINE) green on 13, 14 and 17, RESTORING failing on
   the old code; gossms `TestDeleteDatabaseSaysWhichFilesAnOfflineDropLeaves`
   (mutation-checked), via a new `TypedConfirmDialog.Message()`. Live A/B in
   tmux on 17: old binary's Delete of a RESTORING database fails Msg 5052,
   new one deletes it; the OFFLINE one shows the note, is deleted, and its
   two files were indeed left on disk (removed by hand).
6. **X14** — gosmo option + gossms wiring; removes a wrong-database DROP risk.
   **DONE 2026-10-16**, not as a `ScriptOptions` field: the ~55 `Scripter`
   methods share no exit point, so an option would mean a wrapper at every
   return. Instead gosmo exports `Scripter.DatabaseContext()` (the existing
   `scriptUse` batch, `USE [db];` + GO, `]` doubled), and gossms's `ddl`, the
   one generator every database-scoped DDL and DML verb goes through, prefixes
   it for all three destinations. A database's own script (it switches to
   master itself) and every `serverDDL` verb take none; the index/statistic
   maintenance verbs already got USE from `ScriptCollector.String`. Azure SQL
   Database is not exempt: `USE` of the current database is allowed there, and
   one run elsewhere fails rather than landing in the wrong database. Tests:
   gosmo `TestScripterDatabaseContextQuotesTheName`; gossms
   `TestScriptToFileAndClipboardOpensInTheObjectsDatabase` (Clipboard and File
   through the real destinations), the USE prefix added to
   `TestScriptingEachFamilyReachesTheRightObject` (seven families),
   `TestScriptOfAServerScopedObjectTakesNoUse`; mutation-checked (prefix
   removed → the first two fail). Live A/B on win10cli 17 (scratch config,
   throwaway `x14db.dbo.X14T` plus a decoy `master.dbo.X14T`): Script Table
   as → DROP To → Clipboard from the old binary has no USE and, run from a
   master session, dropped master's decoy; the new binary's opens
   `USE [x14db];` and, run the same way, left the decoy alone and acted on
   x14db's table. Script Database as → CREATE gained no USE. Both objects
   dropped afterwards.
7. **X7 + X8** — the Availability Group dialogs together (gosmo breaking:
   `*int`); verify X7 with `PARSEONLY` on the 2016 instance. **DONE
   2026-10-16**. X7: `RequiredSynchronizedSecondariesToCommit` is now `*int`
   (nil omits); `createStatement(major)` writes CLUSTER_TYPE and required sync
   only from 14 — below it WSFC and a zero are dropped, EXTERNAL/NONE, a
   positive count and a replica's FAILOVER_MODE = EXTERNAL (also 2017 syntax,
   not in the finding) are refused with `ErrUnsupportedVersion`; CONTAINED is
   refused below 16; the setter refuses below 14 as well. gossms's General page
   on < 14 shows Cluster type as a static WSFC with its own note, has no
   Required sync row, and defaults the primary's failover mode to MANUAL
   (EXTERNAL was the default, which a WSFC group refuses); Contained only on
   ≥ 16; Availability Group Properties hides Required sync on < 14. X8: gosmo
   `Server.AvailabilityDatabaseCandidates` (one read: state, recovery,
   read-only, snapshot, user access, AUTO_CLOSE, mirroring, log chain)
   replaces the `Databases` + `DatabaseRecoveryStatuses` pair in both pickers;
   `agEligibleDatabases` excludes a snapshot, read-only, SINGLE_USER,
   AUTO_CLOSE and a mirrored database, each with its own reason line. Probed
   with ADD DATABASE on AAG1 (ubusql2, 2025) first: read-only and snapshot
   fail Msg 1466, AUTO_CLOSE 1469, SINGLE_USER 1488, but RESTRICTED_USER
   joins, so it stays offered; mirroring could not be set up here and is from
   the documentation. Tests: gosmo `TestCreateAvailabilityGroupStatementByVersion`
   (13/14/15/16, text and refusals) and `TestSetRequiredSyncRefusedBefore2017`,
   mutation-checked (gate disabled → 9 cases fail); livedb
   `TestLiveAvailabilityDatabaseCandidates` green on 17 and 13; gossms
   `TestNewAGOffersOnlyWhatTheVersionCanCreate` (page rows plus the scripted
   CREATE through gosmo on 13/14/16/unknown; fails with the page gate removed)
   and `TestAGEligibleDatabasesExcludesWhatTheJoinRefuses` (each of four
   exclusions mutation-checked). Live: under `SET PARSEONLY ON` the old
   dialog's default CREATE fails Msg 156 on SQL2016 and the new one parses;
   CONTAINED fails Msg 155 on SQL2017 and parses on 17. A/B in tmux on
   ubusql2 with throwaway read-only and AUTO_CLOSE databases: old binary's Add
   Database offers both, new one lists them under Not offered with reasons; New
   Availability Group's page on 17 unchanged. Not driven in the binary: the
   2016 page itself — SQL2016 has HADR off, so the dialog stops at its
   blocker there. Everything dropped afterwards.
8. **X9 + X1** — both in `~/go/gosmo/tempdb_usage.go`; one live pass.
   **DONE 2026-10-16**. X9: `TempDBFiles` now runs in tempdb (`DatabaseRef("tempdb").query`,
   since `FILEPROPERTY` answers for the current database only) and reads
   every file's used space as `FILEPROPERTY(name, 'SpaceUsed')`, log included;
   the `dm_db_file_space_usage` join is gone. gossms labels the log's bar
   "templog (log)" (Short stays the bare name). X1: `TempDBObjects` sums
   `row_count` only for `index_id < 2`; reserved/used pages still sum every
   index. Tests: gosmo `TestTempDBReadsConvertPagesToMB` (log UsedMB, the
   `USE [tempdb]` prefix, the `index_id < 2` sum) and livedb
   `live_tempdb_test.go` — `TestLiveTempDBObjectsCountsRowsOnce` (a #table
   with clustered + two nonclustered indexes and 1000 rows grows the
   local-temp count by 1000) and `TestLiveTempDBFilesReadsLogUsage`; green on
   17, 14 and 13, and with the old `tempdb_usage.go` they fail (3000 rows;
   templog 0.00 MB). gossms `TestTempDBFileBarsMarkTheLog`, mutation-checked.
   Live A/B in tmux on 17 with a sqlcmd session holding an open transaction
   over a #table (tempdb log 164.8 of 200 MB per FILEPROPERTY): the old
   binary's templog bar is all free; the new one's "templog (log)" splits at
   column 107.1 of 131 (≈164 MB). Note: FILEPROPERTY's data-file numbers can
   differ by a few pages from `dm_db_file_space_usage`'s (304 vs 296 on
   temp2) — it counts allocated pages, not extents.
9. **X12** — Login Properties Script Changes. **DONE 2026-10-16**: a new
   mapping passes the edited schema to `MapToDatabase` (into the CREATE USER,
   as New Login does) and skips the separate set; an already mapped user's
   schema change goes through `d.UserRef(e.user)`, no user read. Test
   `TestUserMappingScriptsANewMappingWithItsSchemaAndRole` (map + schema +
   role under `WithScript`: `CREATE USER … WITH DEFAULT_SCHEMA`, the
   `ALTER ROLE`, no `ALTER USER`; the fake now answers salesdb's user read
   empty), mutation-checked (old code → "database user not found"; the
   `origMapped` gate dropped → a stray ALTER USER). Live A/B in tmux on 17
   (scratch config, throwaway `x12login`/`x12db` with schema `rpt`): map x12db,
   schema rpt, db_accessadmin, Script Changes — old binary fails `database
   user "x12login" not found in "x12db"`, new one scripts `USE [x12db]`,
   `CREATE USER … WITH DEFAULT_SCHEMA = [rpt]`, `ALTER ROLE`, which ran
   cleanly from the window (catalog confirmed); then a real Apply of schema
   guest on the now-mapped user set it with the role kept. Both dropped.
10. **X2 + X3** — table detail and space reads (gosmo breaking for X2).
    **DONE 2026-10-16**. X2: `TableDetail.SchemaOwner` is gone; `Owner` joins
    `database_principals` on `COALESCE(t.principal_id, s.principal_id)` and
    `OwnedBySchemaOwner` is `t.principal_id IS NULL`. gossms's Owner row
    reads `x2_owner`, or `dbo (schema owner)` when inherited
    (`tableOwnerText`). X3: `index_kb` counts allocation types 1 and 3 only,
    in both `SpaceUsed` and `TableSpaceUsedAll`; every LOB_DATA unit stays in
    `lob_kb`, said on `TableSpaceInfo`. Tests: golden `table_detail.sql`
    regenerated and the ledger test's column count 10 → 11; livedb
    `live_table_detail_test.go` — `TestLiveTableDetailOwner` (re-owned to a
    `WITHOUT LOGIN` user, plus an inherited one) and
    `TestLiveTableSpacePartsSumToReserved` (NC index INCLUDEing a filled
    `nvarchar(max)`, both reads), green on 17, 14 and 13, and both fail with
    either fix reverted (Owner `dbo`; parts 24168 KB vs reserved 16160);
    gossms `TestTableOwnerTextMarksAnInheritedOwner`. Live A/B in tmux on 17
    (scratch config, throwaway `x2db`): old binary's Table Properties shows
    Owner `dbo` for the re-owned table and Storage Indexes 8048 KB (the NC
    index's LOB counted twice); new one shows `x2_owner` / `dbo (schema
    owner)` and Indexes 40 KB, 40+40+16016+64 = 16160 reserved. Dropped
    afterwards. Note: the Tables folder's details grid shows Data/Index/Unused
    but no LOB column, so a table's LOB space (base or, now, NC-included) is
    in none of its columns — left as is.
11. **X15** — log file mode. **DONE 2026-10-16**: `OpenLogFile` caps an
    existing gossms.log at `mode & 0600` after opening (`f.Chmod`), and
    gossms.log.1 the same way — rotation renames the old file with its mode,
    and an older build's .1 holds the same server names and logins. Bits
    already inside 0600 are kept, never widened. Test
    `TestOpenLogFileNarrowsAnOlderWorldReadableLog` (0644 log and .1 → 0600;
    0204 → 0200; skipped on Windows), mutation-checked (either chmod removed;
    the cap replaced by a flat 0600). Live: the built binary under a scratch
    `XDG_CONFIG_HOME` with both files pre-created 0644 leaves them 0600.
    `~/.config/gossms/gossms.log` is still 0644 until the next run of a build
    with this fix.

Each item ends with `gofmt -w .`, `go vet ./...` and `go test ./...` in every
repository it touched, the live step it names, and its line here marked
**DONE** with the date and what was verified.
