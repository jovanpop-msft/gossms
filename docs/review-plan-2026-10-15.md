# Review plan — 2026-10-15 (Z series)

A review of both repositories for bugs, inconsistencies, optimisations,
simplifications and architecture, at gossms `c31f484` / gosmo `852c833`. The
author waived gosmo backward compatibility for this pass, so items that break
gosmo's API are allowed. Each one names its gossms call sites.

**Item IDs are `Z1…Z11`** and belong to this plan only. `Z` does not appear
in any commit message or document of either repository.

Like every earlier review plan this file is scratch. Delete it once the last
step lands. Anything left undone moves to `docs/open-threads.md` (gossms) or
`~/go/gosmo/OPEN-THREADS.md` (gosmo).

---

## What was run, and came back clean

- `go build ./...`, `go vet ./...` (gosmo also `-tags livedb`), `staticcheck
  ./...` (gosmo also `-tags livedb`), `gofmt -l .`, `go mod tidy -diff`: clean
  on both repositories.
- `go test -race -count=1 ./...`: green on both, with no race reports.
- `govulncheck ./...`: no vulnerabilities reachable from either repository.
  The one module-level hit (GO-2026-5932, `x/crypto/openpgp`) is in a
  package neither repository imports.
- `deadcode ./cmd/gossms`: only the documented keepers (`docs/decisions.md`
  § By design) and `sqlparse.NarrowToDMLStatement`, which is a test oracle
  and says so in its doc comment.
- `dupl -t 80` (non-test code): 29 small clone groups in gossms and 14 in
  gosmo. Most are parallel per-family code (see § Considered, not planned).
- Conventions checked by grep: no `context.Background()` on a connection path
  outside the documented exceptions; no direct `database/sql` use outside
  `internal/query` and `internal/activity/proc.go`; no `time.Local` in gosmo;
  `runtime.GOOS` only where `CLAUDE.md` says it is; every un-`%w`
  `fmt.Errorf` in gosmo is on `invalid_request_test.go`'s allowlist; no
  `latest.Begin` without a matching `Done`/`Current`.

Both trees are in good shape. Most findings below are simplification and
API work, not defects.

---

## Priority 1 — defects

### Z1 — `unwrapPredicate` counts parentheses inside quoted text (gosmo) — **DONE**

Done 2026-10-15: `unwrapPredicate` counts depth over `scriptCodeSpans` only.
That surfaced a second bug, fixed with it: `scriptCodeSpans` ended a
`[…]` identifier at the first `]` of an escaped `]]`, so `[a]]'b]` read
`'b]…` as a literal opening. Rows added to `TestUnwrapPredicate` and
`TestBindScriptArgsSkipsLiteralsAndComments` (both mutation-checked); new
`live_unwrap_predicate_test.go` replays a `[fn)x](…, N')')` policy on 17 and
fails without the fix. `FuzzNames` 60 s clean.

`~/go/gosmo/scripter_security.go` `unwrapPredicate` strips the outer
parentheses that `sys.security_predicates` puts around a predicate. It
counts every `(` and `)` in the definition, including those inside a string
literal or a bracketed identifier. A predicate such as `([sec].[fn]([c],
N')'))`, or a function named `[fn)x]`, reaches depth 0 before its last
character. The function then returns the definition still wrapped, and the
scripted `ADD FILTER PREDICATE` fails with "Incorrect syntax near '('". That
failure is the one the function exists to prevent.

1. Count depth only in code spans: walk with `scriptCodeSpans`
   (`~/go/gosmo/script.go`), which already skips literals, both
   quoted-identifier forms and comments. Z4 later replaces this walk with the
   shared scanner.
2. Add these rows to the table test in
   `~/go/gosmo/scripter_storage_test.go` (the one that calls
   `unwrapPredicate`): a `)` inside `N'…'`, inside `[…]`, and inside a
   `/* */` comment. Mutation-check: revert step 1 and confirm the rows fail.
