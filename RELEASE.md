# Release Notes

What changed in the current goSSMS release. Detail, and every earlier release,
is in [CHANGELOG.md](CHANGELOG.md). Questions and feedback:
[goSSMS on Discord](https://discord.gg/7YVKzB3vZ).

## v0.1.0 — 2026-10-20

### New

- Windows on ARM (`windows/arm64`) binary.
- Backup checks: unique names, overwrite prompt, compression default, types
  greyed with the reason.
- Database menu state toggle; distribution databases match SSMS.
- Database Files shrink and grow; snapshot size on disk.
- Change Tracking columns, login policy and unlock, server role ratings.
- Messages pane jumps to the error line.

### Fixes

- Results To File no longer overwrites the file with partial output.
- CSV export of a one-column NULL; copy from an empty grid.
- Atomic config writes; corrupt config is kept as a copy.
- Restore verifies the chosen backup set.
- Availability group, Query Store, containment and filegroup validation.
- Money precision, processor affinity, long values in properties dialogs.

### Changes

- `gosmo` v0.1.0.
