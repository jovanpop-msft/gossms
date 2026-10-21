package tui

import (
	"strings"
	"testing"

	gosmo "github.com/radix29/gosmo"
	"github.com/radix29/gossms/internal/db"
	"github.com/radix29/gossms/internal/tuikit/propsheet"
)

// shrinkFilesTestFiles is a database with two filegroups, so the pickers have
// something to narrow: PRIMARY holds two data files, FG2 one, plus the log.
func shrinkFilesTestFiles() []*gosmo.DatabaseFileInfo {
	return sortDatabaseFiles([]*gosmo.DatabaseFileInfo{
		{FileID: 1, Name: "App", Type: "ROWS", FileGroup: "PRIMARY", PhysicalName: `C:\d\App.mdf`, SizeKB: 72 * 1024, SpaceUsedKB: 19648},
		{FileID: 2, Name: "App_log", Type: "LOG", PhysicalName: `C:\l\App_log.ldf`, SizeKB: 72 * 1024, SpaceUsedKB: 51064},
		{FileID: 3, Name: "App_2", Type: "ROWS", FileGroup: "PRIMARY", PhysicalName: `C:\d\App_2.ndf`, SizeKB: 16 * 1024, SpaceUsedKB: 1024},
		{FileID: 4, Name: "App'fg2", Type: "ROWS", FileGroup: "FG2", PhysicalName: `C:\d\App_fg2.ndf`, SizeKB: 8*1024 + 256, SpaceUsedKB: 512},
	})
}

func shrinkFilesTestDialog(t *testing.T, sc *db.ServerConn) *ShrinkFilesDialog {
	t.Helper()
	d := &ShrinkFilesDialog{dbName: "AppDB"}
	d.sc = sc
	d.pages = []string{"General"}
	d.forms = make([]*propsheet.Form, 1)
	d.applyFns = make([]propApply, 1)
	d.buildPages(&shrinkFilesPrefetch{files: shrinkFilesTestFiles()})
	return d
}

