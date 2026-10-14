# Review plan — 2026-10-13 (H series)

A bug review of both repositories on 2026-10-13, at gossms `fd3442f` / gosmo
`0ec2a46`. gosmo's backward compatibility is waived for this plan (H2 and H10
use that).

The method was different from the K/L passes: besides reading, a scratch Go
program scripted a fixture database of deliberately awkward objects with
gosmo's `Scripter`/`ServerScripter`, replayed the script into an empty
database, scripted that again and diffed the two, and compared the catalogs.
Every item below came out of that loop or a targeted live probe; all ten are
in gosmo and all ten reach the user through gossms (Script As, Object
Explorer rename, New Snapshot, View Dependencies).

**Item IDs are `H1…H10`**, local to this plan. `H` appears in no commit
message or document of either repository.

**Evidence.** Every item is *confirmed live* on win10cli (17.0) in throwaway
`gz_*` databases, logins and sessions, all dropped afterwards. The repro under
each item is the minimal one.

Like every earlier review plan this file is scratch: delete it once the last
step lands; anything left undone moves to `docs/open-threads.md`.

---

## What was checked and is clean

- `go vet`, `staticcheck` (all checks but style) and `go test -race ./...`:
  clean on both trees — no data race reported.
- Script → replay → rescript diff, clean for: alias and table types (keys,
  checks, inline indexes, defaults, identity, computed), partition functions
  (`nvarchar` with a quote, `datetime2` boundaries) and schemes, sequences
  (decimal, negative, alias-typed, tinyint), tables with identity
  `NOT FOR REPLICATION`, ROWGUIDCOL, persisted computed, per-column collation,
  sparse/column sets, rowversion, PK `DESC` with options, filtered and
  `INCLUDE` indexes, `IGNORE_DUP_KEY`, disabled indexes, untrusted and
  `NOT FOR REPLICATION` checks and FKs, `LOCK_ESCALATION`, table and
  per-partition compression (heap, inline PK, clustered columnstore), XML and
  spatial indexes, filtered NCCI, temporal (hidden period, history in another
  schema), ledger, node/edge with edge constraints, `TEXTIMAGE_ON`,
  extended properties with awkward names, schema-bound views with indexes,
  synonyms, security policies, logins, server roles, server audits and specs,
  XE sessions with quoted predicates, `ScriptDatabase` with most options, and
  DROP AND CREATE of 25 object kinds.
- Read and found sound in gossms: the GO splitter and separator rule,
  executor/money/variant rendering, CSV sink, editor search, peer dialling and
  close races, config naming, server-property seeding and affinity masks,
  User Mapping apply, restore source/set selection, Activity Monitor counter
  and file-I/O maths, version comparison, compatibility-level lists.

---

## Implementation order

Each step is one unit the user commits, gosmo first. At the end of every step
both repositories pass `gofmt -l`, `go vet ./...` (and `-tags livedb`),
`go test -race ./...`; gossms builds against the gosmo working tree; and the
step's live check has run (`docs/testing.md`). Live checks create their own
throwaway objects and drop them. Mark an item **Done** under its heading when
its step lands.

- **S1 — gosmo scripter fidelity:** H1, H2, H3, H4, H5, H6, H9. **Done** — uncommitted in both trees (gossms: test fixtures only). Live tests in gosmo `live_script_roundtrip_fidelity_test.go`; all six fail on `0ec2a46`.
- **S2 — gosmo operations:** H7, H8, H10.
- **S3 — gossms:** the follow-ups named under H1, H7, H10 (a confirm text,
  the dependencies grid), plus a tmux pass over Script As on a renamed
  procedure and View Dependencies on a procedure with an unqualified `EXEC`.

---

## H1 — A renamed or transferred module scripts under its old name

**Done (S1).** `renameModuleHeader` (`scripter_module.go`), applied before the ALTER rewrite for T-SQL and CLR. A one-part header name is taken to resolve to `dbo` (a trigger's to its table's schema); a matching header is left byte for byte. Unit table in `scripter_verb_test.go`, a `FuzzNames` invariant, live `TestLiveRoundTripRenamedModules`.

