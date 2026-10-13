# Release Notes

What changed in the current goSSMS release. Detail, and every earlier release,
is in [CHANGELOG.md](CHANGELOG.md). Questions and feedback:
[goSSMS on Discord](https://discord.gg/7YVKzB3vZ).

## v0.0.15 — 2026-10-13

### New

- **Live Query Statistics** and **Live Execution Plan** (Activity Monitor).
- **Replication**: publications, subscriptions, Properties, Replication Monitor.
- New Full-Text Catalog, Index, Search Property List and Stoplist dialogs.
- Apply names the session blocking it.
- Restore hint for log and differential sets.

### Fixes

- Failed or cancelled connect kept the wrong database in a query window.
- Commit after a lost session now reports the rollback.
- Whole-word search for `@id`, `#tmp` and non-ASCII words.
- Extended Events negated filters and grouping.
- Change Tracking disable, file sizes, and a filter hang on a lone `!`.
- Backup set types, login CONNECT SQL grants, parallel plan CPU time.
- Menu and toolbar hover behind dialogs.

### Changes

- `gosmo` v0.0.17.
