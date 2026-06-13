package tui

import (
	"strconv"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/techthos/microapp-crm/internal/models"
)

// newDealsScreen builds the deals table and key handling (UC-13,14,16,17).
func (t *tui) newDealsScreen() *tview.Table {
	table := newListTable("Deals — n new · enter edit · s stage · d delete")
	table.SetSelectedFunc(func(row, _ int) {
		if d, ok := t.selectedDeal(row); ok {
			t.showDealForm(&d)
		}
	})
	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		row, _ := table.GetSelection()
		switch ev.Rune() {
		case 'n':
			t.showDealForm(nil)
			return nil
		case 's':
			if d, ok := t.selectedDeal(row); ok {
				t.showStagePicker(d)
			}
			return nil
		case 'd':
			if d, ok := t.selectedDeal(row); ok {
				t.mutate(pageDeals, t.dealsTable, func() error { return t.store.DeleteDeal(d.ID) })
			}
			return nil
		}
		return ev
	})
	return table
}

func (t *tui) selectedDeal(row int) (models.Deal, bool) {
	idx := row - 1
	if idx < 0 || idx >= len(t.deals) {
		return models.Deal{}, false
	}
	return t.deals[idx], true
}

// showDealForm opens the create/edit deal form (UC-13,16).
func (t *tui) showDealForm(existing *models.Deal) {
	d := models.Deal{Stage: models.StageQualification}
	title := "New Deal"
	if existing != nil {
		d = *existing
		title = "Edit Deal"
	}
	stages := stageOptions()

	form := tview.NewForm()
	form.AddInputField("Title", d.Title, 32, nil, nil)
	form.AddInputField("Contact ID", contactIDText(d.ContactID), 12, tview.InputFieldInteger, nil)
	form.AddInputField("Value", formatMoney(d.Value), 16, tview.InputFieldFloat, nil)
	form.AddInputField("Currency", d.Currency, 8, nil, nil)
	form.AddDropDown("Stage", stages, indexOf(stages, string(d.Stage)), nil)
	form.AddInputField("Notes", d.Notes, 32, nil, nil)
	form.AddButton("Save", func() {
		base := d
		base.Title = formText(form, "Title")
		base.ContactID = parseUint(formText(form, "Contact ID"))
		base.Value = parseFloat(formText(form, "Value"))
		base.Currency = formText(form, "Currency")
		base.Stage = models.DealStage(formDropdown(form, "Stage"))
		base.Notes = formText(form, "Notes")
		t.mutate(pageDeals, t.dealsTable, func() error {
			if base.ID == 0 {
				_, err := t.store.CreateDeal(base)
				return err
			}
			_, err := t.store.UpdateDeal(base)
			return err
		})
	})
	form.AddButton("Cancel", func() { t.closeOverlay(pageDeals, t.dealsTable) })
	form.SetButtonsAlign(tview.AlignCenter)
	form.SetBorder(true).SetTitle(" " + title + " · Esc cancels ")
	form.SetCancelFunc(func() { t.closeOverlay(pageDeals, t.dealsTable) })
	t.showOverlay(form, 52, 18)
}

// showStagePicker advances a deal's stage via a quick modal (UC-16).
func (t *tui) showStagePicker(d models.Deal) {
	stages := stageOptions()
	modal := tview.NewModal().
		SetText("Move deal \"" + d.Title + "\" to which stage?").
		AddButtons(append(stages, "Cancel")).
		SetDoneFunc(func(_ int, label string) {
			if label == "Cancel" || label == "" {
				t.closeOverlay(pageDeals, t.dealsTable)
				return
			}
			updated := d
			updated.Stage = models.DealStage(label)
			t.mutate(pageDeals, t.dealsTable, func() error {
				_, err := t.store.UpdateDeal(updated)
				return err
			})
		})
	t.showModal(modal)
}

// contactIDText renders a contact id for an input field (blank when unset).
func contactIDText(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// parseUint parses a contact-id input; blank/invalid is 0.
func parseUint(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
