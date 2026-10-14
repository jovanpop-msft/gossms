# Review plan — 2026-10-14 (Y series)

A sweep of both repositories' known open work on 2026-10-14, at gossms
`4eb0d3a` / gosmo `5843caf`, keeping only what can be fixed with the estate as
it stands (win10cli 13/14/17, ubusql1/2). Sources: `docs/open-threads.md`,
`~/go/gosmo/OPEN-THREADS.md`, and a fresh `go vet ./...` + `go test ./...` on
both trees (gosmo green and vet-clean; gossms red on one test, Y1).

**Item IDs are `Y1…Y15`**, local to this plan. `Y` appears in no commit
message or document of either repository.

Like every earlier review plan this file is scratch: delete it once the last
step lands; anything left undone moves to `docs/open-threads.md` (gossms) or
`~/go/gosmo/OPEN-THREADS.md` (gosmo). Close the open-thread entry each item
came from in the same change.

---

## Excluded — not fixable with the current estate

Left in the open-thread files, not planned here: V2/V3/V4/V6 (need Azure SQL
Database or the Managed Instance, unavailable since 2026-10-08), V5 (needs a
Standard/Express instance), V8's Azure/MI/Linux/2019–2022 runs, Resource
Governor affinity beyond processor group 0 (needs a multi-group host), backup
`TO URL` (needs a SAS credential), keys `FROM PROVIDER` (needs an EKM DLL),
Entra users/logins (needs a directory principal), external tables (needs
PolyBase), and the azidentity ROPC removal watch. Also excluded as
settled/by design: the DDL-trigger twin files, schedules by id, the disabled
clustered index replay, `ScriptTable`'s round trips, and the `float16`
dynamic-SQL heuristic.

---

## Priority 1 — `main` is red / data-correctness

### Y1 — `TestNoDanglingDocReference` fails on `main` (gossms) — ✅ DONE 2026-10-14

`docs/open-threads.md:66` (B24) cites the review plan deleted in `4eb0d3a`.
`go test ./internal/tui` fails on it today.

- Replace the pointer with the fact: "found while making forced drop/rename
  restore the prior access mode (`captureAccessMode`/`releaseAccessMode`,
  gosmo `server.go`)".
- While there, fix V8's stale last sentence: the follow's retry **is** tested
  (`TestFullTextFollowRetriesAFailedRead`, `internal/tui/fulltext_index_ops_test.go:285`).
- Verify: `go test ./internal/tui -run TestNoDanglingDocReference`.

### Y2 — A failed forced restore can leave a `RESTRICTED_USER` database `MULTI_USER` (gosmo, B24) — ✅ DONE 2026-10-14

`buildRestoreStatement` (`~/go/gosmo/restore.go:259`) ends with a hard-coded
`ALTER DATABASE … SET MULTI_USER` whenever the database is still online, so a
refused RESTRICTED_USER restore widens access.

1. **Probe first** (win10cli 17, a `gz_*` database): set RESTRICTED_USER,
   back up, set MULTI_USER, restore WITH REPLACE — does the restored database
   carry the backup's mode or reset? Repeat with the modes reversed. Record
   the answer in the fix's comment.
2. Reuse `captureAccessMode`/`releaseAccessMode` (`server.go:285`) in the
   restore batch: on failure always put back `@access`; on success follow
   step 1's answer (likely: leave what RESTORE produced unless it was
   SINGLE_USER from our own `CloseExistingConnections`).
3. Also cover the `batchCutShort` best-effort path (`restore.go:116-122`),
   which today also assumes MULTI_USER.
4. Tests: unit-pin the emitted batch; a live test that forces a refused
   RESTORE (bad backup set / `FILE = 99`) over a RESTRICTED_USER database and
   asserts the mode survives.
5. gossms: drive Restore Database with "Close existing connections" in tmux
   against a RESTRICTED_USER database, success and failure.

---

## Priority 2 — red or flaky live tests in gosmo

### Y3 — `TestLiveScriptDatabaseInDatabaseSettingsRoundTrip` fails on 17 — ✅ DONE 2026-10-14

The test, not gosmo: it read `[name].sys.database_scoped_configurations`
from master, and an ad-hoc plan keeps the database id it was compiled
against, so once a rerun's drops/creates reassign the ids the same text
reads the other database (reproduced by shifting ids with a pad database).
Read under `USE [n]` now — also in the file-layout, `ScriptedLoginKeepsAUserMapped`
and `ApplyPermissionEveryClassAndName` tests; rule added to gosmo `CLAUDE.md`.

The replay reads `ELEVATE_ONLINE=WHEN_SUPPORTED`, `IDENTITY_CACHE=0`,
`LEGACY_CARDINALITY_ESTIMATION=1`, `MAXDOP=3` (secondary 1),
`PARAMETER_SNIFFING=0` where the source reads defaults — inverted from a
missing statement.

