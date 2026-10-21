package tui

import (
	"strings"
	"testing"
)

// "Script Database Security" opens the generator in a new query window for
// the node's database and runs nothing: the user reviews and executes it.
func TestScriptDatabaseSecurityOpensUnexecutedScript(t *testing.T) {
	a := newTestApp()
	sc := addTestConn(a, "server-one")
	node := &explorerNode{label: "odd]db", data: nodeData{Type: NodeDatabase, DBName: "odd]db", DBState: "ONLINE", conn: sc}}

	item := findMenuItem(a.contextMenuItemsForNode(node), "Script Database Security")
	if item == nil {
		t.Fatal("the database menu has no Script Database Security item")
	}
	if item.Enabled != nil {
		t.Error("Script Database Security is gated; it only opens a script")
	}
	item.Action()

	if a.panels.Count() != 1 {
		t.Fatalf("panels = %d, want the one query window", a.panels.Count())
	}
	qp, ok := a.panels.PanelAt(0).(*QueryPanel)
	if !ok {
		t.Fatalf("panel is %T, want a *QueryPanel", a.panels.PanelAt(0))
	}
	text := qp.editor.Text()
	if !strings.HasPrefix(text, "USE [odd]]db];\nGO\n") {
		t.Errorf("script does not start in the node's database, quoted: %q", text[:min(len(text), 40)])
	}
	if !strings.Contains(text, "SELECT line AS [Script] FROM #script ORDER BY id;") {
		t.Error("script lost its final result set")
	}
}

// A role created without AUTHORIZATION is owned by whoever runs the output
// (a non-dbo db_owner, live), and a Windows user named as its account is
// scripted as created, without FOR LOGIN.
func TestDatabaseSecurityScriptOwnershipAndWindowsUsers(t *testing.T) {
	body := databaseSecurityScriptBody
	if !strings.Contains(body, "CREATE ROLE ' + QUOTENAME(dp.name) + N' AUTHORIZATION [dbo];'") {
		t.Error("roles are not created owned by dbo")
	}
	windows := strings.Index(body, "WHEN dp.authentication_type = 3 AND dp.name = l.login_name THEN N''")
	forLogin := strings.Index(body, "ELSE N' FOR LOGIN '")
	if windows < 0 || forLogin < 0 || windows > forLogin {
		t.Error("a Windows user named as its account is not scripted without FOR LOGIN ahead of the FOR LOGIN arm")
	}
}