// It opens as SSMS does — Data, PRIMARY, the primary file, Release unused
// space — and that scripts TRUNCATEONLY.
func TestShrinkFilesDefaultsToReleasingThePrimaryFile(t *testing.T) {
	sc, inst := newFakeConn(t)
	d := shrinkFilesTestDialog(t, sc)
	f := d.forms[0]

	for label, want := range map[string]string{
		"File type": "Data", "Filegroup": "PRIMARY", "File name": "App",
	} {
		if got := selectRow(t, f, label).Value(); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
	for label, want := range map[string]string{
		"Location":                  `C:\d\App.mdf`,
		"Currently allocated space": "72.00 MB",
		"Available free space":      "52.81 MB (73%)",
	} {
		if got := staticRowValue(t, f, label); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
	if got := shrinkScript(t, d.preflight, d.applyFns[0]); !strings.Contains(got, "DBCC SHRINKFILE (N'App', TRUNCATEONLY)") {
		t.Errorf("script = %q, want DBCC SHRINKFILE (N'App', TRUNCATEONLY)", got)
	}
	if n := len(inst.Statements()); n != 0 {
		t.Errorf("Script sent %d statements to the server, want none", n)
	}
}

// The pickers narrow in order, and the file scripted is the one on screen —
// not the first of the list, which a page ignoring the selection would pass.
func TestShrinkFilesPickersNarrowToTheChosenFile(t *testing.T) {
	sc, _ := newFakeConn(t)
	d := shrinkFilesTestDialog(t, sc)
	f := d.forms[0]

	if got := selectRow(t, f, "File name").Items(); len(got) != 2 || got[0] != "App" || got[1] != "App_2" {
		t.Fatalf("PRIMARY's files = %q, want [App App_2]", got)
	}
	editSelect(t, f, "Filegroup", "FG2")
	if got := selectRow(t, f, "File name").Items(); len(got) != 1 || got[0] != "App'fg2" {
		t.Fatalf("FG2's files = %q, want [App'fg2]", got)
	}
	if got := staticRowValue(t, f, "Location"); got != `C:\d\App_fg2.ndf` {
		t.Errorf("Location = %q, want FG2's file", got)
	}
	editRadio(t, f, "Shrink action", "Empty file by migrating the data to other files in the same filegroup")
	if got := shrinkScript(t, d.preflight, d.applyFns[0]); !strings.Contains(got, "DBCC SHRINKFILE (N'App''fg2', EMPTYFILE)") {
		t.Errorf("script = %q, want EMPTYFILE on App'fg2", got)
	}

	editSelect(t, f, "File type", "Log")
	groups := selectRow(t, f, "Filegroup")
	if groups.Enabled() || groups.Value() != shrinkNoFileGroup {
		t.Errorf("Filegroup for the log = %q (enabled %v), want %q, disabled", groups.Value(), groups.Enabled(), shrinkNoFileGroup)
	}
	if got := selectRow(t, f, "File name").Value(); got != "App_log" {
		t.Errorf("File name for the log = %q, want App_log", got)
	}
}

// Reorganize sends the typed size as given, except 0, which goes as 1 —
// SHRINKFILE's own 0 stops at the creation size, so a never-shrunk file would
// silently stay put — and refuses one above the file's current size.
func TestShrinkFilesReorganizeSendsTheTarget(t *testing.T) {
	sc, _ := newFakeConn(t)
	d := shrinkFilesTestDialog(t, sc)
	f := d.forms[0]

	editSelect(t, f, "File name", "App_2")
	target := textRow(t, f, "Shrink file to")
	if target.Enabled() {
		t.Fatal("Shrink file to is editable while Release unused space is chosen")
	}
	if target.Value() != "16" {
		t.Errorf("Shrink file to starts at %q, want the current size, 16", target.Value())
	}
	editRadio(t, f, "Shrink action", "Reorganize pages before releasing unused space")
	if !target.Enabled() {
		t.Fatal("choosing Reorganize left Shrink file to disabled")
	}
	editText(t, f, "Shrink file to", "0")
	if got := shrinkScript(t, d.preflight, d.applyFns[0]); !strings.Contains(got, "DBCC SHRINKFILE (N'App_2', 1)") {
		t.Errorf("script = %q, want 0 sent as DBCC SHRINKFILE (N'App_2', 1)", got)
	}
	editText(t, f, "Shrink file to", "5")
	if got := shrinkScript(t, d.preflight, d.applyFns[0]); !strings.Contains(got, "DBCC SHRINKFILE (N'App_2', 5)") {
		t.Errorf("script = %q, want DBCC SHRINKFILE (N'App_2', 5)", got)
	}

	editText(t, f, "Shrink file to", "17")
	if err := d.preflight(); err == nil {
		t.Error("preflight accepted a target above the file's 16 MB")
	}
}

// The default target rounds the size up, so an untouched Reorganize on an
// 8.25 MB file sends 9 — a no-op — not 8, a real shrink; 9 is also the most
// it accepts.
func TestShrinkFilesDefaultTargetRoundsTheSizeUp(t *testing.T) {
	sc, _ := newFakeConn(t)
	d := shrinkFilesTestDialog(t, sc)
	f := d.forms[0]

	editSelect(t, f, "Filegroup", "FG2")
	editRadio(t, f, "Shrink action", "Reorganize pages before releasing unused space")
	if got := textRow(t, f, "Shrink file to").Value(); got != "9" {
		t.Errorf("Shrink file to starts at %q on an 8.25 MB file, want 9", got)
	}
	if got := shrinkScript(t, d.preflight, d.applyFns[0]); !strings.Contains(got, "DBCC SHRINKFILE (N'App''fg2', 9)") {
		t.Errorf("script = %q, want DBCC SHRINKFILE (N'App''fg2', 9)", got)
	}
	editText(t, f, "Shrink file to", "10")
	if err := d.preflight(); err == nil {
		t.Error("preflight accepted a target above the file's rounded-up 9 MB")
	}
}

// A log's minimum is only its used space, which a log shrink can't promise to
// reach: the note says so for a log file and not for a data file.
func TestShrinkFilesLogMinimumIsALowerBound(t *testing.T) {
	sc, _ := newFakeConn(t)
	d := shrinkFilesTestDialog(t, sc)
	f := d.forms[0]

	note := func() string {
		var b strings.Builder
		for _, r := range f.Rows() {
			if n, ok := r.(*propsheet.NoteRow); ok {
				b.WriteString(n.Text() + "\n")
			}
		}
		return b.String()
	}
	if strings.Contains(note(), "virtual log file") {
		t.Errorf("a data file's note mentions VLFs:\n%s", note())
	}
	editSelect(t, f, "File type", "Log")
	if got := note(); !strings.Contains(got, "Minimum is 50 MB") || !strings.Contains(got, "last active virtual log file") {
		t.Errorf("the log's note = %q, want its 50 MB used space called a lower bound", got)
	}
}

func TestShrinkFileMinimumIsUsedSpaceRoundedUp(t *testing.T) {
	// SSMS: 19.19 MB used reads "Minimum is 20 MB".
	if got := shrinkFileMinMB(&gosmo.DatabaseFileInfo{SpaceUsedKB: 19648}); got != 20 {
		t.Errorf("minimum = %d MB, want 20", got)
	}
	if got := shrinkFileMinMB(&gosmo.DatabaseFileInfo{SpaceUsedKB: 2048}); got != 2 {
		t.Errorf("minimum of exactly 2 MB used = %d MB, want 2", got)
	}
}

// A FILESTREAM container takes EMPTYFILE and nothing else (Msg 2563, live on
// 2025), so picking one pins the action to Empty file, greys the other two,
// and shows no size figures — the catalog has none. Picking another type
// gives the choices back.
func TestShrinkFilesFilestreamOnlyEmpties(t *testing.T) {
	sc, _ := newFakeConn(t)
	d := &ShrinkFilesDialog{dbName: "AppDB"}
	d.sc = sc
	d.pages = []string{"General"}
	d.forms = make([]*propsheet.Form, 1)
	d.applyFns = make([]propApply, 1)
	files := append(shrinkFilesTestFiles(),
		&gosmo.DatabaseFileInfo{FileID: 65537, Name: "fs1", Type: "FILESTREAM", FileGroup: "FSG", PhysicalName: `C:\fs\fs1`})
	d.buildPages(&shrinkFilesPrefetch{files: files})
	f := d.forms[0]
	action := radioRow(t, f, "Shrink action")

	editSelect(t, f, "File type", "FILESTREAM Data")
	if got := selectRow(t, f, "File name").Value(); got != "fs1" {
		t.Fatalf("File name = %q, want fs1", got)
	}
	if action.Selected() != shrinkFileEmpty {
		t.Errorf("action = %q, want Empty file", action.CopyText())
	}
	if action.OptionEnabled(shrinkFileRelease) || action.OptionEnabled(shrinkFileReorganize) || !action.OptionEnabled(shrinkFileEmpty) {
		t.Error("a FILESTREAM container leaves Release or Reorganize pickable, or Empty file not")
	}
	for _, label := range []string{"Currently allocated space", "Available free space"} {
		if got := staticRowValue(t, f, label); got != shrinkFilestreamNA {
			t.Errorf("%s = %q, want %q", label, got, shrinkFilestreamNA)
		}
	}
	action.Edit(shrinkFileRelease)
	if action.Selected() != shrinkFileEmpty {
		t.Error("Edit picked a greyed-out Release")
	}
	if got := shrinkScript(t, d.preflight, d.applyFns[0]); !strings.Contains(got, "DBCC SHRINKFILE (N'fs1', EMPTYFILE)") {
		t.Errorf("script = %q, want DBCC SHRINKFILE (N'fs1', EMPTYFILE)", got)
	}
	// Even were the group forced onto another action, preflight refuses it.
	action.SetSelected(shrinkFileReorganize)
	if err := d.preflight(); err == nil || !strings.Contains(err.Error(), "EMPTYFILE") {
		t.Errorf("preflight = %v, want the FILESTREAM refusal", err)
	}

	editSelect(t, f, "File type", "Log")
	if action.Selected() != shrinkFileRelease || !action.OptionEnabled(shrinkFileRelease) || !action.OptionEnabled(shrinkFileReorganize) {
		t.Errorf("on Log: action = %q, Release/Reorganize enabled %v/%v; want Release, both enabled",
			action.CopyText(), action.OptionEnabled(shrinkFileRelease), action.OptionEnabled(shrinkFileReorganize))
	}
}
