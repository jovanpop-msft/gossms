package activity

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

// findQueryFor is the lookup Proc.Find issues, so the fake answers the real
// query.
func findQueryFor(p *Proc) string {
	return `select
	case when object_id('master.dbo.` + p.MasterName + `', 'P') is not null then 1 else 0 end,
	case when object_id('tempdb.dbo.` + p.TempDBName + `', 'P') is not null then 1 else 0 end`
}

// Find prefers master: a master procedure survives restarts, a tempdb one
// doesn't.
func TestProcFindPrefersMaster(t *testing.T) {
	p := BlockProc
	for _, tc := range []struct {
		name             string
		master, inTempDB bool
		want             ProcLocation
	}{
		{"both", true, true, ProcMaster},
		{"master only", true, false, ProcMaster},
		{"tempdb only", false, true, ProcTempDB},
		{"neither", false, false, ProcNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := scriptedDB(t, map[string]reply{
				findQueryFor(p): {
					cols: []string{"in_master", "in_tempdb"},
					rows: [][]driver.Value{{tc.master, tc.inTempDB}},
				},
			})
			got, err := p.Find(context.Background(), db)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if got != tc.want {
				t.Errorf("Find = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProcFindReportsAFailedLookup(t *testing.T) {
	boom := errors.New("activity_test: lookup failed")
	p := BlockProc
	db, _ := scriptedDB(t, map[string]reply{findQueryFor(p): {err: boom}})

	got, err := p.Find(context.Background(), db)
	if err == nil {
		t.Fatalf("Find returned %v and no error", got)
	}
	if !errors.Is(err, boom) {
		t.Errorf("error = %v, want the driver's own wrapped", err)
	}
	if got != ProcNone {
		t.Errorf("Find = %v on a failed lookup, want ProcNone", got)
	}
}

// Install must reach the target database without USE (the pooled connection's
// database must be left alone) and pass the body as a parameter, since it's
// full of quotes.
func TestProcInstallGoesThroughTheTargetDatabasesSpExecutesql(t *testing.T) {
	p := BlockProc
	for _, tc := range []struct {
		loc      ProcLocation
		wantStmt string
	}{
		{ProcMaster, "exec master.sys.sp_executesql @stmt"},
		{ProcTempDB, "exec tempdb.sys.sp_executesql @stmt"},
	} {
		t.Run(tc.loc.Database(), func(t *testing.T) {
			db, log := scriptedDB(t, nil)
			if err := p.Install(context.Background(), db, tc.loc); err != nil {
				t.Fatalf("Install: %v", err)
			}
			if n := log.count(tc.wantStmt); n != 1 {
				t.Errorf("Install ran %d statements matching %q; statements: %v", n, tc.wantStmt, log.qs)
			}
			if n := log.count("use "); n != 0 {
				t.Error("Install issued a USE: the pooled connection's database must be left as it was found")
			}
		})
	}
}

// ProcNone names no database; installing must error rather than hit whatever
// database the pooled connection is in.
func TestProcInstallRefusesProcNone(t *testing.T) {
	p := BlockProc
	db, log := scriptedDB(t, nil)

	if err := p.Install(context.Background(), db, ProcNone); err == nil {
		t.Fatal("Install(ProcNone) returned no error")
	}
	if n := len(log.qs); n != 0 {
		t.Errorf("Install(ProcNone) ran %d statements: %v", n, log.qs)
	}
}

// The tempdb copy must not be sp_-prefixed: sp_ names fall back to master, so
// DROP PROCEDURE IF EXISTS dbo.sp_block in tempdb deleted master's copy
// (verified live).
func TestProcTempDBNameIsNotSpPrefixed(t *testing.T) {
	p := BlockProc
	if !strings.HasPrefix(p.MasterName, "sp_") {
		t.Errorf("MasterName = %q, want the sp_ name a hand-installed copy already has", p.MasterName)
	}
	if strings.HasPrefix(p.TempDBName, "sp_") {
		t.Errorf("TempDBName = %q; an sp_ name in tempdb resolves to master's copy", p.TempDBName)
	}
	// "dbo." spelled out because MasterName is a substring of TempDBName
	// (sp_block in usp_block).
	if got := p.Script(ProcTempDB); !strings.Contains(got, "dbo."+p.TempDBName) || strings.Contains(got, "dbo."+p.MasterName) {
		t.Errorf("the tempdb script does not create dbo.%s:\n%s", p.TempDBName, got)
	}
	if got := p.Script(ProcMaster); !strings.Contains(got, "dbo."+p.MasterName) {
		t.Errorf("the master script does not create dbo.%s:\n%s", p.MasterName, got)
	}
	if got := p.Script(ProcNone); got != "" {
		t.Errorf("Script(ProcNone) = %q, want empty", got)
	}
	if got := p.Exec(ProcTempDB); got != "exec tempdb.dbo."+p.TempDBName {
		t.Errorf("Exec(ProcTempDB) = %q", got)
	}
	if got := p.Exec(ProcNone); got != "" {
		t.Errorf("Exec(ProcNone) = %q, want empty", got)
	}
}

// Current compares the stored definition with this build's script, so an
// earlier gossms's tempdb copy is found out and replaced.
func TestProcCurrentComparesTheStoredDefinition(t *testing.T) {
	p := BlockProc
	q := "select definition from tempdb.sys.sql_modules where object_id = object_id(@p1, 'P')"
	for _, tc := range []struct {
		name string
		rows [][]driver.Value
		want bool
	}{
		// As the server stores it: CREATE OR ALTER's head rewritten.
		{"this build's", [][]driver.Value{{strings.Replace(p.Script(ProcTempDB), "create or alter procedure", "create   procedure", 1)}}, true},
		{"this build's, head as sent", [][]driver.Value{{p.Script(ProcTempDB)}}, true},
		{"one byte off", [][]driver.Value{{p.Script(ProcTempDB) + " "}}, false},
		{"an earlier body", [][]driver.Value{{"create or alter procedure dbo.usp_block as select 1"}}, false},
		{"unreadable definition", [][]driver.Value{{nil}}, false},
		{"no such procedure", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := scriptedDB(t, map[string]reply{q: {cols: []string{"definition"}, rows: tc.rows}})
			got, err := p.Current(context.Background(), db, ProcTempDB)
			if err != nil || got != tc.want {
				t.Errorf("Current = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	db, log := scriptedDB(t, nil)
	if _, err := p.Current(context.Background(), db, ProcNone); err == nil || len(log.qs) != 0 {
		t.Errorf("Current(ProcNone) = %v after %d statements, want an error and none", err, len(log.qs))
	}
}
