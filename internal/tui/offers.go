package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/techthos/microapp-crm/internal/models"
)

// newOffersScreen builds the offers master-detail screen. Offers are email-style
// proposals each linked to a lead; the detail pane shows the full subject/body
// and the resolved lead, and `l` jumps to that lead (UC-24,25,27,28).
func newOffersScreen(t *tui) *listScreen[models.Offer] {
	return newListScreen[models.Offer](t, screenConfig[models.Offer]{
		page:      pageOffers,
		cols:      offerCols,
		cells:     offerCells,
		detail:    t.offerDetail,
		id:        func(o models.Offer) uint64 { return o.ID },
		emptyHint: "No offers yet — press n to add one",
		hints:     "n new · e edit · l go to lead · d delete · / filter · r reload",
		newForm:   func() { t.showOfferForm(nil) },
		editForm:  func(o models.Offer) { t.showOfferForm(&o) },
		del:       t.deleteOffers,
		extra: func(ev *tcell.EventKey, sel models.Offer, ok bool) *tcell.EventKey {
			if ev.Rune() == 'l' {
				if ok {
					t.gotoLead(sel.LeadID)
				}
				return nil
			}
			return ev
		},
	})
}

// offerDetail renders an offer with its owning lead resolved to a name; the read
// is cheap and runs only when a row is highlighted.
func (t *tui) offerDetail(o models.Offer) string {
	name := ""
	if lead, err := t.store.GetLead(o.LeadID); err == nil {
		name = lead.Name
	}
	return offerDetail(o, name)
}

// deleteOffers deletes the targeted offers after a single batch confirm.
func (t *tui) deleteOffers(targets []models.Offer) {
	t.confirm("Delete offers", confirmDeleteText("offer", len(targets), targets[0].Title), true, func() {
		t.mutate(func() error {
			for _, o := range targets {
				if err := t.store.DeleteOffer(o.ID); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// showOfferForm opens the create or edit offer form. A nil existing is a blank
// create; a non-nil existing with a zero ID is a create pre-filled with its
// LeadID (used by the Leads `o` action); a non-zero ID is an edit. Body is a
// multi-line text area for raw email content.
func (t *tui) showOfferForm(existing *models.Offer) {
	o := models.Offer{}
	title := "New Offer"
	if existing != nil {
		o = *existing
		if o.ID != 0 {
			title = "Edit Offer"
		}
	}

	f := newForm(t, title, t.closeForm)
	f.numInput("Lead ID", uintField(o.LeadID), acceptInt, required("Lead ID"))
	f.input("Title", o.Title, required("Title"))
	f.input("Description", o.Description, nil)
	f.input("Subject", o.Subject, nil)
	f.textArea("Body", o.Body, 8, nil)
	f.onSave(func(v map[string]string) {
		base := o
		base.LeadID = parseUint(v["Lead ID"])
		base.Title = v["Title"]
		base.Description = v["Description"]
		base.Subject = v["Subject"]
		base.Body = v["Body"]
		t.mutate(func() error {
			if base.ID == 0 {
				_, err := t.store.CreateOffer(base)
				return err
			}
			_, err := t.store.UpdateOffer(base)
			return err
		})
	})
	t.openForm(f)
}
