package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/radix29/gossms/internal/format"
	"github.com/radix29/gossms/internal/tuikit/controls"
)

// canFormatActiveDocument gates Edit > Format Document (and Alt+F): a query
// panel whose editor is writable, whose language has a formatter, and with no
// block selection — a column span has no lines to rewrite.
func (a *App) canFormatActiveDocument() bool {
	qp := a.activeQueryPanel()
	return qp != nil && !qp.editor.ReadOnly() && !qp.editor.HasBlockSelection() &&
		len(format.Styles(qp.lang)) > 0
}

// formatSyncBytes is the largest text Format Document formats on the UI
// goroutine. SQL, the slowest formatter, takes about 55 ms on 256 KB and 230
// ms on 1 MB (i5-2500K); a 5 MB script froze every key and redraw for 1.7 s.
// Below this a "Formatting..." flash would cost more than it saves.
var formatSyncBytes = 256 << 10

// formatText is format.Format; a variable so a test can make it slow.
var formatText = format.Format

// formatActiveDocument is Edit > Format Document: it lays out the active query
// panel's text in its language's default style, as one undo step. With a
// selection it formats the whole lines the selection touches, re-indented at
// the first line's indent; otherwise the whole document. A parse failure or a
// guard refusal leaves the text untouched and says why on the status bar.
//
// Text over formatSyncBytes is formatted off the UI goroutine. Its result
// lands only if the panel's formatRun is still current (a repeat supersedes
// it, closing the panel abandons it) and the text and selection are as they
// were when it started; otherwise the typing done meanwhile wins.
func (a *App) formatActiveDocument() {
	qp := a.activeQueryPanel()
	if qp == nil {
		return
	}
	e := qp.editor
	st, ok := format.StyleByID(qp.lang, "")
	switch {
	case e.ReadOnly():
		a.setStatus("Format Document: the editor is read-only")
		return
	case !ok:
		a.setStatus(fmt.Sprintf("Format Document: no formatter for %s", qp.lang))
		return
	case e.HasBlockSelection():
		a.setStatus("Format Document: not available for a block selection")
		return
	}
	j := formatJob{qp: qp, st: st, lang: qp.lang, doc: e.Document(), ver: e.Document().Version(),
		sel: e.HasSelection(), top: e.SelectionTopRow()}
	o := format.Options{IndentWidth: e.IndentWidth()}
	if j.sel {
		j.src, _ = e.SelectedLinesText()
		// The formatter indents the lines it lays out, not those inside a
		// multi-line literal or text value, before its guard checks the result.
		o.LineIndent = leadingIndent(j.src)
	} else {
		j.src = e.Text()
	}

	if len(j.src) <= formatSyncBytes {
		qp.formatRun.Abandon() // this one is newer than any still running
		out, err := formatText(j.lang, st.ID, j.src, o)
		a.applyFormatted(j, out, err)
		return
	}
	// The formatter takes no context, so a superseded run still finishes; the
	// latest only drops its result.
	_, token := qp.formatRun.Begin(context.Background())
	a.setStatus(fmt.Sprintf("Formatting as %s...", j.lang))
	repair := func() { qp.formatRun.Done(token) }
	a.safegoRepair("Format Document", repair, func() {
		out, err := formatText(j.lang, st.ID, j.src, o)
		a.postAndWake(func() {
			if !qp.formatRun.Done(token) {
				return
			}
			if !j.current() {
				a.setStatus("Format Document: the text changed while formatting; not applied")
				return
			}
			a.applyFormatted(j, out, err)
		})
	})
}

// formatJob is what one Format Document run read from its panel, enough to
// apply its result and to tell whether the editor still holds that text.
type formatJob struct {
	qp   *QueryPanel
	st   format.Style
	lang format.Language
	src  string // the text formatted: the whole document or the selected lines
	doc  *controls.Document
	ver  uint64
	sel  bool
	top  int // the selection's first line
}

// current reports whether j's editor still holds the text j formatted, at the
// same place and selected the same way, so applying the result replaces
// exactly what was formatted. The document version catches any edit; a moved
// selection leaves it alone, so that is compared too.
func (j formatJob) current() bool {
	e := j.qp.editor
	if e.ReadOnly() || j.qp.lang != j.lang || e.Document() != j.doc || e.Document().Version() != j.ver ||
		e.HasSelection() != j.sel {
		return false
	}
	if !j.sel {
		return true
	}
	src, ok := e.SelectedLinesText()
	return ok && e.SelectionTopRow() == j.top && src == j.src
}

// applyFormatted puts a run's result into its editor, or reports its error.
func (a *App) applyFormatted(j formatJob, out string, err error) {
	e := j.qp.editor
	if err != nil {
		// Positions count from the selection's first line; report document lines.
		if se, ok := errors.AsType[*format.SyntaxError](err); ok && j.sel {
			what := fmt.Sprintf("is not a complete %s value", se.Lang)
			if se.Lang == format.LangSQL {
				what = "does not lex as SQL" // any run of statements is a whole SQL "value"
			}
			a.setStatus(fmt.Sprintf("Format failed: selection %s: line %d col %d: %s",
				what, se.Line+j.top, se.Col, se.Msg))
			return
		}
		a.setStatus("Format failed: " + err.Error())
		return
	}
	if j.sel {
		a.reportFormatted(j.lang, j.st, e.ReplaceSelectedLines(out))
		return
	}
	a.reportFormatted(j.lang, j.st, e.ReplaceText(out))
}

// reportFormatted is the status line after a successful format; changed is
// false when the text was already in the style (no undo step was pushed).
func (a *App) reportFormatted(l format.Language, st format.Style, changed bool) {
	if !changed {
		a.setStatus(fmt.Sprintf("Already formatted as %s (%s)", l, st.Name))
		return
	}
	a.setStatus(fmt.Sprintf("Formatted as %s (%s)", l, st.Name))
}

// leadingIndent is the run of spaces and tabs s starts with.
func leadingIndent(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
