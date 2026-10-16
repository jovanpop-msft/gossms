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
