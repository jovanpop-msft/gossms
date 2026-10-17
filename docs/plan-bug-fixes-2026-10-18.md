# Plan: three bugs found in review (2026-10-18)

Review of gossms `HEAD` (a1b52cc) and gosmo `HEAD` (3cd5fa6). `go vet` and
`staticcheck` are clean on both, and `go test ./internal/...` passes, so all
three were found by reading. Each was reproduced or confirmed against source.
Nothing here is committed; the fixes are not started.

Not covered: no sweep of the full gosmo surface (about 190 non-test files).
This pass read `quoting.go`, `retry.go`, `restore.go`, the `connection*.go`
address parsing, the permission and replica emitters, `config/secret.go`,
`fileutil/atomic.go` and `query/executor.go`. The SQL emitters read were
escaped correctly. Nothing was run against a live server.

## B1 — Results To File commits partial or cancelled output over the destination (gossms, data loss)

**Where:** `internal/tui/query_panel_exec.go:286-301` (`startRun`),
`internal/tui/query_panel_export.go:97-110` (`csvSink.Close`/`Abort`).

**What:** commit a1b52cc made the CSV sink stream to a temp file and replace
the target only on `Commit`, so "a failed or stopped run leaves what the user
agreed to overwrite as it was". The wiring was not finished:

- `startRun` always calls `sink.Close()` after `ExecuteToSink`, which flushes
  and **commits**. `sink.Abort()` has no caller.
- `exportKept` is declared (`:281`) and passed to `reportExport`, but never
  assigned, so the "Results not written; … was not changed" message can't
  appear.

**Failure scenario:** the user picks an existing `results.csv`, confirms the
overwrite, runs a long query and presses Stop (or a batch fails mid-stream).
The destination is replaced by the partial rows, and the status says
"N row(s) written". The old file is gone. This is the exact behaviour the
commit set out to remove.

**Fix:**
1. After `sess.ExecuteToSink`, decide commit vs abort:
   `ctx.Err() != nil` (cancelled) → `sink.Abort()`, `exportKept = true`.
   Decide separately whether a batch error also aborts. Recommended: yes when
   no result set completed, no when at least one set finished (SSMS keeps
   partial output). Settle this with the user before implementing; if unsure,
   abort on any error, which is what the commit message promised.
2. Otherwise `exportErr = sink.Close()`.
3. Make `Close`/`Abort` mutually safe (`Stream` already no-ops the second
   call).
4. Tests in `query_panel_test.go`: a cancelled export leaves an existing file's
   bytes unchanged and reports "not changed"; a successful one replaces it;
   a failed `newCSVSink` still reports the error. Use `fakedb_test.go`.
5. Drive the built binary per `docs/testing.md` (tmux harness): run a long
   `WAITFOR`/large `SELECT` to an existing file, Stop it, diff the file.

**Risk:** low. Local to `startRun`.

## B2 — CSV export writes a one-column NULL row as a blank line, which readers drop (gossms, silent row loss)

**Where:** `internal/tui/query_panel_export.go:51-69` (`writeRecord`).

**What:** a NULL is written as nothing, and an empty string as `""`. In a
single-column result a NULL row is therefore the bare line `\n`.
`encoding/csv` (and most readers) skip blank lines, which is why
`encoding/csv`'s own writer quotes a lone empty field. The blank line also
doubles as the separator between result sets (`BeginSet`), so the two can't be
told apart.

**Failure scenario:** `SELECT nullable_col FROM t` exported to CSV and read
back loses every NULL row. Row counts differ from "(N rows affected)".

**Fix:** in `writeRecord`, when `len(cells) == 1` and the only cell is NULL,
write `""`. This collapses NULL and empty string for that one shape, which
`encoding/csv` accepts as the lone-field case. Document the collapse in the
function comment. The alternative, an explicit NULL token, is a format
change and needs the user's decision (`docs/decisions.md` check first).
Tests: single-column NULL row round-trips through `encoding/csv` with the
right row count; multi-column rows are unchanged; the separator blank line
between sets still appears.

**Risk:** low. Output differs only for the one-column NULL case.

## B3 — gosmo accepts an out-of-range or signed port inside `Server`, and dials the wrong port (gosmo)

**Where:** `~/go/gosmo/connection.go:414-460` (`splitServerAddress` uses
`strconv.Atoi`), `connection_dsn.go:364` (the 0–65535 check covers
`opts.Port` only).

**What:** a port written in the server string skips range validation.
Observed with a probe test:

| Server | Parsed port | Result |
|---|---|---|
| `host,99999` | 99999 | DSN host `host:99999`, no error |
| `host\i,70000` | 70000 | DSN host `host:70000`, no error |
| `host,-5` | -5 | DSN host `host`, so it **silently dials 1433** |
| `host:+1500` | 1500 | accepted; `+` is not part of the port syntax |

The `host,-5` case connects to a different endpoint than the user typed, with
the saved credentials. `config.InstanceKey` also builds keys from the
unvalidated port.

**Fix (back-compat may break):**
1. `splitServerAddress`: accept a port only if it is all ASCII digits
   (reject `+`/`-`), as `splitProtocolPrefix` already does for its check.
   A non-digit suffix stays part of the host, per the existing doc.
2. `dsnHost`: after `opts.address()`, apply the 0–65535 check to the
   effective port, so `host,99999` returns the same `invalidf` error as
   `ConnectionOptions.Port`. Port 0 written in the string (`host,0`) should
   also be refused rather than treated as unset.
3. gosmo tests: table in `connection_test.go` for the four inputs above plus
   `host,0`, `host\i,65535`, `host,65536`. Update the `ParseServerAddress`
   doc.
4. gossms: no code change expected. `ResolveServer`/`InstanceKey` follow
   gosmo. Add one `address_test.go` case so a bad embedded port doesn't key
   as a different instance. Build with the active `replace`, per the
   `dev-with-local-gosmo` skill.
5. CHANGELOG/RELEASE are not edited (repo rule).

**Risk:** low-medium. Strings like `host,99999` stop connecting, which is the
intent. Check saved configs and the Connect dialog's error display.

## Smaller finding, not planned: `core.SafeFileName` is not Windows-safe

`internal/tuikit/core/strutil.go:408` claims "usable as one path component on
every OS" but doesn't handle Windows reserved device names (`CON`, `NUL`,
`AUX`, `COM1`…, reserved even with an extension, so a schemaless node named
`CON` proposes `CON.sql`) or the 255-byte limit (schema + name + `.sql` can exceed it).
Impact is low: it only seeds the Save dialog's proposed name, and the user can
edit it. If wanted, prefix reserved stems with `_` and truncate by bytes on a
rune boundary. If you don't want to fix it, add it to
`docs/open-threads.md` § Bugs.

## Recommended implementation order

1. **B1** — regression from the latest commit, user-visible data loss, and the
   smallest fix. Do it first and drive the real binary.
2. **B3** — independent of B1/B2 and lives in gosmo. Do it next so the
   `replace` build carries it, then add the gossms test.
3. **B2** — small, isolated, no dependencies. Confirm the NULL-versus-empty
   representation with the user first.
4. (Optional) `SafeFileName` hardening.

After each: `gofmt -w . && go vet ./... && go test ./...` in the repo
touched, then a live check from `docs/testing.md`. Do not commit; ask the user
to.