1. Rerun at `5843caf`; dump both sides' `sys.database_scoped_configurations`.
2. Suspects in order: the test sets the values on the *replay* side or
   compares the wrong handle; the source database is created from a `model`
   carrying non-defaults left by an earlier test (check `model` on win10cli);
   `is_value_default` semantics vs `scopedConfigStatements`
   (`~/go/gosmo/scripter.go:745`).
3. Fix whichever it is; if it is leftover `model`/instance state, make the
   test reset it in `t.Cleanup`.

### Y4 — `TestLiveReplicationMonitor` gets no `SessionActions` rows on 17 — ✅ DONE 2026-10-14

Not the fixture: a gosmo bug. Every `sp_MSenum_*_sd` reads the session of the
newest history row at or before `@time`, and `SessionActions` passed a
non-merge session's `Start`; history cleanup keeps only an agent's last row,
so a session that lost its opening row read as empty (gossms's Replication
Monitor too). It now passes `End`; the test checks every session, not the
newest, and fails without the fix. The fixture change below was dropped —
fresh history would hide exactly this case.

`live_replication_monitor_test.go:150`. Likely the fixture agents have not run
or their history aged out of distribution.

- Make the fixture (`live_replication_fixture_test.go`) start the Log Reader
  and Distribution agent jobs and wait for one history row before asserting,
  rather than relying on history from a previous run.
- If agents fail, report their error from `MSlogreader_history` /
  `MSdistribution_history` in the test failure.

### Y5 — `TestLiveFullTextWrites` flakes: "population did not finish" — ✅ DONE 2026-10-14

Shared `liveFullTextWait` (2 min, catalog status + `dm_fts_index_population`
on timeout). A stop racing the crawl was ruled out live: under MANUAL
tracking STOP is refused and the FULL runs on. 10× clean on 17 and on 14.

Failed once in three runs. Make the wait poll
`FULLTEXTCATALOGPROPERTY(…,'PopulateStatus')` to idle with a longer, bounded
deadline, and on timeout print the status and item count so the next flake is
diagnosable. Run 10× on 17 and 14.

### Y6 — `TestLiveScriptDatabaseOptionsRoundTrip` flaked once on 14 with no diff — ✅ DONE 2026-10-14 (diagnostics)

The failure now names each differing field. 30× on 14 reproduced it once:
the *script* lacked `SET CHANGE_TRACKING` with the source's on. Not
reproduced in 60 more runs; moved to gosmo `OPEN-THREADS.md` with what was
ruled out, and the test re-reads change tracking when the script lacks it.

1/18 "replayed settings differ" with nothing captured. First make the failure
print the differing fields (source vs replay); then loop it 30× on 14. Likely
candidates: an `AUTO_CLOSE` timing read, or async stats/`AUTO_UPDATE_STATISTICS_ASYNC`
settling. Fix if it reproduces; otherwise ship the diagnostics and move the
note to `~/go/gosmo/OPEN-THREADS.md` with "diagnostics in place".

---

## Priority 3 — `ScriptDatabase` replays that are silently wrong (gosmo)

### Y7 — `scopedConfigNumeric` is a hand-kept list — ✅ DONE 2026-10-14

`DatabaseScopedConfig.ValueType`/`ValueForSecondaryType` (the sql_variant
base type, `bit`/`int`/`smallint`/`nvarchar` — the same on 13) decide the
spelling; the map is gone, no 2016 fallback needed. gossms's fake scoped-config
rows gained the two columns.

A new numeric option reporting 0/1 is written OFF/ON and fails the replay.
Read `SQL_VARIANT_PROPERTY(value, 'BaseType')` alongside `value` in
`DatabaseScopedConfigs` (exported field on `DatabaseScopedConfig`, version
sweep on 13); in `scopedConfigValue` treat a numeric base type as a number
unless the option is a known switch. Keep the map only as the 2016 fallback
if 2016 reports a different type. Unit tests per base type; the existing
live round trip covers it.

### Y8 — ADR `PERSISTENT_VERSION_STORE_FILEGROUP` not scripted

A source with the version store off PRIMARY replays it on PRIMARY. Probe on
17 where the setting is exposed (`sys.databases` has no column; check
`sys.dm_tran_persistent_version_store_stats.pvs_filegroup_id` and whether it
needs VIEW SERVER STATE). Emit `SET ACCELERATED_DATABASE_RECOVERY = ON
(PERSISTENT_VERSION_STORE_FILEGROUP = [fg])` when not PRIMARY, after the
filegroups exist; if the read needs VSS and it's missing, write a comment as
TDE does. Gate 2019+ (`colSince`). Extend
`TestLiveScriptDatabaseInDatabaseSettingsRoundTrip`.

### Y9 — TDE key left behind by `SET ENCRYPTION OFF` not scripted

