package activity

import (
	"strings"
	"testing"
)

// TestBlockProcReadsBlockersFromWaitingTasks: a parallel request's lock wait
// is on a worker task, which only sys.dm_os_waiting_tasks names; the
// sysprocesses form lost it (its ecid > 0 row has a zero sql_handle) and,
// joined back by spid, repeated every tree row once per thread. Spelled out
// literally so a return to either fails here.
func TestBlockProcReadsBlockersFromWaitingTasks(t *testing.T) {
	body := strings.Join(strings.Fields(strings.ToLower(blockProcBody)), " ")
	for _, want := range []string{
		"from sys.dm_os_waiting_tasks where blocking_session_id > 0 and blocking_session_id <> session_id",
		"coalesce(wb.blocking_session_id, nullif(r.blocking_session_id, r.session_id), 0) as blocked",
		"order by wait_duration_ms desc, blocking_session_id",
		"from sys.dm_exec_sessions s",
		// The tree joins back to the one-row-per-session table.
		"inner join #blkchain blc on blc.spid = bt.spid",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("blockProcBody lacks %q", want)
		}
	}
	if strings.Contains(body, "sysprocesses") {
		t.Error("blockProcBody reads sys.sysprocesses: one row per thread, and a parallel worker's lock wait is lost")
	}
}

// The session running the procedure shows its text as [sql text], an xml
// processing instruction: past the body's own '--?>' literal the text is
// parsed as markup, so a "<" there fails try_cast and blanks the cell.
func TestBlockProcHasNoMarkupAfterItsPIClose(t *testing.T) {
	i := strings.Index(blockProcBody, "'--?>'")
	if i < 0 {
		t.Fatal("blockProcBody has no '--?>' literal")
	}
	if j := strings.IndexAny(blockProcBody[i:], "<&"); j >= 0 {
		t.Errorf("blockProcBody has %q after its '--?>': %q", blockProcBody[i+j], blockProcBody[i+j-20:i+j+20])
	}
}
