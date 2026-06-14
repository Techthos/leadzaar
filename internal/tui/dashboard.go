package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/techthos/microapp-crm/internal/models"
)

// dashboardScreen is the read-only pipeline-summary section (UC-18). It is the
// landing section, reached like any other via the sidebar.
type dashboardScreen struct {
	t    *tui
	view *tview.TextView
	err  error
	rows int // funnel/stage lines, for a rough record count
}

func newDashboard(t *tui) *dashboardScreen {
	tv := tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	tv.SetBorder(true).SetTitle(" Pipeline summary ")
	d := &dashboardScreen{t: t, view: tv}
	d.wireKeys()
	return d
}

// set renders the latest summary (or an error state).
func (d *dashboardScreen) set(s models.PipelineSummary, err error) {
	d.err = err
	if err != nil {
		d.view.SetText("[" + colorError + "]" + err.Error() + "[-]\n\npress r to retry")
		d.rows = 0
		return
	}
	lines := summaryLines(s)
	d.rows = len(s.DealsByStage)
	d.view.SetText(strings.Join(lines, "\n"))
}

func (d *dashboardScreen) primitive() tview.Primitive { return d.view }

func (d *dashboardScreen) focus() { d.t.app.SetFocus(d.view) }

func (d *dashboardScreen) rowContext() string {
	if d.err != nil {
		return "error"
	}
	return "pipeline summary"
}

func (d *dashboardScreen) keyHints() string { return "r refresh" }

func (d *dashboardScreen) total() int { return 0 } // no record badge for the dashboard

func (d *dashboardScreen) focusables() []tview.Primitive {
	return []tview.Primitive{d.view}
}

// inputCapture lets `r` refresh the dashboard like the list screens.
func (d *dashboardScreen) wireKeys() {
	d.view.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		if ev.Rune() == 'r' {
			d.t.reload()
			return nil
		}
		return ev
	})
}
