package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	gosmo "github.com/radix29/gosmo"
	"github.com/radix29/gossms/internal/db"
	"github.com/radix29/gossms/internal/tuikit/propsheet"
)

// shrink_files_dialog.go is the database node's Shrink > Files... — SSMS's
// Tasks > Shrink > Files: one file, picked by type, filegroup and name, shrunk
// one of three ways. Script-only, like Shrink Database (see
// shrink_database_dialog.go for why and for the shared parts).

// shrinkFileTypes maps sys.database_files.type_desc to the File type row's
// wording, SSMS's, in the order the row lists them.
var shrinkFileTypes = []struct{ typeDesc, label string }{
	{"ROWS", "Data"},
	{"LOG", "Log"},
	{"FILESTREAM", "FILESTREAM Data"},
}

// shrinkFileActions are the Shrink action choices, indexed by the
// shrinkFile* constants below.
var shrinkFileActions = []string{
	"Release unused space",
	"Reorganize pages before releasing unused space",
	"Empty file by migrating the data to other files in the same filegroup",
}

const (
	shrinkFileRelease = iota
	shrinkFileReorganize
	shrinkFileEmpty
)

// shrinkNoFileGroup is the Filegroup row's only item for a log file, which
// belongs to none.
const shrinkNoFileGroup = "(not applicable)"

// shrinkFilesPrefetch is the database's files, data files first.
type shrinkFilesPrefetch struct {
	files []*gosmo.DatabaseFileInfo
}

// ShrinkFilesDialog is the Shrink File dialog.
type ShrinkFilesDialog struct {
	newObjectDialog[shrinkFilesPrefetch]

	// dbName is set by show, before the shell's own show runs.
	dbName string
}

// NewShrinkFilesDialog creates the dialog and wires its callbacks.
func NewShrinkFilesDialog(app *App) *ShrinkFilesDialog {
	d := &ShrinkFilesDialog{}
	d.init(app, newObjectConfig[shrinkFilesPrefetch]{
		title:      "Shrink File",
		noun:       "File",
		pages:      []string{"General"},
		fetch:      d.fetchPrefetch,
		build:      d.buildPages,
		refresh:    func(*db.ServerConn) {},
		scriptOnly: true,
	})
	return d
}

func (d *ShrinkFilesDialog) show(sc *db.ServerConn, dbName string) {
	d.dbName = dbName
	d.scriptDatabase = dbName
	d.newObjectDialog.show(sc)
	d.SetTitle("Shrink File - " + dbName)
	d.SetHeader("Database: "+dbName, "Server: "+sc.Opts.Server)
}

// fetchPrefetch reads the file list with each file's used space, which is what
// the free-space figure and the shrink's floor come from.
func (d *ShrinkFilesDialog) fetchPrefetch(ctx context.Context, sc *db.ServerConn) (*shrinkFilesPrefetch, error) {
	files, err := sc.Server.DatabaseRef(d.dbName).Files(ctx)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files of %q are visible to this login", d.dbName)
	}
	return &shrinkFilesPrefetch{files: sortDatabaseFiles(files)}, nil
}

// shrinkFilePicker narrows the file list as the three pickers do: the types
// present, the filegroups of a type, the files of a type and filegroup. Kept
// apart from the rows so the narrowing can be tested without a form.
type shrinkFilePicker struct{ files []*gosmo.DatabaseFileInfo }

// types returns the File type labels present, in shrinkFileTypes order, with
// the type_desc each stands for.
func (p shrinkFilePicker) types() (labels, typeDescs []string) {
	for _, t := range shrinkFileTypes {
		for _, f := range p.files {
			if f.Type == t.typeDesc {
				labels = append(labels, t.label)
				typeDescs = append(typeDescs, t.typeDesc)
				break
			}
		}
	}
	return labels, typeDescs
}

// fileGroups returns the filegroups holding files of typeDesc, in file order
// (PRIMARY first, as its file 1 is), or shrinkNoFileGroup for the log.
func (p shrinkFilePicker) fileGroups(typeDesc string) []string {
	if typeDesc == "LOG" {
		return []string{shrinkNoFileGroup}
	}
	var out []string
	for _, f := range p.files {
		if f.Type == typeDesc && !slices.Contains(out, f.FileGroup) {
			out = append(out, f.FileGroup)
		}
	}
	return out
}

// filesIn returns the files of typeDesc in fileGroup (any, for the log).
func (p shrinkFilePicker) filesIn(typeDesc, fileGroup string) []*gosmo.DatabaseFileInfo {
	var out []*gosmo.DatabaseFileInfo
	for _, f := range p.files {
		if f.Type == typeDesc && (typeDesc == "LOG" || f.FileGroup == fileGroup) {
			out = append(out, f)
		}
	}
	return out
}

// shrinkFileMinMB is the smallest size, in whole MB, a reorganizing shrink can
// reach: the file's used space rounded up — SSMS's "Minimum is 20 MB" for
// 19.19 MB used. 0 when the used space is unknown.
func shrinkFileMinMB(f *gosmo.DatabaseFileInfo) int64 {
	return (f.SpaceUsedKB + 1023) / 1024
}

