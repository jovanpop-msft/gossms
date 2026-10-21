//go:build livedb

// Live check of WithMaxRows, retained and streamed: reaching the cap cancels
// the rest of the script (an attention for the batch), so a set of ~100M rows
// returns at once, neither the batch's later statements nor the next GO batch
// run, and the session survives with its temp tables.
//
//	go test -tags livedb ./internal/query/ -run TestLiveMaxRows -v \
//	  -livedb 'sqlserver://sa:PASS@host?TrustServerCertificate=true'
package query

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// maxRowsScript's first batch starts a set far too large to read in the test's
// time (sys.all_columns squared), then two statements that must not run.
const maxRowsScript = "CREATE TABLE #keep(i int)\n" +
	"GO\n" +
	"SELECT 1 FROM sys.all_columns a CROSS JOIN sys.all_columns b\n" +
	"SELECT TOP (3) object_id FROM sys.all_objects\n" +
	"PRINT 'after'\n" +
	"GO\n" +
	"PRINT 'next batch'"

func checkCutRun(t *testing.T, res *Result, elapsed time.Duration) {
	t.Helper()
	if res.HasErrors() || res.SessionLost {
		t.Fatalf("errors (lost=%v): %v", res.SessionLost, messageTexts(res))
	}
	if elapsed > 10*time.Second {
		t.Errorf("run took %v: the capped set was read on, not cancelled", elapsed)
	}
	msgs := messageTexts(res)
	if slices.Contains(msgs, "after") || slices.Contains(msgs, "next batch") {
		t.Errorf("a statement after the capped set ran: %v", msgs)
	}
	for _, want := range []string{"Result set 1: only the first 3 rows", "The rest of the script was cancelled"} {
		if !slices.ContainsFunc(msgs, func(m string) bool { return strings.HasPrefix(m, want) }) {
			t.Errorf("no %q notice: %v", want, msgs)
		}
	}
}

func TestLiveMaxRows(t *testing.T) {
	db, ctx, done := livePlanDB(t)
	defer done()

	s, _, err := Open(ctx, db, "tempdb")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	start := time.Now()
	res := s.Execute(ctx, maxRowsScript, WithMaxRows(3))
	checkCutRun(t, res, time.Since(start))
	if len(res.Sets) != 1 || len(res.Sets[0].Rows) != 3 || !res.Sets[0].Truncated {
		t.Fatalf("sets %+v, want one truncated set of 3 rows: %v", res.Sets, messageTexts(res))
	}

	// A GO count does not repeat a cut batch.
	res = s.Execute(ctx, "SELECT 1 FROM sys.all_columns a CROSS JOIN sys.all_columns b\nGO 3", WithMaxRows(3))
	if len(res.Sets) != 1 || slices.ContainsFunc(messageTexts(res), func(m string) bool { return strings.HasPrefix(m, "Batch execution completed") }) {
		t.Errorf("GO 3 after a cut: %d sets, %v; want 1 set and no repeat", len(res.Sets), messageTexts(res))
	}

	// The cancelled batch left the session as it was.
	res = s.Execute(ctx, "SELECT COUNT(*) FROM #keep")
	if res.HasErrors() || len(res.Sets) != 1 {
		t.Errorf("session lost its temp table after the cancel: %v", messageTexts(res))
	}
}

func TestLiveMaxRowsToSink(t *testing.T) {
	db, ctx, done := livePlanDB(t)
	defer done()

	s, _, err := Open(ctx, db, "tempdb")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	sink := &recordingSink{}
	start := time.Now()
	res := s.ExecuteToSink(ctx, maxRowsScript, sink, WithMaxRows(3))
	checkCutRun(t, res, time.Since(start))
	if !slices.Equal(sink.endRows, []int{3}) || res.RowsWritten != 3 || res.SinkSetsTruncated != 1 {
		t.Errorf("EndSet %v, RowsWritten %d, SinkSetsTruncated %d; want [3], 3, 1",
			sink.endRows, res.RowsWritten, res.SinkSetsTruncated)
	}
}

// With an actual plan captured, the cancel must not leave STATISTICS XML on:
// a SessionLost would say switching it off failed.
func TestLiveMaxRowsWithPlan(t *testing.T) {
	db, ctx, done := livePlanDB(t)
	defer done()

	s, _, err := Open(ctx, db, "tempdb")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	start := time.Now()
	res := s.ExecuteWithPlan(ctx, maxRowsScript, WithMaxRows(3))
	checkCutRun(t, res, time.Since(start))
	res = s.Execute(ctx, "SELECT 1")
	if res.HasErrors() || len(res.Sets) != 1 || len(res.PlanXML) != 0 {
		t.Errorf("after the cancel: %d sets, %d plans, %v; want 1 set, no plan", len(res.Sets), len(res.PlanXML), messageTexts(res))
	}
}
