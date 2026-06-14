package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/techthos/microapp-crm/internal/models"
)

// newDealsScreen builds the deals master-detail screen (UC-13,14,16,17). Beyond
// the shared keys it adds `s` to change the highlighted deal's stage.
func newDealsScreen(t *tui) *listScreen[models.Deal] {
	return newListScreen[models.Deal](t, screenConfig[models.Deal]{
		page:      pageDeals,
		cols:      dealCols,
		cells:     dealCells,
		detail:    dealDetail,
		id:        func(d models.Deal) uint64 { return d.ID },
		emptyHint: "No deals yet — press n to add one",
		hints:     "n new · e edit · s stage · d delete · / filter · r reload",
		newForm:   func() { t.showDealForm(nil) },
		editForm:  func(d models.Deal) { t.showDealForm(&d) },
		del:       t.deleteDeals,
		extra: func(ev *tcell.EventKey, sel models.Deal, ok bool) *tcell.EventKey {
			if ev.Rune() == 's' {
				if ok {
					t.showStagePicker(sel)
				}
				return nil
			}
			return ev
		},
	})
}

// deleteDeals deletes the targeted deals after a single batch confirm.
func (t *tui) deleteDeals(targets []models.Deal) {
	t.confirm("Delete deals", confirmDeleteText("deal", len(targets), targets[0].Title), true, func() {
		t.mutate(func() error {
			for _, d := range targets {
				if err := t.store.DeleteDeal(d.ID); err != nil {
					return err
				}
			}
			return nil
		})
	})
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

	f := newForm(t, title, t.closeForm)
	f.input("Title", d.Title, required("Title"))
	f.numInput("Contact ID", uintField(d.ContactID), acceptInt, required("Contact ID"))
	f.numInput("Value", formatMoney(d.Value), acceptFloat, nil)
	f.input("Currency", d.Currency, nil)
	f.dropdown("Stage", stages, indexOf(stages, string(d.Stage)))
	f.input("Notes", d.Notes, nil)
	// A non-zero value requires a currency (validation enforced at the store; we
	// surface it live here so save is blocked before the round-trip).
	f.onSave(func(v map[string]string) {
		base := d
		base.Title = v["Title"]
		base.ContactID = parseUint(v["Contact ID"])
		base.Value = parseFloat(v["Value"])
		base.Currency = v["Currency"]
		base.Stage = models.DealStage(v["Stage"])
		base.Notes = v["Notes"]
		t.mutate(func() error {
			if base.ID == 0 {
				_, err := t.store.CreateDeal(base)
				return err
			}
			_, err := t.store.UpdateDeal(base)
			return err
		})
	})
	t.openForm(f)
}

// showStagePicker advances a deal's stage via a quick modal (UC-16).
func (t *tui) showStagePicker(d models.Deal) {
	stages := stageOptions()
	modal := newModal("Move deal \""+d.Title+"\" to which stage?", append(stages, "Cancel"),
		func(_ int, label string) {
			if label == "Cancel" || label == "" {
				t.closeOverlay()
				return
			}
			t.closeOverlay()
			updated := d
			updated.Stage = models.DealStage(label)
			t.mutate(func() error {
				_, err := t.store.UpdateDeal(updated)
				return err
			})
		})
	t.showModal(modal)
}