3. Live (17): create a security policy whose predicate passes a `')'`
   literal, script it, replay the script into a scratch database, and drop
   both.

---

## Priority 2 — inconsistencies

### Z2 — `CLAUDE.md` pins tcell at `v3.5.0`, but `go.mod` requires `v3.6.0` (gossms) — **DONE**

Done 2026-10-15: version dropped from `CLAUDE.md`; no other pin found in
`ARCHITECTURE.md`, `internal/tuikit/README.md`, `README.md` or `docs/`.

`CLAUDE.md` § What this is says `github.com/gdamore/tcell/v3` (`v3.5.0`).
Drop the version from `CLAUDE.md`, since `go.mod` is the authority and a
copy of it drifts. If a version stays, make it `v3.6.0`. Check
`internal/tuikit/README.md` and `ARCHITECTURE.md` for the same pin (none was
found today).

### Z3 — gossms re-derives which securable classes are schema-scoped (gosmo + gossms) — **DONE**

Done 2026-10-15: gosmo exports `SecurableClass.SchemaScoped()` (now beside
`HasColumns`); `label()` is database → `(database)`, `SchemaScoped()` →
`[schema].[name]`, else `[name]`. New `TestSecurableLabelCoversEveryClass`
(gossms) and `TestSchemaScopedAgreesWithOnClause` (gosmo, mutation-checked by
dropping `SecurableUserType` from the switch).

`securable.label()` (`internal/tui/securables_matrix.go`) switches on
`SecurableDatabase`, then on `SecurableSchema`/`SecurableCertificate`, and
treats every other class as schema-qualified. gosmo already holds the real
answer in the unexported `SecurableClass.schemaScoped()`
(`~/go/gosmo/permission_options.go`). gosmo's `PermissionsForPrincipal` doc
lists the classes still to come: assembly, asymmetric key, XML schema
collection and Service Broker objects. The first of those to gain a
`SecurableClass` would render as `[].[name]` on the Securables page, and no
test would notice.

1. gosmo: export the method as `SchemaScoped()` and keep its doc beside
   `HasColumns`. This adds API and breaks nothing.
2. gossms: `label()` becomes database → `(database)`, `SchemaScoped()` →
   `[schema].[name]`, anything else → `[name]`.
3. Test: in gossms, one `label()` table over every `SecurableClass` constant.
   In gosmo, `TestEverySecurableClassIsReadAndSearched` already lists them;
   add a check that `schemaScoped` agrees with `Securable.on()`'s qualified
   arms.

---

## Priority 3 — simplifications in gosmo

### Z4 — One T-SQL lexical walker instead of four (gosmo) — **DONE**

Done 2026-10-15: new `tsql_scan.go` — `tsqlSpans(s) iter.Seq[tsqlSpan]`
(code, line/block comment, literal, N-literal, bracket/quoted identifier;
`closed` false for an unterminated span) plus `unescape`, `literal()`,
`quoted()`, `comment()`, with `isIdentByte` moved beside it. All four sites
ported, and every `scriptCodeSpans` consumer with them: `redactSecrets`,
`bindScriptArgs`, `usesFloat16Vector`/`buildsFloat16Vector` (shared
`codeOnly`), `stringLiterals` (no longer re-joins `''`-split spans),
`leadingTriviaEnd`, `nextHeaderToken`, `unwrapPredicate` and
`comment_safe_test.go`. `scriptCodeSpans`, `codeSpan` and `scanQuoted` are
gone. Unterminated `--` settled as a comment to end of text (in the scanner's
doc); new `TestRenameModuleHeader` rows pin it and the `]]` escape. New
`TestTSQLSpans`/`TestTSQLSpanUnescape`, and `FuzzTSQLSpans`, which checks the
spans tile the input and agree byte-for-byte with the old `scriptCodeSpans`
walk and value-for-value with the old `scanQuoted` literal walk (both kept in
the test as the oracle): 60 s clean, `FuzzNames` 60 s clean. Five scanner
mutants (no `]]` escape, no nesting, no N prefix, N inside a word, unescape
keeping doubles) all killed by `Test*` alone. `go test ./...`, `go vet -tags
livedb`, staticcheck (both tag sets) clean; gossms builds; 16 live scripting
tests on 17 (float16, unwrap predicate, credential, fidelity, round trip)
pass.