func (d *ShrinkFilesDialog) buildPages(pf *shrinkFilesPrefetch) {
	sc := d.sc
	dbName := d.dbName
	picker := shrinkFilePicker{files: pf.files}

	typeLabels, typeDescs := picker.types()
	typeRow := propsheet.Select("File type", typeLabels, 0)
	groupRow := propsheet.Select("Filegroup", nil, 0)
	fileRow := propsheet.Select("File name", nil, 0)
	location := propsheet.Static("Location", "")
	allocated := propsheet.Static("Currently allocated space", "")
	free := propsheet.Static("Available free space", "")
	action := propsheet.Radio("Shrink action", shrinkFileActions, shrinkFileRelease)
	target := propsheet.Int("Shrink file to", 0, 0, 1<<31-1, "MB")
	target.SetEnabled(false)
	minimum := propsheet.DynamicNote("")
	waitRows, wait := shrinkWaitRows(sc)

	// candidates is the File name row's files, in its order; current the one
	// selected. Both follow the pickers, on the UI goroutine only.
	var candidates []*gosmo.DatabaseFileInfo
	current := func() *gosmo.DatabaseFileInfo {
		if i := fileRow.Selected(); i >= 0 && i < len(candidates) {
			return candidates[i]
		}
		return nil
	}
	typeDesc := func() string {
		if i := typeRow.Selected(); i >= 0 && i < len(typeDescs) {
			return typeDescs[i]
		}
		return ""
	}
	showFile := func() {
		f := current()
		if f == nil {
			return
		}
		location.SetValue(f.PhysicalName)
		allocated.SetValue(shrinkMB(f.SizeKB))
		if f.SpaceUsedKB > 0 {
			free.SetValue(shrinkFreeText(f.SizeKB-f.SpaceUsedKB, f.SizeKB))
			minimum.SetText(fmt.Sprintf("Minimum is %d MB — the space the file's data occupies. A smaller target, 0 included, shrinks as far as that.", shrinkFileMinMB(f)))
		} else {
			free.SetValue("unknown")
			minimum.SetText("The file's used space could not be read, so its minimum size is unknown.")
		}
		// SSMS starts the target at the current size, so a shrink happens only
		// once a smaller one is typed.
		target.SetValue(fmt.Sprint(f.SizeKB / 1024))
	}
	pickFiles := func() {
		var group string
		if g := groupRow.Items(); groupRow.Selected() >= 0 && groupRow.Selected() < len(g) {
			group = g[groupRow.Selected()]
		}
		candidates = picker.filesIn(typeDesc(), group)
		names := make([]string, len(candidates))
		for i, f := range candidates {
			names[i] = f.Name
		}
		fileRow.SetItems(names)
		fileRow.SetSelected(0)
		showFile()
	}
	pickGroups := func() {
		groupRow.SetItems(picker.fileGroups(typeDesc()))
		groupRow.SetSelected(0)
		groupRow.SetEnabled(typeDesc() != "LOG")
		pickFiles()
	}
	typeRow.SetOnChange(func(string) { pickGroups() })
	groupRow.SetOnChange(func(string) { pickFiles() })
	fileRow.SetOnChange(func(string) { showFile() })
	action.SetOnChange(func(i int) { target.SetEnabled(i == shrinkFileReorganize) })
	pickGroups()

	rows := []propsheet.Row{
		propsheet.Note("The size of the database is reduced by shrinking individual files to release unallocated space. To shrink all database files, use Shrink > Database instead."),
		propsheet.Static("Database", dbName),
		propsheet.Section("Database files and filegroups"),
		typeRow, groupRow, fileRow, location,
		allocated, free,
		// The radio group draws its own "Shrink action" caption, so no
		// Section above it.
		action,
		target,
		minimum,
	}
	rows = append(rows, waitRows...)
	rows = append(rows, propsheet.Note("Script opens the DBCC SHRINKFILE statement in a new query window; nothing runs until you execute it there. Emptying a file needs other files in its filegroup to take its data."))

	d.forms[0] = propsheet.NewForm(rows...)
	d.objectName = func() string {
		if f := current(); f != nil {
			return f.Name
		}
		return ""
	}
	opts := func() (string, gosmo.ShrinkFileOptions, error) {
		f := current()
		if f == nil {
			return "", gosmo.ShrinkFileOptions{}, fmt.Errorf("pick a file to shrink")
		}
		o := gosmo.ShrinkFileOptions{Wait: wait()}
		switch action.Selected() {
		case shrinkFileRelease:
			o.Truncate = gosmo.ShrinkTruncateOnly
		case shrinkFileReorganize:
			mb, err := target.IntValue()
			if maxMB := f.SizeKB / 1024; err != nil || mb < 0 || mb > maxMB {
				return "", o, fmt.Errorf("shrink file to must be a whole number of MB from 0 to %d, the file's current size, not %q",
					maxMB, strings.TrimSpace(target.Value()))
			}
			o.TargetMB = new(mb)
		case shrinkFileEmpty:
			o.EmptyFile = true
		}
		return f.Name, o, nil
	}
	// preflight reads the rows on the UI goroutine and leaves the request for
	// the apply, which runs on the pipeline's.
	var name string
	var req gosmo.ShrinkFileOptions
	d.preflight = func() error {
		var err error
		name, req, err = opts()
		return err
	}
	d.applyFns[0] = func(ctx context.Context) error {
		return sc.Server.DatabaseRef(dbName).FileRef(name).ShrinkWith(ctx, req)
	}
}

// showShrinkFilesDialog opens Shrink File for one database.
func (a *App) showShrinkFilesDialog(sc *db.ServerConn, dbName string) {
	if !a.requireConn(sc) {
		return
	}
	a.shrinkFilesDialog.show(sc, dbName)
}
