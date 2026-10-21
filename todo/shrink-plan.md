# Plan: Shrink Database / Shrink Files (SSMS Tasks > Shrink)

Status: IMPLEMENTED 2026-10-21 — unit tests green in both repos; driven live on win10cli (SQL 2025). Not committed.

## Goal

Database node context menu gains SSMS's two shrink tasks:

- **Shrink Database...** — `DBCC SHRINKDATABASE`
- **Shrink Files...** — `DBCC SHRINKFILE`

The dialogs **generate the T-SQL into a new query window; they never execute it.**
No OK/Apply button — Script and Cancel only. The prefetch (space numbers, file
list) is the only thing that touches the server.

## SSMS reference behaviour (from the user's screenshots, SSMS on SQL 2025 / MSSQL17)

Both are single-page ("General") dialogs titled "<Task> - <db>".

### Shrink Database - <db>
Intro: "The size of a database is reduced by collectively shrinking the
database files, releasing unused space. To shrink individual database files,
use Shrink Files instead."

    Database:                       HealthClinic          (read-only)
    Database size
      Currently allocated space:    144,00 MB
      Available free space:         52,90 MB (36%)
    Shrink action
      [ ] Reorganize files before releasing unused space. Selecting this
          option may affect performance.
          Maximum free space in files after shrinking: [0] %   (0-99, disabled
                                                               until ticked)

Script:
Confirmed from SSMS's Script output (user, 2026-10-21), unticked:

    USE [HealthClinic]
    GO
    DBCC SHRINKDATABASE(N'HealthClinic' )
    GO

- unticked → `DBCC SHRINKDATABASE(N'db')` — bare form, **not** TRUNCATEONLY.
  Both dialogs' scripts start with `USE [db]` / `GO`.
- ticked   → `DBCC SHRINKDATABASE(N'db', <pct>)`

### Shrink File - <db>
Intro: "The size of the database is reduced by shrinking individual files to
release unallocated space. To shrink all database files, use Shrink Database."

    Database:                       HealthClinic          (read-only)
    Database files and filegroups
      File type:     [Data       v]   Data / Log (FILESTREAM if present)
      Filegroup:     [PRIMARY    v]   data only; disabled/blank for Log
      File name:     [HealthClinic v] filtered by type + filegroup
      Location:      C:\Program Files\...\DATA\...     (read-only)
      Currently allocated space:    72,00 MB
      Available free space:         52,81 MB (73%)
    Shrink action
      (o) Release unused space
      ( ) Reorganize pages before releasing unused space
            Shrink file to: [72] MB (Minimum is 20 MB)   (default = current
                                                         size; enabled only
                                                         with this radio)
      ( ) Empty file by migrating the data to other files in the same filegroup

Script (preceded by `USE [db]` / `GO`):
1. `DBCC SHRINKFILE (N'f', TRUNCATEONLY)`
2. `DBCC SHRINKFILE (N'f', <MB>)` — the typed MB **verbatim, 0 allowed** (Q5,
   "like SSMS"). Do not reuse gosmo's 1 MB floor here.
   "Minimum is N MB" = used space rounded up to MB; preflight refuses below it.
3. `DBCC SHRINKFILE (N'f', EMPTYFILE)`

Allocated / free numbers: allocated = size*8/1024; free = allocated -
FILEPROPERTY(name,'SpaceUsed')*8/1024; % = free/allocated. Shrink Database
sums over data **and** log files (144 MB in the screenshot = 72 data + 72 log
is consistent with this — confirm on the live server against SSMS).

TUI mapping: propsheet Static rows for read-only, Select for the three combos,
Radio for actions, Check + numeric field; labels must fit LabelWidth 30
("Maximum free space in files after shrinking" does not → shorten, e.g.
"Max free space after shrink %"; the long checkbox text goes in a Note).

## Design