Four hand-written scanners each re-implement nested `/* */` comments and the
three quoted forms:

| Site | What it scans for |
|---|---|
| `scriptCodeSpans` (`script.go`) | `@pN` placeholders outside literals and comments; also behind `leadingTriviaEnd`, `usesFloat16Vector`, `buildsFloat16Vector`, `stringLiterals` |
| `redactSecrets` + `scanQuoted` (`script.go`) | `'…'`/`N'…'` literals equal to a secret |
| `nextHeaderToken` (`scripter_module.go`) | the `CREATE … <name>` header for `renameModuleHeader` |
| `unwrapPredicate` (`scripter_security.go`) | parenthesis depth (Z1) |

The four already disagree. `nextHeaderToken` treats a `--` comment that
reaches end of text without a newline as untrustworthy and returns
`ok=false`, while `scriptCodeSpans` and `redactSecrets` treat it as a comment
that runs to the end. A fix to one rule, such as nested comments, has to be
made four times.

1. Add one unexported scanner in a new `tsql_scan.go`: an iterator over spans
   `{Kind: code|lineComment|blockComment|literal|nliteral|bracketIdent|quotedIdent, Start, End}`
   plus an `Unescape(span)` helper.
2. Port the four sites onto it one at a time, each in its own change with its
   existing tests green. Settle the unterminated-`--` rule once (comment to
   end of text) and record it in the scanner's doc.
3. Run `FuzzNames` for 60 s after each port (gosmo `CLAUDE.md` § Build &
   verify names `bindScriptArgs`/`alterModuleDefinition` as fuzz triggers).
4. Add a fuzz target that compares the new scanner's literal spans with
   `scanQuoted`'s on random input before `scanQuoted` is deleted.

Do Z1 first and keep its tests: they become this item's regression cases.

### Z5 — `Enable`/`Disable`/`SetEnabled`/`setEnabled`: four functions where three do (gosmo) — **DONE**

Done 2026-10-15: on all eight families `SetEnabled` now holds the body and
`Enable`/`Disable` (and the audit specifications' internal
disable/restore/drop paths) call it; `setEnabled` is gone. The trigger twins
stay twins; their comments and `~/go/gosmo/OPEN-THREADS.md` now say `SetEnabled`.
`set_enabled_test.go` and the rest of `go test ./...` green; `go vet -tags
livedb` clean; gossms builds.

Eight families (`Job`, `Schedule`, `Alert`, `Operator`, `DatabaseTrigger`,
`ServerTrigger`, `ServerAuditSpecification`, `DatabaseAuditSpecification`)
have an exported `SetEnabled(ctx, on)` whose whole body is a call to an
unexported `setEnabled(ctx, on)`, which `Enable`/`Disable` also call. Make
`SetEnabled` the implementation, have `Enable`/`Disable` call it, and delete
`setEnabled`. This changes no API. Keep `database_trigger.go` and
`server_trigger.go` twins as `~/go/gosmo/OPEN-THREADS.md` § "The two DDL-trigger files
stay near-identical" requires, and update its "Enable/Disable/setEnabled
block" wording in the same change.

### Z6 — Positional `bool` parameters on exported writes (gosmo, **breaking**) — **DONE**

