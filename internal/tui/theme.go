package tui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// themeOnce guards the single global theme assignment so concurrent app
// constructions (notably parallel tests) don't race on tview.Styles.
var themeOnce sync.Once

// Semantic color vocabulary — used consistently across every screen so the app
// reads the same way everywhere (see tui-rules.md "Color & markup semantics").
const (
	colorSuccess = "green"  // success / healthy
	colorError   = "red"    // error / destructive
	colorWarn    = "yellow" // warning / attention
	colorDim     = "gray"   // disabled / placeholder / empty
)

// applyTheme sets the single global tview theme. It must run once at startup,
// before any widget is created.
func applyTheme() {
	themeOnce.Do(func() { tview.Styles = theme() })
}

func theme() tview.Theme {
	return tview.Theme{
		PrimitiveBackgroundColor:    tcell.ColorDefault,
		ContrastBackgroundColor:     tcell.ColorBlue,
		MoreContrastBackgroundColor: tcell.ColorGreen,
		BorderColor:                 tcell.ColorGray,
		TitleColor:                  tcell.ColorWhite,
		GraphicsColor:               tcell.ColorGray,
		PrimaryTextColor:            tcell.ColorWhite,
		SecondaryTextColor:          tcell.ColorYellow,
		TertiaryTextColor:           tcell.ColorGreen,
		InverseTextColor:            tcell.ColorBlack,
		ContrastSecondaryTextColor:  tcell.ColorBlack,
	}
}