**Severity: high.** `writeModuleDefinition` (`~/go/gosmo/scripter_module.go`)
writes `sys.sql_modules.definition` verbatim (rewriting only CREATE → ALTER).
`sp_rename` and `ALTER SCHEMA … TRANSFER` change the catalog but never the
stored text, and gossms offers both (Rename, Move to Schema).

Repro: `CREATE PROCEDURE dbo.oldp AS SELECT 1; EXEC sp_rename 'dbo.oldp','newp'`;
`ScriptStoredProcedure(ctx,"dbo","newp")` gives `CREATE PROCEDURE dbo.oldp`
and, as ALTER, `ALTER PROCEDURE dbo.oldp`. Script As ALTER then fails (no such
object) or — once someone has created a new `oldp` — alters the wrong one.
DROP AND CREATE drops `newp` and creates `oldp`. SMO rewrites the header name
for exactly this reason.

Fix: after `leadingTriviaEnd` and `createKeyword`, lex the module keyword
(`PROC`/`PROCEDURE`/`VIEW`/`FUNCTION`/`TRIGGER`) and the one-to-three-part
name after it with the same code-span walk, and replace that name with
`qualifiedName(schema, name)` when it does not resolve to the current
schema/name. Apply to CREATE and ALTER, T-SQL and CLR alike; leave the text
untouched when the header cannot be parsed (as `alterModuleDefinition` does).
A trigger's `ON <table>` is not touched. Tests: rename, transfer, bracketed
and quoted names, a comment between keyword and name, `CREATE OR ALTER`, a
two-part vs one-part original; live round trip of a renamed proc, view,
function and trigger.

gossms: nothing beyond the tmux check (S3).

## H2 — An index on a different data space than its table loses its ON clause

**Done (S1).** `indexDataSpaceClause(ds, tableDS)` (`scripter_index.go`) on every index/key-constraint site, inline ones included; `ScriptIndex` now reads `Table.DataSpace`, and a view's indexes compare against its clustered index. It keeps today's explicit `ON [fg]` where the index shares a non-default filegroup with its table (adds ON where it was missing, never removes one). Live `TestLiveRoundTripIndexDataSpace`.

**Severity: high (silent).** `dataSpaceClause` (`~/go/gosmo/scripter_index.go`)
omits ON for the default filegroup, on the premise that an omitted ON means
the default filegroup. For CREATE INDEX — and a constraint inside CREATE
TABLE — it means *the table's* data space.

Repro 1: table on partition scheme `ps_s`, `CREATE INDEX nx ON t(id) ON [PRIMARY]`
(non-aligned). Scripted without ON; replayed, `nx` is on `ps_s` (aligned) —
confirmed in `sys.indexes`/`sys.data_spaces`. Repro 2: table on `[fg 2]`,
index `ON [PRIMARY]`: replays on `[fg 2]`.

Fix: render ON for an index when its data space differs from its table's
(`Table.DataSpace` / the heap-or-clustered `DataSpace`), even when the index's
is the default filegroup; omit it only when the two are equal. The clustered
index *is* the table's data space and keeps today's rule. Check the PK/UQ
constraint path in `scripter_table.go` uses the same comparison. Tests: the
two repros plus the unchanged cases (aligned index, everything on PRIMARY);
live replay compares `data_space_id` names.

## H3 — A masking function's quotes double on every round trip

**Done (S1).** `scanColumns` unescapes `''`; field doc updated. `masking_function_read_test.go`, live `TestLiveRoundTripMaskingFunctionQuotes` (two round trips, masked output compared as a non-UNMASK user).

**Severity: medium (silent behaviour change).** `sys.masked_columns.masking_function`
already stores the function with its quotes escaped: created with padding
`"X'X"`, the catalog reads `partial(1, "X''X", 2)`. `scripter_table.go`
escapes it again (`escapeSingle(col.MaskingFunction)`), so the replay masks
with `X''X` — confirmed by selecting as a user without UNMASK (`aX'Xgh`
before, `aX''Xgh` after) — and each further round trip doubles again.

