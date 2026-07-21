package tui

import (
	"strconv"
	"strings"

	"github.com/rivo/tview"
	"github.com/techthos/leadzaar/internal/models"
)

// formField is one labeled input, its inline error line, and its validator.
type formField struct {
	label    string
	item     tview.FormItem
	errTV    *tview.TextView
	validate func(value string) string // returns "" when valid
	value    func() string
}

// formView is a full-screen create/edit form rendered inside the body. It does
// live per-field validation (inline red errors), saves on Ctrl-S (blocked while
// any field is invalid), and cancels on Esc (with a discard prompt when dirty).
// Form-level keys (Tab/Shift-Tab/Ctrl-S/Esc) are delivered by the tui's global
// input capture, since tview only routes events to the focused primitive.
type formView struct {
	root   *tview.Flex
	footer *tview.TextView
	t      *tui

	fields []*formField
	order  []tview.Primitive // focus cycle
	save   func(values map[string]string)
	back   func()
	dirty  bool
}

// newForm starts a form titled title; back returns to the originating screen.
func newForm(t *tui, title string, back func()) *formView {
	f := &formView{t: t, back: back}
	f.footer = tview.NewTextView().SetDynamicColors(true)
	f.root = tview.NewFlex().SetDirection(tview.FlexRow)
	f.root.SetBorder(true).SetTitle(" " + title + " · Ctrl-S save · Esc cancel ")
	return f
}

func newErrTV() *tview.TextView {
	return tview.NewTextView().SetDynamicColors(true)
}

// Acceptance functions constraining numeric input fields.
var (
	acceptInt   = tview.InputFieldInteger
	acceptFloat = tview.InputFieldFloat
)

// append wires a field into the form: input row, then its inline error row.
func (f *formView) append(ff *formField) {
	f.appendSized(ff, 1)
}

// appendSized wires a field into the form reserving height rows for the input
// (1 for single-line fields; more for a multi-line text area).
func (f *formView) appendSized(ff *formField, height int) {
	f.fields = append(f.fields, ff)
	f.order = append(f.order, ff.item)
	f.root.AddItem(ff.item, height, 0, len(f.fields) == 1)
	f.root.AddItem(ff.errTV, 1, 0, false)
}

// input adds a single-line text field; validate may be nil.
func (f *formView) input(label, initial string, validate func(string) string) {
	in := tview.NewInputField().SetLabel(label + ": ").SetText(initial).SetFieldWidth(40)
	ff := &formField{label: label, item: in, errTV: newErrTV(), validate: validate, value: in.GetText}
	in.SetChangedFunc(func(string) { f.dirty = true; f.revalidate(ff) })
	f.append(ff)
}

// numInput adds a numeric field constrained to the given acceptance func.
func (f *formView) numInput(label, initial string, accept func(string, rune) bool, validate func(string) string) {
	in := tview.NewInputField().SetLabel(label + ": ").SetText(initial).SetFieldWidth(40)
	in.SetAcceptanceFunc(accept)
	ff := &formField{label: label, item: in, errTV: newErrTV(), validate: validate, value: in.GetText}
	in.SetChangedFunc(func(string) { f.dirty = true; f.revalidate(ff) })
	f.append(ff)
}

// dropdown adds a constrained choice; its value is always valid.
func (f *formView) dropdown(label string, opts []string, current int) {
	dd := tview.NewDropDown().SetLabel(label+": ").SetOptions(opts, nil).SetCurrentOption(current)
	ff := &formField{label: label, item: dd, errTV: newErrTV(), value: func() string {
		_, o := dd.GetCurrentOption()
		return o
	}}
	dd.SetSelectedFunc(func(string, int) { f.dirty = true })
	f.append(ff)
}

// companyPicker adds a dropdown for linking a Company. The first option is a
// "— none —" entry mapping to ID 0; value() returns the selected company ID as a
// decimal string (resolved via a parallel ID slice so duplicate names are
// unambiguous). currentID pre-selects the linked company on edit.
func (f *formView) companyPicker(label string, companies []models.Company, currentID uint64) {
	opts := make([]string, 0, len(companies)+1)
	ids := make([]uint64, 0, len(companies)+1)
	opts = append(opts, "— none —")
	ids = append(ids, 0)
	current := 0
	for i, c := range companies {
		opts = append(opts, c.Name)
		ids = append(ids, c.ID)
		if c.ID == currentID {
			current = i + 1
		}
	}
	dd := tview.NewDropDown().SetLabel(label+": ").SetOptions(opts, nil).SetCurrentOption(current)
	ff := &formField{label: label, item: dd, errTV: newErrTV(), value: func() string {
		idx, _ := dd.GetCurrentOption()
		if idx < 0 || idx >= len(ids) {
			return "0"
		}
		return strconv.FormatUint(ids[idx], 10)
	}}
	dd.SetSelectedFunc(func(string, int) { f.dirty = true })
	f.append(ff)
}

