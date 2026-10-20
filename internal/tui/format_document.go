package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/radix29/gossms/internal/format"
)

// canFormatActiveDocument gates Edit > Format Document (and Alt+F): a query
// panel whose editor is writable, whose language has a formatter, and with no
// block selection — a column span has no lines to rewrite.
func (a *App) canFormatActiveDocument() bool {
	qp := a.activeQueryPanel()
	return qp != nil && !qp.editor.ReadOnly() && !qp.editor.HasBlockSelection() &&
		len(format.Styles(qp.lang)) > 0
}

// formatActiveDocument is Edit > Format Document: it lays out the active query
// panel's text in its language's default style, as one undo step. With a
// selection it formats the whole lines the selection touches, re-indented at
// the first line's indent; otherwise the whole document. A parse failure or a
// guard refusal leaves the text untouched and says why on the status bar.
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
	o := format.Options{IndentWidth: e.IndentWidth()}

	if !e.HasSelection() {
		out, err := format.Format(qp.lang, st.ID, e.Text(), o)
		if err != nil {
			a.setStatus("Format failed: " + err.Error())
			return
		}
		a.reportFormatted(qp.lang, st, e.ReplaceText(out))
		return
	}

	src, _ := e.SelectedLinesText()
	// The formatter indents the lines it lays out, not those inside a
	// multi-line literal or text value, before its guard checks the result.
	o.LineIndent = leadingIndent(src)
	out, err := format.Format(qp.lang, st.ID, src, o)
	if err != nil {
		// Positions count from the selection's first line; report document lines.
		if se, ok := errors.AsType[*format.SyntaxError](err); ok {
			what := fmt.Sprintf("is not a complete %s value", se.Lang)
			if se.Lang == format.LangSQL {
				what = "does not lex as SQL" // any run of statements is a whole SQL "value"
			}
			a.setStatus(fmt.Sprintf("Format failed: selection %s: line %d col %d: %s",
				what, se.Line+e.SelectionTopRow(), se.Col, se.Msg))
			return
		}
		a.setStatus("Format failed: " + err.Error())
		return
	}
	a.reportFormatted(qp.lang, st, e.ReplaceSelectedLines(out))
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