Fix (compat waived): unescape `''` → `'` when reading `Column.MaskingFunction`
so the field holds the function as written, and keep the scripter's
`escapeSingle`. Document the field accordingly. Tests: read/script unit test
on a quoted padding; live round trip asserting the masked output is
unchanged. gossms shows the field raw nowhere today; nothing to change there.

## H4 — Database and server DDL triggers are scripted without their SET options

**Done (S1).** New `UsesANSINulls`, `UsesQuotedIdentifier`, `IsCLR` on `DatabaseTrigger`/`ServerTrigger`; SET options emitted unless `IsCLR`. Live `TestLiveRoundTripDDLTriggerSetOptions` (both scopes).

**Severity: medium.** `buildDatabaseTriggerScript` (`scripter_module.go`) and
`buildServerTriggerScript` (`scripter_server.go`) write the definition with no
`SET ANSI_NULLS`/`SET QUOTED_IDENTIFIER`, unlike every other module
(`moduleSetOptions`). A DDL trigger created under `QUOTED_IDENTIFIER OFF`
that uses a "string" replays under gossms's ON and fails.

Repro: `SET QUOTED_IDENTIFIER OFF; CREATE TRIGGER gz_qi ON DATABASE FOR CREATE_VIEW AS PRINT "view created"`;
replaying `ScriptDatabaseTrigger`'s output fails Msg 128.

Fix: read `uses_ansi_nulls`/`uses_quoted_identifier` (`sys.sql_modules`,
`sys.server_sql_modules`) onto `DatabaseTrigger`/`ServerTrigger` and emit
`moduleSetOptions` before the definition (not for CLR triggers, as
`writeModuleDefinition` does). Tests: build-function tests; live repro above
for both scopes.

## H5 — A column-bound rule or default is lost from the table script

**Done (S1).** `Column.BoundRule[Schema]`/`BoundDefault[Schema]` (NULLIF the type's binding, default constraints excluded), `columnBindings` after CREATE TABLE. Version-gate golden `columns.sql` regenerated. gossms fake column rows widened to 41. Live `TestLiveRoundTripBoundRulesAndDefaults`.

**Severity: medium.** A legacy default/rule bound to a column with
`sp_bindefault`/`sp_bindrule` (`sys.columns.default_object_id` pointing at a
`D` object with no parent, `rule_object_id`) is not scripted:
`ScriptTable` on such a table emits the bare column, so the replay accepts
values the original rejects and fills no default. Types already script their
bindings (`buildUserDefinedDataTypeScript`); columns do not.

Fix: read the bound default and rule (schema-qualified, see H6) per column,
excluding the column-default constraint case, and append
`EXEC sp_bindefault N'[s].[d]', N'[s].[t].[c]'` / `sp_bindrule` after the
CREATE TABLE. Skip a binding inherited from the column's alias type (SQL
Server binds it with the type; `sys.columns` shows the same id as
`sys.types`) so it isn't bound twice. Tests: unit + live replay comparing
`default_object_id`/`rule_object_id`.

## H6 — An alias type's bound rule or default loses its schema

