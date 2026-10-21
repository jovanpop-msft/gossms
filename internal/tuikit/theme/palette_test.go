package theme

import (
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v3"
)

// SetPalette must be seen through Active's pointer; returning a copy would
// leave callers reading the old colours.
func TestSetPaletteReplacesWhatActiveReads(t *testing.T) {
	saved := *Active()
	defer SetPalette(saved)

	before := Active()
	custom := saved
	custom.Background = tcell.NewRGBColor(1, 2, 3)
	custom.MenuBar = tcell.NewRGBColor(4, 5, 6)
	SetPalette(custom)

	if got := Active().Background; got != custom.Background {
		t.Errorf("Active().Background = %v, want %v", got, custom.Background)
	}
	if got := before.MenuBar; got != custom.MenuBar {
		t.Errorf("a pointer taken before SetPalette reads MenuBar = %v, want %v", got, custom.MenuBar)
	}
	if got := StyleMenuBar(); got != tcell.StyleDefault.Background(custom.MenuBar).Foreground(custom.Text) {
		t.Errorf("StyleMenuBar did not follow the new palette: %v", got)
	}

	SetPalette(saved)
	if got := Active().Background; got != saved.Background {
		t.Errorf("after restoring: Background = %v, want %v", got, saved.Background)
	}
}

// Each palette must set every colour: a field left zero renders as the terminal's
// default and silently mixes the user's terminal theme into the UI.
func TestPalettesSetEveryColour(t *testing.T) {
	v := reflect.ValueOf(Light)
	for i := range v.NumField() {
		if v.Field(i).Interface().(tcell.Color) == tcell.ColorDefault {
			t.Errorf("Light.%s is unset", v.Type().Field(i).Name)
		}
	}
	d := reflect.ValueOf(Default)
	for i := range d.NumField() {
		if d.Field(i).Interface().(tcell.Color) == tcell.ColorDefault {
			t.Errorf("Default.%s is unset", d.Type().Field(i).Name)
		}
	}
}
