//go:build livedb

// Live coverage of the Block tab's procedure on a parallel blocked request
// (review plan 2026-10-16, U1): the lock wait sits on a worker task, which
// the sysprocesses form lost, showing no blocking at all.
//
//	go test -tags livedb ./internal/activity/ -run TestLiveBlockProc -v \
//	  -livedb 'sqlserver://sa:PASS@host?TrustServerCertificate=true'
//
// Creates a global temporary table (gone with its connection) and a
// test-named procedure in tempdb, dropped afterwards.
package activity

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveBlockProcShowsAParallelBlocker(t *testing.T) {
	db, ctx := liveDB(t)

	p := &Proc{MasterName: "sp_gossms_live_parblock", TempDBName: "usp_gossms_live_parblock", script: BlockProc.script}
	if err := p.Install(ctx, db, ProcTempDB); err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(),
			"exec tempdb.sys.sp_executesql N'DROP PROCEDURE IF EXISTS dbo."+p.Name(ProcTempDB)+"'"); err != nil {
			t.Errorf("drop %s: %v — drop it by hand", p.Qualified(ProcTempDB), err)
		}
	})

	conn := func() (*sql.Conn, int) {
		t.Helper()
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("connection: %v", err)
		}
		t.Cleanup(func() { c.Close() })
		var spid int
		if err := c.QueryRowContext(ctx, "SELECT @@SPID").Scan(&spid); err != nil {
			t.Fatalf("@@SPID: %v", err)
		}
		return c, spid
	}

	// The head blocker is idle in an open transaction: AWAITING COMMAND, so
	// it shows only as the root of a chain.
	head, headID := conn()
	for _, q := range []string{
		`DROP TABLE IF EXISTS ##gossms_live_parblk`,
		`CREATE TABLE ##gossms_live_parblk (id INT NOT NULL PRIMARY KEY, pad CHAR(100) NOT NULL DEFAULT 'x')`,
		`INSERT ##gossms_live_parblk (id)
		 SELECT TOP (300000) ROW_NUMBER() OVER (ORDER BY (SELECT NULL))
		 FROM sys.all_objects a CROSS JOIN sys.all_objects b`,
		`BEGIN TRANSACTION`,
		`UPDATE ##gossms_live_parblk SET pad = 'y' WHERE id = 150000`,
	} {
		if _, err := head.ExecContext(ctx, q); err != nil {
			t.Fatalf("head: %s: %v", q, err)
		}
	}
	t.Cleanup(func() { head.ExecContext(context.Background(), "IF @@TRANCOUNT > 0 ROLLBACK") })

	for _, tc := range []struct {
		name, hint string
		parallel   bool
	}{
		{"parallel", "OPTION (MAXDOP 4, USE HINT('ENABLE_PARALLEL_PLAN_PREFERENCE'))", true},
		{"serial", "OPTION (MAXDOP 1)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader, readerID := conn()
			rctx, cancel := context.WithCancel(ctx)
			finished := make(chan error, 1)
			go func() {
				var n int
				// The predicate keeps the plan out of TRIVIAL optimization,
				// which is always serial whatever the hint.
				finished <- reader.QueryRowContext(rctx, "SELECT COUNT(*) FROM ##gossms_live_parblk WHERE pad <> 'z' "+tc.hint).Scan(&n)
			}()
			t.Cleanup(func() { cancel(); <-finished })

			var tasks int
			deadline := time.Now().Add(15 * time.Second)
			for {
				var blocked int
				if err := db.QueryRowContext(ctx, `
SELECT (SELECT COUNT(*) FROM sys.dm_os_tasks WHERE session_id = @p1),
       (SELECT COUNT(*) FROM sys.dm_os_waiting_tasks WHERE session_id = @p1 AND blocking_session_id = @p2)`,
					readerID, headID).Scan(&tasks, &blocked); err != nil {
					t.Fatalf("waiting tasks: %v", err)
				}
				if blocked > 0 && (tasks > 1 || !tc.parallel) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("reader %d (%d task(s)) never blocked on head %d; a parallel run needs a parallel plan", readerID, tasks, headID)
				}
				time.Sleep(100 * time.Millisecond)
			}

			rows, err := db.QueryContext(ctx, p.Exec(ProcTempDB))
			if err != nil {
				t.Fatalf("%s: %v", p.Exec(ProcTempDB), err)
			}
			defer rows.Close()
			// spid (the number ending blktree) → blocking_spid, and how often
			// each spid appears.
			blocker, seen := map[int]int{}, map[int]int{}
			for rows.Next() {
				var tree, blkBy sql.NullString
				var skip [6]sql.NullString
				if err := rows.Scan(&tree, &skip[0], &skip[1], &skip[2], &skip[3], &skip[4], &skip[5], &blkBy); err != nil {
					t.Fatalf("scan: %v", err)
				}
				f := strings.Fields(tree.String)
				spid, _ := strconv.Atoi(f[len(f)-1])
				b, _ := strconv.Atoi(blkBy.String)
				blocker[spid] = b
				seen[spid]++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if b, ok := blocker[readerID]; !ok || b != headID {
				t.Errorf("reader %d: in tree %v, blocked by %d; want blocked by head %d (tree %v)", readerID, ok, b, headID, blocker)
			}
			if b, ok := blocker[headID]; !ok || b != 0 {
				t.Errorf("head %d: in tree %v, blocked by %d; want a root (tree %v)", headID, ok, b, blocker)
			}
			for spid, n := range seen {
				if n != 1 {
					t.Errorf("session %d appears %d times, want once", spid, n)
				}
			}
		})
	}
}
