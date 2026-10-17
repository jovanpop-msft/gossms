package tui

import (
	"context"
	"slices"
	"testing"
)

// backupDatabases offers only what BACKUP DATABASE accepts: tempdb, a
// non-ONLINE database and a database snapshot (Msg 3002) are each a certain
// refusal, so none is listed.
func TestBackupDatabaseNamesDropsWhatBackupRefuses(t *testing.T) {
	offline := databaseRow(8, "offlinedb", 0)
	offline[2] = "OFFLINE"
	sc, _ := newFakeConn(t, databasesListResp(
		databaseRow(1, "master", 0),
		databaseRow(2, "tempdb", 0),
		databaseRow(7, "appdb", 0),
		offline,
		databaseRow(9, "appdb_snap", 7),
	))
	dbs, err := backupDatabases(context.Background(), sc)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := namesOf(dbs), []string{"master", "appdb"}; !slices.Equal(got, want) {
		t.Errorf("backupDatabases = %v, want %v", got, want)
	}
}

// On a case-sensitive server TEMPDB is a legal user database distinct from
// tempdb (ID 2); only the real tempdb is excluded.
func TestBackupDatabasesKeepsCaseVariantOfTempdb(t *testing.T) {
	sc, _ := newFakeConn(t, databasesListResp(
		databaseRow(2, "tempdb", 0),
		databaseRow(9, "TEMPDB", 0),
	))
	dbs, err := backupDatabases(context.Background(), sc)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := namesOf(dbs), []string{"TEMPDB"}; !slices.Equal(got, want) {
		t.Errorf("backupDatabases = %v, want %v", got, want)
	}
}