### gosmo (library first, per CLAUDE.md § Changing gosmo)
1. `Database.Shrink(ctx, ShrinkDatabaseOptions{TargetFreePercent int; TruncateOnly bool; NoTruncate bool; LowPriority *ShrinkLowPriority})`
   → `DBCC SHRINKDATABASE`. Under `WithScript` captures the statement.
2. Extend file shrink: `DatabaseFileInfo.ShrinkWith(ctx, ShrinkFileOptions{Mode: ShrinkTarget|ShrinkTruncateOnly|ShrinkEmptyFile|ShrinkNoTruncate, TargetKB})`
   — keep existing `Shrink(ctx, targetKB)` untouched (other callers:
   Database Properties > Files). The new path sends the target verbatim
   (Q5); the existing Shrink keeps its 1 MB floor.
2b. `ShrinkLowPriority{AbortAfterWait: Self|Blockers}` →
   `WITH WAIT_AT_LOW_PRIORITY (ABORT_AFTER_WAIT = SELF|BLOCKERS)` on both
   commands. Verify exact grammar and which modes accept it (TRUNCATEONLY?)
   against a 2022 server / docs before writing the builder.
3. Per-file used space: add `SpaceUsedKB` to `DatabaseFileInfo` filled by
   `Database.Files` via `FILEPROPERTY(name,'SpaceUsed')*8` (needs USE db; works
   in the existing query since Files already runs in db context) — or a separate
   `Database.FileSpace(ctx)` read. Needed for "Available free space" and the
   min size of option 2.
4. Tests in gosmo: statement builders (quoting `N'..'`, `]` in names), WithScript capture.

### gossms
- `internal/tui/shrink_database_dialog.go` — `ShrinkDatabaseDialog`, on the
  `newObjectDialog` shell (like `detach_database_dialog.go`): prefetch =
  `SpaceUsed` + file list; one General page; Script button only — the shell's OK/Apply hidden
  (check `newObjectDialog` has a script-only mode; add one if not).
  `scriptDatabase = dbName` so the window opens in that database.
- `internal/tui/shrink_files_dialog.go` — `ShrinkFilesDialog`: file-type select,
  filegroup select, file select (filtered by the two), static location/size/free,
  radio for the 3 actions, numeric MB field with min/max validation in preflight.
- Menu: `databaseMenuItems` in `explorer_databases.go` — a "Shrink" cascade
  (`MenuItem.Sub`) with "Database..." and "Files...", ONLINE databases only,
  not on snapshots,
  gated `gate.AlterDatabase`-equivalent (DBCC SHRINK* needs sysadmin or
  db_owner — check `docs/db-rules.md` / `gate` for the right check),
  **no** `gateAzure` — offered on Azure SQL DB / MI too.
- WAIT_AT_LOW_PRIORITY: a Check + ABORT_AFTER_WAIT select on both dialogs,
  shown only when the server supports it (major ≥ 16, or Azure).
- App wiring: dialog fields on `App`, `showShrinkDatabaseDialog` /
  `showShrinkFilesDialog`; register in dialog registration test list.
- `ARCHITECTURE.md` § Package map rows for both new files (TestPackageMapListsEveryTUIFile).
- `README.md` feature list; `help_dialog.go` only if a key binding is added (none planned).

### Tests
- Unit: script text for each option combination (fakedb), preflight validation
  (target below used space, % out of range), menu presence/absence by state
  (OFFLINE, snapshot, system db), propsheet labels ≤ 30 chars.
- Live (tmux harness, per docs/testing.md): open each dialog on a disposable
  database on the live test server, press Script, confirm the query window
  content and that **nothing ran** (file sizes unchanged in sys.database_files).

## Open questions

Decided 2026-10-21:
- Q1 Script only, never executes.
- Q2 One file per run (SSMS).
- Q3 Shrink cascade submenu.
- Q4 Yes, WAIT_AT_LOW_PRIORITY, version-gated.
- Q6 Yes, offered on Azure.

- Q5 Like SSMS: typed MB sent verbatim, 0 allowed.
- Q7 Screenshots received; layout above.
