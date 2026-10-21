package tui

import (
	"slices"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/radix29/gossms/internal/config"
	"github.com/radix29/gossms/internal/db"
)

func TestConnectCertificateValidationReachesTheDriver(t *testing.T) {
	check := func(t *testing.T, d *ConnectDialog, wantTrust bool) {
		t.Helper()
		d.fServer.SetValue("sql.example.com")
		opts := d.currentOptions()
		if opts.Encrypt != config.EncryptMandatory || opts.TrustServerCertificate != wantTrust {
			t.Fatalf("Encrypt = %q, TrustServerCertificate = %v; want Mandatory and %v",
				opts.Encrypt, opts.TrustServerCertificate, wantTrust)
		}
		dsn, err := db.BuildConnectionString(opts)
		if err != nil {
			t.Fatal(err)
		}
		driver, err := msdsn.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		if driver.TLSConfig == nil {
			t.Fatal("Mandatory encryption did not configure TLS")
		}
		if driver.TLSConfig.InsecureSkipVerify != wantTrust {
			t.Fatalf("driver InsecureSkipVerify = %v, want %v", driver.TLSConfig.InsecureSkipVerify, wantTrust)
		}
	}
	t.Run("new form", func(t *testing.T) {
		check(t, NewConnectDialog(newTestApp()), false)
	})
	t.Run("explicit opt-in and reset", func(t *testing.T) {
		d := NewConnectDialog(newTestApp())
		d.Show()
		d.setFocus(indexOfFocusable(d.focusable, d.cbTrust))
		d.HandleKey(tcell.NewEventKey(tcell.KeyRune, " ", tcell.ModNone))
		check(t, d, true)
		d.btnFocus = connectBtnReset
		d.doButton()
		check(t, d, false)
	})
	for _, trust := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved validation", true: "saved opt-in"}[trust], func(t *testing.T) {
			d := NewConnectDialog(newTestApp())
			saved := config.Connection{
				Server: "sql.example.com", Encrypt: config.EncryptMandatory,
				TrustServerCertificate: trust,
			}
			d.PreFill(&saved)
			check(t, d, trust)
			if saved.TrustServerCertificate != trust {
				t.Fatal("PreFill changed the saved connection")
			}
		})
	}
}

// setEncryptMode's fallback used to be a tail call into itself with
// EncryptMandatory, which terminated only because AllEncryptModes happens to
// contain that mode. The two live in different packages and nothing pinned the
// dependency, so dropping or renaming a mode turned a hand-edited config.json
// into a stack overflow the moment the Connect dialog opened.
//
// Both halves are pinned: that an unknown mode still lands on Mandatory, and
// that it gets there without recursing. A test that only asserted the result
// would go on passing while the recursive form was reintroduced.
func TestSetEncryptModeFallsBackWithoutRecursing(t *testing.T) {
	for _, m := range []config.EncryptMode{"", "yes", "TRUE", "mandatory "} {
		t.Run(string(m), func(t *testing.T) {
			d := NewConnectDialog(newTestApp())
			d.setEncryptMode(m)
			got := config.AllEncryptModes()[d.ddEncrypt.Selected()]
			if got != config.EncryptMandatory {
				t.Errorf("an unknown mode %q selected %q, want %q", m, got, config.EncryptMandatory)
			}
		})
	}
}

// The fallback no longer depends on Mandatory being in the list, but the
// dialog's *meaning* still does: a list without it would silently connect
// under a weaker mode than the one the fallback names. Cheaper to pin than to
// rediscover.
func TestAllEncryptModesContainsMandatory(t *testing.T) {
	modes := config.AllEncryptModes()
	if len(modes) == 0 {
		t.Fatal("AllEncryptModes is empty, so the Connect dialog has no mode to select at all")
	}
	if !slices.Contains(modes, config.EncryptMandatory) {
		t.Errorf("AllEncryptModes %v no longer contains %q, which setEncryptMode falls back to",
			modes, config.EncryptMandatory)
	}
}
