package tui

import (
	"bufio"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/radix29/gossms/internal/fileutil"
	"github.com/radix29/gossms/internal/query"
)

// csvSink implements query.RowSink by writing each row to a CSV file as it
// arrives, so an export never holds more than one row in memory. Result sets are
// separated by a blank line, each preceded by its header row.
//
// A SQL NULL is written as an empty field and an empty string as "", so the file
// keeps the two apart, and the string 'NULL' as NULL: the convention of
// PostgreSQL's COPY CSV and most importers. encoding/csv never quotes an empty
// field, hence writeRecord. One limit is the format's: a row of one NULL column is
// written as "" too, since an empty line is skipped by readers and is the separator
// between sets, so there a NULL and an empty string are the same.
//
// The write path is deliberately dumb: no counting beyond what EndSet is handed,
// no buffering past bufio's own, nothing retained between rows.
type csvSink struct {
	st *fileutil.Stream
	w  *bufio.Writer

	// sets counts result sets begun so far, so the blank-line separator goes between
	// sets and not before the first.
	sets int
}

// newCSVSink returns a sink streaming to a temp file beside path. path itself is
// replaced only by Close, so a failed or stopped run (Abort) leaves what the user
// agreed to overwrite as it was.
func newCSVSink(path string) (*csvSink, error) {
	st, err := fileutil.CreateStream(path, 0o644)
	if err != nil {
		return nil, err
	}
	return &csvSink{st: st, w: bufio.NewWriter(st)}, nil
}

// writeRecord writes one CSV line: cells comma-separated, a NULL (isNull, which
// may be nil or shorter) as nothing, an empty string as "", and any other field
// quoted exactly where encoding/csv would quote it. bufio's error is sticky, so
// the last write's error is every write's.
func (s *csvSink) writeRecord(cells []string, isNull []bool) error {
	// A lone NULL would be a bare "\n", which readers (encoding/csv's included) skip
	// as a blank line and which is also the separator between sets. Write it as ""
	// the way encoding/csv quotes a lone empty field: for this one shape NULL and an
	// empty string are the same on disk.
	if len(cells) == 1 && len(isNull) > 0 && isNull[0] {
		s.w.WriteString(`""`)
		return s.w.WriteByte('\n')
	}
	for i, c := range cells {
		if i > 0 {
			s.w.WriteByte(',')
		}
		switch {
		case i < len(isNull) && isNull[i]:
		case c == "":
			s.w.WriteString(`""`)
		case csvNeedsQuotes(c):
			s.w.WriteByte('"')
			s.w.WriteString(strings.ReplaceAll(c, `"`, `""`))
			s.w.WriteByte('"')
		default:
			s.w.WriteString(c)
		}
	}
	return s.w.WriteByte('\n')
}

// csvNeedsQuotes is encoding/csv's rule for a non-empty field with ',' as the
// separator: a quote, the separator or a line break anywhere, a leading space,
// or the field \. (which a PostgreSQL reader takes as end of data).
func csvNeedsQuotes(field string) bool {
	if field == `\.` || strings.ContainsAny(field, "\",\r\n") {
		return true
	}
	r, _ := utf8.DecodeRuneInString(field)
	return unicode.IsSpace(r)
}

// BeginSet writes the separator (for every set after the first) and header.
func (s *csvSink) BeginSet(columns []string) error {
	if s.sets > 0 {
		if err := s.w.WriteByte('\n'); err != nil {
			return err
		}
	}
	s.sets++
	return s.writeRecord(columns, nil)
}

func (s *csvSink) Row(cells []string, isNull []bool) error { return s.writeRecord(cells, isNull) }

// EndSet flushes this set's rows so a long export reaches the disk as it goes
// rather than only at Close.
func (s *csvSink) EndSet(int) error { return s.w.Flush() }

// Close flushes the rows and replaces the destination with them. A flush error is
// preferred over a commit error since it names the actual failure; either way the
// destination is left as it was.
func (s *csvSink) Close() error {
	if err := s.w.Flush(); err != nil {
		s.st.Abort()
		return err
	}
	return s.st.Commit()
}

// Abort discards the rows written so far; the destination is not touched.
func (s *csvSink) Abort() { s.st.Abort() }

// finish ends a run: a stopped run or one that reported any error is aborted, so
// the destination the user agreed to overwrite is left as it was (kept), and
// anything else is committed by Close. The handle is released either way.
func (s *csvSink) finish(cancelled bool, res *query.Result) (kept bool, err error) {
	if cancelled || res.HasErrors() {
		s.Abort()
		return true, nil
	}
	return false, s.Close()
}

// promptResultsFile asks where a Results To File run should write, then hands the
// chosen path to run. Cancelling the dialog calls neither.
//
// The prompt comes *before* execution: rows are streamed as scanned (csvSink), so
// the destination has to be known by the time the query starts. This also matches SSMS, which asks for the filename when you execute in
// Results To File mode.
func (p *QueryPanel) promptResultsFile(run func(path string)) {
	p.app.fileDialog.ShowSave("Results To File", "results.csv", run)
}

// reportExport appends the outcome of a streamed export to res so it shows up
// in the Messages tab, and mirrors it to the status bar.
func (p *QueryPanel) reportExport(res *query.Result, path string, rows int, kept bool, err error) {
	msg := query.Message{Text: fmt.Sprintf("%d row(s) written to %s", rows, path)}
	if kept {
		msg = query.Message{Text: fmt.Sprintf("Results not written; %s was not changed", path)}
	}
	if err != nil {
		msg = query.Message{Text: fmt.Sprintf("write results to %s: %v", path, err), IsError: true}
	}
	res.Messages = append(res.Messages, msg)
	if p.result == res {
		p.renderActiveTab()
	}
	p.app.setStatus(msg.Text)
}
