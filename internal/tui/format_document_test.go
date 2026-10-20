package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/radix29/gossms/internal/format"
)

// newFormatTestPanel opens a query panel holding text in language l.
func newFormatTestPanel(t *testing.T, l format.Language, text string) (*App, *QueryPanel) {
	t.Helper()
	a := newKeyTestApp(t)
	a.newQueryPanel()
	qp := a.activeQueryPanel()
	qp.setLanguage(l)
	qp.editor.SetText(text)
	return a, qp
}

// Format Document re-indents the whole JSON document as one undo step: one
// Ctrl+Z restores the exact previous text. Alt+F and Alt+Shift+F both run it.
func TestFormatDocumentWholeJSONIsOneUndoStep(t *testing.T) {
	const src = `{"a":[1,2],"b":{}}`
	const want = "{\n    \"a\": [\n        1,\n        2\n    ],\n    \"b\": {}\n}"
	for _, ev := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, "f", tcell.ModAlt),
		tcell.NewEventKey(tcell.KeyRune, "F", tcell.ModAlt|tcell.ModShift),
	} {
		a, qp := newFormatTestPanel(t, format.LangJSON, src)
		a.handleKey(ev)
		if got := qp.editor.Text(); got != want {
			t.Fatalf("%s: text = %q, want %q (status %q)", ev.Name(), got, want, a.statusText)
		}
		if !strings.HasPrefix(a.statusText, "Formatted as JSON") {
			t.Errorf("%s: status = %q", ev.Name(), a.statusText)
		}
		qp.editor.Undo()
		if got := qp.editor.Text(); got != src {
			t.Fatalf("%s: after one undo text = %q, want %q", ev.Name(), got, src)
		}
	}
}

// An XML panel formats too, with the editor's indent width, as one undo step;
// a selection inside a document formats just its element, and the error for
// half an element reports document lines.
func TestFormatDocumentXML(t *testing.T) {
	const src = `<a x="1"><b>t</b><c/></a>`
	a, qp := newFormatTestPanel(t, format.LangXML, src)
	if !a.canFormatActiveDocument() {
		t.Fatal("disabled on an XML panel")
	}
	a.formatActiveDocument()
	want := "<a x=\"1\">\n" + strings.Repeat(" ", qp.editor.IndentWidth()) + "<b>t</b>\n" +
		strings.Repeat(" ", qp.editor.IndentWidth()) + "<c/>\n</a>"
	if got := qp.editor.Text(); got != want || a.statusText != "Formatted as XML (Standard)" {
		t.Fatalf("text = %q, status = %q; want %q", got, a.statusText, want)
	}
	qp.editor.Undo()
	if got := qp.editor.Text(); got != src {
		t.Fatalf("after one undo text = %q, want %q", got, src)
	}

	a, qp = newFormatTestPanel(t, format.LangXML, "<root>\n  <p><q/></p>\n  <r/>\n</root>")
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyEnd, "", tcell.ModShift))
	a.formatActiveDocument()
	ind := strings.Repeat(" ", qp.editor.IndentWidth())
	want = "<root>\n  <p>\n  " + ind + "<q/>\n  </p>\n  <r/>\n</root>"
	if got := qp.editor.Text(); got != want {
		t.Fatalf("selection: text = %q, want %q (status %q)", got, want, a.statusText)
	}

	a, qp = newFormatTestPanel(t, format.LangXML, "<root>\n  <p>\n  </p>\n</root>")
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyEnd, "", tcell.ModShift))
	a.formatActiveDocument()
	if a.statusText != "Format failed: selection is not a complete XML value: line 2 col 3: <p> is never closed" {
		t.Errorf("half an element: status = %q", a.statusText)
	}
}

// Invalid JSON leaves the text and the panel's clean state alone and says
// where it failed.
func TestFormatDocumentParseErrorLeavesTextUntouched(t *testing.T) {
	const src = "{\n\"a\": 1,\n\"b\" 2}"
	a, qp := newFormatTestPanel(t, format.LangJSON, src)
	qp.markSaved()
	a.formatActiveDocument()
	if got := qp.editor.Text(); got != src {
		t.Fatalf("text changed to %q", got)
	}
	if qp.Dirty() {
		t.Error("a failed format marked the panel dirty")
	}
	if !strings.HasPrefix(a.statusText, "Format failed: JSON line 3 col") {
		t.Errorf("status = %q", a.statusText)
	}
}