**Done (S1).** New `UserDefinedDataType.RuleSchema`/`DefaultSchema` (names kept, so gossms's display is unchanged), bind emitted qualified. Same live test as H5, with a same-named `dbo` rule as the decoy.

**Severity: medium-low.** `userDefinedDataTypeSelect`
(`~/go/gosmo/user_defined_type.go`) reads the binding with `OBJECT_NAME()`,
dropping the schema, and the scripter emits `sp_bindrule N'[r_hist]', …`.
Repro: rule `hist.r_hist` bound to `dbo.small` — the script's
`sp_bindrule` fails Msg 15017 ("The rule '[r_hist]' does not exist"), or
binds a same-named `dbo` rule instead.

Fix: read `OBJECT_SCHEMA_NAME` too (new `RuleSchema`/`DefaultSchema` fields,
or one qualified string — compat waived) and emit
`qualifiedName(schema, name)`. Shares its reader with H5.

## H7 — Forced rename, drop or detach leaves the database MULTI_USER

**Severity: medium.** `renameExclusiveBatch` and `exclusiveBatch`
(`~/go/gosmo/server.go`) take `SINGLE_USER WITH ROLLBACK IMMEDIATE` and
release with a hard-coded `SET MULTI_USER` — on success (rename) and in CATCH
(a refused rename, drop or detach). A database the DBA left `RESTRICTED_USER`
(or `SINGLE_USER`) comes back `MULTI_USER`. gossms always renames with
`force = true` (`explorer_object_ops.go`), so every Object Explorer rename of
such a database opens it to everyone. The `batchCutShort` comment already
states the intended contract for the refused case.

Repro: `CREATE DATABASE gz_rn; ALTER DATABASE gz_rn SET RESTRICTED_USER;`
then `DatabaseRef("gz_rn").Rename(ctx, "gz_rn2", true)` →
`user_access_desc = MULTI_USER`.

Fix: capture `user_access_desc` into a variable at the top of the batch and
release to that mode (by name on the right side of the rename) instead of
`MULTI_USER`; `restoreMultiUserAfterRename`'s client-side repair needs the
same original value (read it before the batch, or have the batch select it
first). Tests: batch-text tests; live: RESTRICTED_USER and SINGLE_USER
databases renamed (success), renamed onto a taken name (refused), and a
refused forced drop (database with a snapshot) keep their mode.

## H8 — Default snapshot file names collide

**Severity: medium.** `snapshotFilePath` (`~/go/gosmo/database_snapshot.go`)
derives `<dir><stem>_<snapshot>.ss` by stripping the extension, so two data
files in one folder that differ only by extension (`gz_dst` with the
extensions `mdf` and `ndf`) both map to `gz_dst_gz_snap.ss`. `CreateDatabaseSnapshot` then
fails "Cannot create file … because it already exists"; gossms's New Snapshot
dialog fills its paths from `SnapshotFileDefaults` and hits the same.

Fix: in `SnapshotFileDefaults`, detect a repeat (compared case-insensitively —
Windows paths) and disambiguate, e.g. keep the extension in the stem
(`gz_dst_ndf_gz_snap.ss`) or append the logical file name sanitised to
filename characters. Unit test with two same-stem files and with mixed case;
live create/drop of the repro snapshot.

## H9 — SELECT To on a graph table selects internal graph columns

**Done (S1).** `buildSelectScript` skips graph types 1, 3, 4, 6, 7. Live `TestLiveRoundTripGraphSelect`.

**Severity: medium.** `buildSelectScript` (`~/go/gosmo/scripter_dml.go`)
lists every column `ObjectColumns` returns, including the hidden internal
ones (`graph_id_…`, `from_obj_id_…`, `from_id_…`, `to_obj_id_…`, `to_id_…`;
`graph_type` 1, 3, 4, 6, 7). Running the script fails Msg 13908 "Cannot
access internal graph column" for every node and edge table.

Fix: skip those graph types in SELECT (keep the `$node_id`/`$edge_id`/
`$from_id`/`$to_id` computed columns, types 2, 5, 8). Insert/Update already
filter. Test: extend `scripter_dml_test.go`'s graph fixture to SELECT; live
run of the SELECT on a node and an edge table.

## H10 — View Dependencies misses unqualified references

**Severity: low-medium.** `Database.Dependencies` (`~/go/gosmo/dependency.go`)
joins `sys.sql_expression_dependencies.referenced_id` to `sys.objects`. A
caller-dependent reference — `EXEC dep_b` without a schema — is stored with
`referenced_id` NULL, so it is dropped. `Dependents` was fixed for this in the
other direction (its doc comment explains it); `Dependencies` was not.

Repro: `dbo.dep_a AS EXEC dep_b; SELECT * FROM dbo.parent` — the row for
`dep_b` exists (`is_caller_dependent = 1`) but only `parent` is returned.

Fix: resolve a NULL `referenced_id` with `referenced_schema_name IS NULL` by
name — the referencing object's schema first, then `dbo` (the resolution an
owner calling the module gets) — and add `Dependency.IsCallerDependent` so
the caller can say the match is by name. gossms: show that flag in the
dependencies grid (`properties_dialog.go`). Tests: live repro; a reference to
a missing object still returns nothing rather than an error.