// textArea adds a multi-line text field for long content (e.g. a raw email
// body). It reserves rows lines of height; validate may be nil.
func (f *formView) textArea(label, initial string, rows int, validate func(string) string) {
	ta := tview.NewTextArea().SetLabel(label+": ").SetText(initial, false)
	ff := &formField{label: label, item: ta, errTV: newErrTV(), validate: validate, value: ta.GetText}
	ta.SetChangedFunc(func() { f.dirty = true; f.revalidate(ff) })
	f.appendSized(ff, rows)
}

// checkbox adds a boolean field; value() reports "true"/"false".
func (f *formView) checkbox(label string, checked bool) {
	cb := tview.NewCheckbox().SetLabel(label + ": ").SetChecked(checked)
	ff := &formField{label: label, item: cb, errTV: newErrTV(), value: func() string {
		return strconv.FormatBool(cb.IsChecked())
	}}
	cb.SetChangedFunc(func(bool) { f.dirty = true })
	f.append(ff)
}

// onSave registers the save action and appends the key-hint footer.
func (f *formView) onSave(save func(values map[string]string)) {
	f.save = save
	f.footer.SetText("[" + colorDim + "]Ctrl-S save · Tab next field · Esc cancel[-]")
	f.root.AddItem(f.footer, 1, 0, false)
}

// revalidate updates a single field's inline error line.
func (f *formView) revalidate(ff *formField) {
	if ff.validate == nil {
		return
	}
	if msg := ff.validate(ff.value()); msg != "" {
		ff.errTV.SetText("[" + colorError + "]" + msg + "[-]")
	} else {
		ff.errTV.SetText("")
	}
}

// trySave validates every field; on success it invokes save, otherwise it shows
// the errors and focuses the first offending field.
func (f *formView) trySave() {
	first := -1
	for i, ff := range f.fields {
		f.revalidate(ff)
		if ff.validate != nil && ff.validate(ff.value()) != "" && first < 0 {
			first = i
		}
	}
	if first >= 0 {
		f.footer.SetText("[" + colorError + "]Fix the highlighted fields before saving.[-]")
		f.t.app.SetFocus(f.fields[first].item)
		return
	}
	values := make(map[string]string, len(f.fields))
	for _, ff := range f.fields {
		values[ff.label] = ff.value()
	}
	f.save(values)
}

// cancel backs out, prompting first when the form has unsaved edits.
func (f *formView) cancel() {
	if f.dirty {
		f.t.confirm("Discard changes?", "Discard your changes? [y/N]", false, f.back)
		return
	}
	f.back()
}

func (f *formView) focusNext() { f.moveFocus(1) }
func (f *formView) focusPrev() { f.moveFocus(-1) }

func (f *formView) moveFocus(delta int) {
	if len(f.order) == 0 {
		return
	}
	cur := f.t.app.GetFocus()
	idx := 0
	for i, p := range f.order {
		if p == cur {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(f.order)) % len(f.order)
	f.t.app.SetFocus(f.order[idx])
}

// --- shared field validators ---

// required rejects blank/whitespace input.
func required(field string) func(string) string {
	return func(v string) string {
		if strings.TrimSpace(v) == "" {
			return field + " is required"
		}
		return ""
	}
}

// dateValidator accepts a blank value (no date set) or a YYYY-MM-DD calendar
// date, matching the wire format of the date-only model fields.
func dateValidator(v string) string {
	if _, err := models.ParseDate(v); err != nil {
		return "Use YYYY-MM-DD (or leave blank)"
	}
	return ""
}

// qualityValidator accepts a blank value (unscored) or an integer in 1–10.
func qualityValidator(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return ""
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 10 {
		return "Quality must be 1–10 (or blank)"
	}
	return ""
}

// parseFloat parses a money input; a blank or invalid value is 0.
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

// parseUint parses an ID input; blank/invalid is 0.
func parseUint(s string) uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