// With a selection only its whole lines are formatted, re-indented at the
// first line's indent; the lines around it are untouched. A selection that is
// not a complete value reports document line numbers.
func TestFormatDocumentSelectionLines(t *testing.T) {
	a, qp := newFormatTestPanel(t, format.LangJSON, "before\n  {\"a\":1,\n  \"b\":[]}\nafter")
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyRight, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModShift))
	a.formatActiveDocument()
	want := "before\n  {\n      \"a\": 1,\n      \"b\": []\n  }\nafter"
	if got := qp.editor.Text(); got != want {
		t.Fatalf("text = %q, want %q (status %q)", got, want, a.statusText)
	}
	// The replacement stays selected, so a repeat is a no-op.
	a.formatActiveDocument()
	if got := qp.editor.Text(); got != want || !strings.HasPrefix(a.statusText, "Already formatted") {
		t.Fatalf("repeat: text = %q, status = %q", got, a.statusText)
	}

	a, qp = newFormatTestPanel(t, format.LangJSON, "x\n{\"a\":\n1,")
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModShift))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyEnd, "", tcell.ModShift))
	a.formatActiveDocument()
	if !strings.HasPrefix(a.statusText, "Format failed: selection is not a complete JSON value: line 3 ") {
		t.Errorf("status = %q", a.statusText)
	}
}

// The command is offered only where it can act: a writable query panel whose
// language has a formatter, with no block selection.
func TestFormatDocumentEnabled(t *testing.T) {
	a, qp := newFormatTestPanel(t, format.LangJSON, "{}")
	if !a.canFormatActiveDocument() {
		t.Fatal("disabled on a JSON panel")
	}
	qp.editor.SetReadOnly(true)
	if a.canFormatActiveDocument() {
		t.Error("enabled on a read-only editor")
	}
	a.formatActiveDocument()
	if !strings.Contains(a.statusText, "read-only") {
		t.Errorf("read-only status = %q", a.statusText)
	}

	a, qp = newFormatTestPanel(t, format.LangText, "select 1")
	if a.canFormatActiveDocument() {
		t.Error("enabled for plain text, which has no formatter")
	}
	a.formatActiveDocument()
	if qp.editor.Text() != "select 1" || !strings.Contains(a.statusText, "no formatter for Text") {
		t.Errorf("Text: text = %q, status = %q", qp.editor.Text(), a.statusText)
	}

	a = newKeyTestApp(t)
	if a.canFormatActiveDocument() {
		t.Error("enabled with no query panel")
	}
	a.formatActiveDocument() // must not panic
}

// A SQL panel formats as one undo step, and the status names the style.
func TestFormatDocumentSQL(t *testing.T) {
	const src = "select a, b from t where x = 1"
	a, qp := newFormatTestPanel(t, format.LangSQL, src)
	if !a.canFormatActiveDocument() {
		t.Fatal("disabled on a SQL panel")
	}
	a.formatActiveDocument()
	ind := strings.Repeat(" ", qp.editor.IndentWidth())
	lead := strings.Repeat(" ", qp.editor.IndentWidth()-2) + ", "
	want := "SELECT\n" + ind + "a\n" + lead + "b\nFROM t\nWHERE x = 1"
	if got := qp.editor.Text(); got != want || a.statusText != "Formatted as SQL (Standard)" {
		t.Fatalf("text = %q, status = %q; want %q", got, a.statusText, want)
	}
	qp.editor.Undo()
	if got := qp.editor.Text(); got != src {
		t.Fatalf("after one undo text = %q, want %q", got, src)
	}
}

// A selection is re-indented at its first line's indent by the formatter
// itself, so the second line of a multi-line literal — part of the value —
// keeps exactly the text it had. Prefixing every line afterwards changed it.
func TestFormatDocumentSQLSelectionKeepsLiteralLines(t *testing.T) {
	a, qp := newFormatTestPanel(t, format.LangSQL, "begin\n  print 'a\nb'; select 1\nend")
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModShift))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyEnd, "", tcell.ModShift))
	a.formatActiveDocument()
	want := "begin\n  PRINT 'a\nb';\n  SELECT 1\nend"
	if got := qp.editor.Text(); got != want {
		t.Fatalf("text = %q, want %q (status %q)", got, want, a.statusText)
	}

	a, qp = newFormatTestPanel(t, format.LangSQL, "select 1\nselect 'x")
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyDown, "", tcell.ModNone))
	qp.editor.HandleKey(tcell.NewEventKey(tcell.KeyEnd, "", tcell.ModShift))
	a.formatActiveDocument()
	if want := "Format failed: selection does not lex as SQL: line 2 col 8: unterminated string literal"; a.statusText != want {
		t.Errorf("status = %q, want %q", a.statusText, want)
	}
}
