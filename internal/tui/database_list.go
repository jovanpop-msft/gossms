package tui

import (
	"context"

	gosmo "github.com/radix29/gosmo"
	"github.com/radix29/gossms/internal/db"
)

// The rule for which databases a dropdown offers, in one place rather than decided
// separately at each call site.
//
// It turns on *when* the name is resolved, not on what looks tidy:
//
//   - A name stored now and resolved later — a job step's database, an alert's
//     database, a login's default database — lists every database, system and
//     non-ONLINE alike. The database is opened when the job runs or the login
//     connects, so one that is offline today is a legitimate choice, and
//     filtering it out would silently drop a name already configured on the
//     server.
//   - A name acted on now lists only what the action can actually run against:
//     backupDatabases for Backup, onlineDatabaseNames for a read made the
//     moment the name is picked.
//
// databaseNames is the first case: every database, in the server's order.
func databaseNames(ctx context.Context, sc *db.ServerConn) ([]string, error) {
	dbs, err := sc.Server.Databases(ctx)
	if err != nil {
		return nil, err
	}
	return namesOf(dbs), nil
}

// tempdbID is the database_id SQL Server permanently gives tempdb. Matching it
// rather than the name keeps a user database called TEMPDB on a case-sensitive
// server.
const tempdbID = 2

// backupDatabases is the second case: the databases BACKUP DATABASE will
// accept right now. It returns the databases, not their names, because the
// Backup dialog also needs each one's recovery model (backupTypeBlocks).
//
// Every exclusion is a hard server restriction, not a preference: `BACKUP
// DATABASE tempdb` and a backup of an OFFLINE database each fail with "BACKUP
// DATABASE is terminating abnormally", and a database snapshot — ONLINE, and
// listed by Server.Databases — with Msg 3002 ("Cannot BACKUP or RESTORE a
// database snapshot"), so offering any of them gives a dropdown entry whose
// only outcome is that error. SSMS does not list snapshots either.
func backupDatabases(ctx context.Context, sc *db.ServerConn) ([]*gosmo.Database, error) {
	dbs, err := sc.Server.Databases(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*gosmo.Database, 0, len(dbs))
	for _, d := range dbs {
		if d.ID == tempdbID || d.State != "ONLINE" || d.IsSnapshot() {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// onlineDatabaseNames is the second case for a picker that reads the chosen
// database at once (the full-text New dialogs' "Copy from database"): every
// ONLINE database. Anything else fails the read with "cannot be opened", so
// offering it only offers that error. Access is not filtered here — it is a
// probe per database, asked when one is chosen.
func onlineDatabaseNames(ctx context.Context, sc *db.ServerConn) ([]string, error) {
	dbs, err := sc.Server.Databases(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(dbs))
	for _, d := range dbs {
		if d.State == "ONLINE" {
			out = append(out, d.Name)
		}
	}
	return out, nil
}

// namesOf extracts the names for the dropdown prefetches.
func namesOf(dbs []*gosmo.Database) []string {
	out := make([]string, len(dbs))
	for i, d := range dbs {
		out[i] = d.Name
	}
	return out
}