Done 2026-10-15: every row below except `SetReadOnly` takes an `int` mode
type, so a bare `true`/`false` no longer compiles and an unknown value is
refused as `ErrInvalidRequest` before anything is sent. `Database.Drop`/`Rename`
reuse `Termination` (its doc now covers DROP and MODIFY NAME); the new types
are `TableDropMode` (`DropTableOnly`/`DropTableCascade`), `DeviceFileMode`
(`KeepDeviceFile`/`DeleteDeviceFile`), `FullTextRepopulation`
(`FullTextRepopulate`/`FullTextNoPopulation`), `SignatureKind`
(`PlainSignature`/`CounterSignature`), `ReconfigureMode`
(`ReconfigureChecked`/`ReconfigureOverride`), `ScopedConfigReplica`
(`ScopedConfigPrimary`/`ScopedConfigSecondary`, also through
`buildScopedConfigStatement` and the scripter), `MailProfileDefault`
(`MailNotDefaultProfile`/`MailDefaultProfile`), `SessionScope`
(`UserSessionsOnly`/`AllSessions`) and `ReplSessionFilter`
(`AllReplSessions`/`FailedReplSessions`). Beyond the table:
`MasterKey.Regenerate`'s `force` (same shape, called from
`master_key_dialogs.go`) is `MasterKeyRegenerateMode`
(`MasterKeyRegenerate`/`MasterKeyForceRegenerate`). Left as they are, being
setters the name explains: `SetChangeTracking(enable, trackColumns)`,
`Login.SetPasswordPolicy`, `SetDBFailover`, `SetDTCSupport`,
`ConnectionString(maskSecrets)`. New `mode_args_test.go` (every type refuses
an unknown value with nothing sent; `Reconfigure`, `ActiveSessions` and
`Sessions` had no offline statement test and now do); 13 mutants killed, two
equivalent ones (a `!=` for an `==` behind the validation) survive.
gosmo `go test ./...`, `go vet` (both tag sets), staticcheck (both) clean;
`ARCHITECTURE.md` rows updated. gossms call sites updated; `go vet`, `go test
./...` green. The `CHANGELOG.md` break note is still to write at release.

At a call site `x.Drop(ctx, true)` or `ref.AddColumn(ctx, spec, false)` does
not say what the `bool` means. The exported methods with this shape:

