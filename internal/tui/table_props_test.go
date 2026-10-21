package tui

import (
	"testing"

	gosmo "github.com/radix29/gosmo"
)

// TestTableOwnerTextMarksAnInheritedOwner: Table Properties' Owner names the
// table's own owner as is, and says when the name is the schema's owner the
// table inherits (review plan X2).
func TestTableOwnerTextMarksAnInheritedOwner(t *testing.T) {
	for _, c := range []struct {
		d    gosmo.TableDetail
		want string
	}{
		{gosmo.TableDetail{Owner: "x2_owner"}, "x2_owner"},
		{gosmo.TableDetail{Owner: "dbo", OwnedBySchemaOwner: true}, "dbo (schema owner)"},
	} {
		if got := tableOwnerText(&c.d); got != c.want {
			t.Errorf("tableOwnerText(%+v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// A hypothetical (tuning advisor) index is counted but called out: it has no
// data, so a bare count overstated what the table carries.
func TestIndexCountTextCallsOutHypothetical(t *testing.T) {
	real, hypo := &gosmo.Index{Name: "IX"}, &gosmo.Index{Name: "IX_dta", IsHypothetical: true}
	for _, c := range []struct {
		idxs []*gosmo.Index
		want string
	}{
		{nil, "0"},
		{[]*gosmo.Index{real}, "1"},
		{[]*gosmo.Index{real, hypo, hypo}, "3 (2 hypothetical)"},
	} {
		if got := indexCountText(c.idxs); got != c.want {
			t.Errorf("indexCountText(%d indexes) = %q, want %q", len(c.idxs), got, c.want)
		}
	}
}