The key is read only when `is_encrypted = 1`. When the caller has VIEW
SERVER STATE (already checked for the encrypted case), also read
`sys.dm_database_encryption_keys` when encryption is off and script `CREATE
DATABASE ENCRYPTION KEY` without `SET ENCRYPTION ON`. Without VSS, keep
today's behaviour (no key, no false comment). Live test: encrypt, decrypt,
script, replay, compare the key's algorithm and protector.

### Y10 — The in-database options read has no live test that fails without it

Add a live test that reads `Options` for an `AUTO_CLOSE` database that is
**closed** without first running the layout's file reads (call the options
read directly), asserting collation, `TARGET_RECOVERY_TIME`,
`DELAYED_DURABILITY` and ADR are non-NULL. Confirm it fails against the old
server-scope read before keeping it.

---

## Priority 4 — Extended properties outside tables/modules (gosmo)

`ScriptOptions.ExtendedProperties` covers tables, views, procedures,
functions, DML triggers and their children only. All five items share one
approach: generalise `objectExtendedProperties`
(`~/go/gosmo/scripter_extended_properties.go:61`) from `object_id` to
`(class, major_id, minor_id)` and reuse the existing `sp_addextendedproperty`
renderer with the right `@level0type/@level1type`. Do Y11 first — it builds
the generalisation; the rest are each a reader call plus a test. Each needs a
fixture/replay case in `live_script_extended_properties_test.go`, and the
gossms Script As menu needs no change (it already passes the option).

- **Y11** — database (class 0) and schema (class 3).
- **Y12** — database user and database role (class 4).
- **Y13** — alias type, table type (class 6), sequence, synonym (class 1
  objects without the module path), XML schema collection (class 10).
- **Y14** — partition function (class 20), partition scheme (class 21), DDL
  database trigger (class 1, `parent_class = 0`).
- **Y15** — standalone `CREATE INDEX`, `ScriptCheckConstraint`,
  `ScriptForeignKey`: emit the one object's own properties (minor id /
  constraint object id) as the table script already does for them.

After Y15, rewrite gosmo's open-thread "extended properties beyond tables and
modules" to list only what's still left (FileTable system constraints, by
design).

---

## Execution order

Seven steps; each is one commit (or one per item where noted). Steps 2–4
touch disjoint files and may be worked in parallel, but **never run two live
test packages against win10cli at once** — they share instance state
(`model`, Agent, full-text crawls), which is a suspect in Y3 and Y6.

| Step | Items | Together because | Depends on |
|---|---|---|---|
| 1 ✅ | **Y1** | Doc-only, gossms; turns `main` green before anything else lands | — |
| 2 ✅ | **Y2** | Alone: the only data-correctness bug; `restore.go`/`server.go`, own live probe, then gossms tmux drive | — |
| 3 ✅ | **Y4 + Y5** | Test-fixture hygiene only, no library code; separate files (`live_replication_monitor_test.go`, `live_replication_fixture_test.go`, `live_fulltext_write_test.go`) | — |
| 4 ✅ | **Y6 → Y3 + Y7** | All in `live_scriptdatabase_test.go` (`:302`, `:588`) and the scoped-config code (`scripter.go:728-790`). Y6's diff output first, since it is the diagnostic Y3 also needs; Y3's inverted values may well *be* Y7's numeric/switch mapping, so diagnose Y3 before writing Y7 | 1 |
| 5 | **Y8 + Y9 + Y10** | All extend `ScriptDatabase` and the same round-trip test; Y8 and Y9 are both VIEW SERVER STATE-gated DMV reads with the same comment-if-missing fallback; one `TestLiveVersionSweep` run on 13 covers both. Y10 is test-only and pins the options read before the others change around it — write it first in the step | 4 (green round-trip tests to extend) |
| 6 | **Y11** | Builds the `(class, major_id, minor_id)` generalisation the rest reuse | — (can start after 1, in parallel with 2–5) |
| 7 | **Y12 + Y13 + Y14 + Y15** | Each is a reader call + a replay case on Y11's base, all in `scripter_extended_properties.go` and `live_script_extended_properties_test.go`; one commit per item keeps them reviewable, one tmux Script As pass at the end covers all | 6 |

Critical path: 1 → 4 → 5. Step 2 is the highest-value fix and blocks
nothing. If time is short, stop after step 5: steps 6–7 are additive
fidelity with no wrong-replay risk to existing scripts.

---

## Verification for every item

Per `docs/testing.md`: unit + live tests in gosmo first, `TestLiveVersionSweep`
on win10cli\SQL2016 after any query change (Y7, Y8, Y9), then the gossms
binary driven in tmux for anything user-visible (Y2, Y8, Y9, Y11–Y15 via
Script As). `gofmt -w . && go vet ./... && go test ./...` green in both trees.
