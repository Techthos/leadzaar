package tui

import (
	"github.com/rivo/tview"
)

// helpText is the full keybinding reference shown by the `?` overlay. The status
// bar only surfaces the few keys relevant to the focused screen; this lists them
// all (see tui-rules.md "Keybindings").
const helpText = `[::b]microapp-crm — keys[::-]

[::b]Navigation[::-]
  1–6         jump to a section (Dashboard, Leads, Contacts, Deals, Companies, Offers)
  ↑ ↓ / j k   move selection
  Tab         cycle sidebar ↔ table ↔ detail
  Ctrl-B      toggle the sidebar
  Esc         back / cancel / clear filter

[::b]Lists[::-]
  Enter       open record
  n           new          e  edit          d  delete
  r           reload       /  filter        Space  toggle row select

[::b]Per screen[::-]
  c           convert a lead        s  change a deal's stage
  o           new offer for a lead  l  go to the offer's lead (Offers)

[::b]Forms[::-]
  Ctrl-S      save         Esc  cancel (prompts if changed)

[::b]Global[::-]
  ?           toggle this help      q / Ctrl-C  quit

[gray]press ? or Esc to close[-]`

// showHelp layers the centered help overlay over the body.
func (t *tui) showHelp() {
	tv := tview.NewTextView().SetDynamicColors(true).SetText(helpText)
	tv.SetBorder(true).SetTitle(" Help ")
	t.prevOverlay = t.overlay
	t.overlay = ovHelp
	t.body.AddPage(pageOverlay, centeredModalBox(tv, 64, 24), true, true)
	t.app.SetFocus(tv)
}

// centeredModalBox floats p at a fixed size in the middle of its region.
func centeredModalBox(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}