| Method | Bool | gossms call sites |
|---|---|---|
| `Database.Drop(ctx, force)` | close connections first | `explorer_object_ops.go` (`dbOf(sc, n).Drop(ctx, true)`) |
| `Database.Rename(ctx, newName, force)` | same | `explorer_object_ops.go` (`Rename(ctx, newName, true)`) |
| `Table.Drop(ctx, cascade)` | drop referencing FKs | `explorer_object_ops.go` (from the Delete dialog's cascade checkbox) |
| `BackupDevice.Drop(ctx, deleteFile)` | delete the media file | `explorer_object_ops.go` |
| `FullTextIndex.AddColumn/DropColumn(…, noPopulation)` | `WITH NO POPULATION` | `fulltext_index_props.go` (both) |
| `StoredProcedure`/`UserDefinedFunction`/`Trigger` `.AddSignature/DropSignature(ctx, by, counter)` | countersignature | `key_signatures_page.go` (through its own interface) |
| `Server.Reconfigure(ctx, override)` | `WITH OVERRIDE` | none in gossms (Server Properties applies through its own path) |
| `Database.SetDatabaseScopedConfig(ctx, name, value, forSecondary)` | `FOR SECONDARY` | `database_props_scoped_config.go` (two) |
| `Database.SetReadOnly(ctx, readOnly, term)` | — (already paired with a `Termination`) | keep as is |
| `MailProfile.Grant/SetGrantDefault(ctx, principal, isDefault)` | default profile | `database_mail_props_profiles.go` |
| `Server.ActiveSessions(ctx, includeSystem)`, `ReplicationAgent.Sessions(ctx, hours, errorsOnly)` | read filters | `login_props.go`, `detach_database_dialog.go`, `replication_monitor_panel_load.go` |

Replace each `bool` with a named type and constants, such as `type
DropMode int` with `DropOnly`/`DropForce`, or `Cascade`, `DeleteFile`,
`NoPopulation`, `Countersign`. Do not use an options struct: one flag does
not need one. `SetEnabled(ctx, on bool)` keeps its `bool`, which the method
name explains.

1. gosmo: change the signatures, update the doc comments, and update the
   `script_*_write_test.go` / `drop_rename_test.go` rows that call them. Then
   build with `-tags livedb`, which compiles the live tests the rename touches
   (gosmo `CLAUDE.md` § Build & verify).
2. gossms: update the call sites above. `go build ./...` finds every one.
3. Note the break in gosmo's `CHANGELOG.md` at release time. Per `CLAUDE.md`,
   do not edit it now.

### Z7 — Hand-written `N'…'` in the Extended Events export (gossms) — **DONE**

Done 2026-10-15: `insertScript` uses `gosmo.QuoteLiteral` for text and
`gosmo.QuoteAnsiLiteral` for the `datetime2(6)` arm. `TestXEventInsertScript`
already pins `N'it''s'`.

`internal/tui/xevent_viewer_export.go` builds `N'` +
`strings.ReplaceAll(v, "'", "''")` + `'` by hand. `gosmo.QuoteLiteral`
produces the same text, and the package already imports gosmo. Use it. The
`datetime2(6)` arm's `'…'` stays: it is a non-Unicode literal, which is
`gosmo.QuoteAnsiLiteral`, so use that too. Pin the output with the existing
export test, or add one that holds a `'` in a value.

---

## Priority 4 — simplifications in gossms

### Z8 — `" (Disabled)"` label suffix written out at ten sites — **DONE**

Done 2026-10-15: `disabledLabel` in `explorer_loaders.go`, used at all ten
sites (the Resource Governor root included); the literal now appears only in
the helper. Mutation-checked: 9 sites were already covered by loader tests; new
`TestSecurityPoliciesLoaderLabelsTheDisabledOnes` covers the tenth.

`label += " (Disabled)"` appears 10 times across `explorer_databases.go` (3),
`explorer_security.go` (3), `explorer_management.go`,
`explorer_resource_governor.go`, `explorer_programmability.go` and
`explorer_service_broker.go`. Add `disabledLabel(name string, enabled bool)
string` to `explorer_loaders.go` beside the other shared loader helpers, and
use it at all ten sites. The existing explorer label tests cover the change.
Grep for the literal afterwards and confirm zero hits outside the helper.

---

## Priority 5 — architecture (optional, breaking)

### Z9 — A stdlib-only `gosmo/tsql` leaf package — **DONE**

Done 2026-10-15: new package `github.com/radix29/gosmo/tsql`, stdlib only
(`TestImportsOnlyStdlib` parses its imports): `QuoteName`, `QuoteLiteral`,
`QuoteAnsiLiteral`, `UnquoteName` (`quote.go`), `CommentSafe`,
`BlockCommentSafe` (`comment.go`) and Z4's scanner as `Spans`/`Span`/
`SpanKind` with `IsQuoted`/`IsLiteral`/`IsComment`/`Unescape` (`scan.go`).
The root's four quoting functions stay as one-line forwards, so gosmo's API
does not break and no gossms call site changed; `quoting.go` no longer
imports the driver. `tsql_scan.go` and the root `commentSafe` pair are gone,
every consumer calls `tsql.*`. The driver's two `TSQLQuoter` rules are
written out and pinned by root `TestTSQLQuoterParity` (a no-doubling mutant
fails it). Tests moved with the code (`TestSpans`, `TestSpanUnescape`,
`FuzzSpans`, `TestBlockCommentSafe`, `TestUnquoteName`). gossms:
`internal/showplan` drops `bracket()` *and* `unbracket()` (it mirrored
`UnquoteName`) for `tsql.QuoteName`/`tsql.UnquoteName`, and still links no
driver (`go list -deps`). `QuoteNameIfNeeded`, `IsReservedKeyword` and
`SplitName` stayed in the root (`SplitName` returns gosmo's
invalid-request error; the live keyword test reads the root's set). Docs:
gosmo `CLAUDE.md`, `ARCHITECTURE.md`, `README.md`; gossms
`docs/db-rules.md`. gosmo `go test ./...`, `go vet` (both tag sets),
staticcheck (both tag sets), `FuzzSpans` and `FuzzNames` 60 s each clean;
gossms `go build`/`vet`/`test ./...` clean.

gosmo is one 58k-line package: 153 files, 455 exported types and about 1,000
exported methods. Any import of it pulls in go-mssqldb and azidentity. gossms
already works around that. `internal/showplan/missing_index.go` has its own
`bracket()` because importing `gosmo.QuoteName` would "keep this package free
of the mssql driver" only if it did not import gosmo.

Proposal: move the pure T-SQL text layer into a leaf sub-package that imports
only the standard library: `QuoteName`, `QuoteLiteral`, `QuoteAnsiLiteral`,
the `commentSafe` pair and Z4's scanner. The root package keeps calling it.
`QuoteName`/`QuoteLiteral` either move (breaking: gossms call sites become
`tsql.QuoteName`) or stay as one-line forwards. `QuoteLiteral` uses
`mssql.TSQLQuoter` today, so moving it means reproducing that quoter's two
rules in the leaf package and pinning them against the driver in a test.

A larger split (agent, xevent, replication and dbmail sub-packages) was
considered and not proposed. Each family reaches the unexported `query`,
`exec`, `useBatch` and `createdObject` chokepoints, so a split would need an
exported internal core. That would be a second API surface to keep stable,
for little gain to the main consumer.

Do this after Z4, or not at all. It is worth it only if a third
driver-free consumer appears. Today the only gain is deleting `bracket()`.

### Z10 — Ctrl+click after Ctrl+A on a large grid fills a map with every row (gossms) — **DONE**

Done 2026-10-15: `markedRows` is now `markLo..markHi` (the run the first
Ctrl+click folds in) XOR `markFlips` (the Ctrl+clicked rows); `rowMarked`,
`selectedRows` (sorts only the flips and merges them with the run) and a new
`selectedRowCount` (the context menu's Show Value gate, which no longer builds
the slice) read it. There is no grid Ctrl+A. The large selection comes from
a click, a scrollbar drag and a Shift+click. New benchmarks in
`datagrid_bench_test.go` at 1M rows: `markRow` ~300 ms / 84 MB → ~0.3 µs /
192 B; `selectedRows` ~270 ms → ~5.7 ms (the 8 MB result slice remains). New
`TestCtrlClickUnpicksAndRepicksInsideAShiftSelectedRun`. 7 mutants: 6 killed,
1 equivalent (the empty-run fallback in `markRow`, unreachable while
`selRow` ≥ 0). `datagrid_reset_paths_test.go` used to set `markedRows` without
`marking`, so its reset check was inert. It now marks through `markRow` and
asserts the setup. Live on win10cli: `SELECT TOP 1000000` cross join, Shift
run 1…1,000,000, then Ctrl+clicks unpick and re-pick rows inside it at once.
The menu shows Copy without Show Value. `go vet`, `go test ./...` green.


`DataGrid.markRow` (`internal/tuikit/controls/datagrid.go`) folds the
selection in force into `markedRows` the first time a Ctrl+click arrives.
After Ctrl+A on a million-row result, that is a million-entry map built on
the UI goroutine (tens of MB, a visible stall), and `selectedRows()` then
sorts it on every copy. Represent a discontiguous selection as the base
range plus a set of toggled rows (XOR), and make `selectedRows` iterate the
range and skip toggled rows. The `markRow`/`rowMarked`/`selectedRows` tests
in `datagrid_features_test.go` (Ctrl+Space marking) pin the behaviour. Add a benchmark at 1M rows
before and after, and drive it in tmux with `SELECT TOP 1000000 …`.

### Z11 — gosmo reads with no offline test (gosmo) — **DONE**

Done 2026-10-15: new `agent_status_test.go`, `dependency_test.go` and
`azure_resources_test.go` over the `qsRec` recording driver. Beyond the plan:
the Azure single-row reads' empty view → `ErrNotFound`, the row caps they
bind, `ResourceGovernance`'s name filter (the `strings.Replace` splice), and
`serverInfo`'s nil-safety. Coverage: `AgentInfo`/`agentInfoAzure` 92%,
`Dependencies`/`Dependents` 100%, every `azure_resources.go` read's gate
covered (the scans themselves stay live-only); package total 67.1% → 67.7%.
11 mutants, all killed. `go test ./...`, `go vet -tags livedb`, staticcheck
clean.

At gosmo's offline coverage of 67.1% (`go test -cover .`), three files score
0%: `agent_status.go`, `dependency.go` and `azure_resources.go`. The live
tests cover them, but `go test ./...` would not catch a broken branch. Add
`fakeQueryConn` tests for:

- `AgentInfo`: `sql.ErrNoRows` → `StatusText "Unknown"`, `"Running"` (any
  case) → `Running`, and the Azure branch's three outcomes (sessions →
  Running, only a start date → Stopped, neither → Unknown).
- `Dependencies`/`Dependents`: the bound argument is
  `qualifiedName(schema, name)`, an empty schema is refused with
  `ErrSchemaRequired`, and both scans place the two `bit`s in order.
- `azure_resources.go`: at least each read's refusal off Azure, matching the
  version sweep's expectation.

---

## Considered, not planned

- **Clone groups in parallel families.** `dashboard/history.go` against
  `dashboard/tempdb.go`, the full-text stoplist/search-property-list detail
  and General pages (`detail_browser_fulltext.go`, `fulltext_props.go`), and
  the explorer loaders differ in type and labels only. A generic helper over
  two to four call sites would cost more than the duplication.
- **Settled in `docs/decisions.md` or `~/go/gosmo/OPEN-THREADS.md` and not
  re-raised here:** restructuring `internal/tui`; the DDL-trigger twin
  files; schedules by id; the redo stack's byte cap; the Databases folder's
  per-database fan-out; `ScriptTable`'s round trips; the two search dialogs'
  key routing; and the case rule in `validateColumnstoreOrder`, which leaves
  case to the server on purpose (its doc comment says so).
- **`LivePlanPanel`** never calls `run.Done` after a session ends. Its
  context stays registered on the connection until the panel closes, which
  `Close`'s `Abandon` releases. That is one context per open panel, so it is
  not worth an item.

---

## Recommended implementation order

1. ~~**Z2** — doc fix, a few minutes, no code.~~ **DONE**
2. ~~**Z1** — the only defect; small, self-contained, with a live replay.~~ **DONE**
3. ~~**Z3** — additive gosmo export plus a gossms one-liner; removes a latent
   mislabel.~~ **DONE**
4. ~~**Z7** and **Z8** — mechanical gossms cleanups; one change each.~~ **DONE**
5. ~~**Z5** — non-breaking gosmo cleanup; keeps the trigger twins in step.~~ **DONE**
6. ~~**Z11** — offline tests, written before the refactors so they guard them.~~ **DONE**
7. ~~**Z4** — the scanner consolidation, one site per change, fuzzing between.~~ **DONE**
8. ~~**Z6** — the breaking signature change, done as one coordinated gosmo +
   gossms change built with `-tags livedb`.~~ **DONE**
9. ~~**Z10** — the grid selection representation, with a benchmark first.~~ **DONE**
10. ~~**Z9** — only if wanted after Z4 lands; otherwise close it as not worth
    it.~~ **DONE**

Z1–Z5, Z7, Z8 and Z11 do not depend on each other. Z4 should follow Z1, and
Z9 needs Z4.
