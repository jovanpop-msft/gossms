# **🚀 goSSMS v0.0.15**
Live query plans and Replication browsing arrive.

## ✨ New
- **Live Query Statistics** and **Live Execution Plan** from Activity Monitor
- **Replication**: publications, subscriptions, Properties, Monitor
- Full-text Catalog, Index, Property List and Stoplist dialogs
- Apply names the session blocking it
- Restore hint for log and differential sets

## 🔧 Fixes
- Failed connect kept the wrong database in a query window
- Commit after a lost session reports the rollback
- Whole-word search for `@id`, `#tmp`, non-ASCII
- Extended Events negated filters and grouping
- Change Tracking disable, file sizes, filter hang
- Backup set types, login CONNECT SQL, plan CPU time

## ⚡ Changes
- `gosmo` v0.0.17

## 📦 Install or upgrade
```
brew install radix29/tap/gossms
```
APT, downloads, checksums: <https://github.com/radix29/gossms#installation>

📋 Notes — <https://github.com/radix29/gossms/blob/main/RELEASE.md>
⬇️ Downloads — <https://github.com/radix29/gossms/releases/tag/v0.0.15>
