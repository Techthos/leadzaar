package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

// newLeadsScreen builds the leads master-detail screen (UC-1,2,4,5,6). Beyond
// the shared keys it adds `c` to convert the highlighted lead.
func newLeadsScreen(t *tui) *listScreen[models.Lead] {
	return newListScreen[models.Lead](t, screenConfig[models.Lead]{
		page:      pageLeads,
		cols:      leadCols,
		cells:     func(l models.Lead) []string { return leadCells(l, t.companyName) },
		detail:    t.leadDetail,
		id:        func(l models.Lead) uint64 { return l.ID },
		emptyHint: "No leads yet — press n to add one",
		hints:     "n new · e edit · c convert · o offer · d delete · / filter · r reload",
		newForm:   func() { t.showLeadForm(nil) },
		editForm:  func(l models.Lead) { t.showLeadForm(&l) },
		del:       t.deleteLeads,
		extra: func(ev *tcell.EventKey, sel models.Lead, ok bool) *tcell.EventKey {
			switch ev.Rune() {
			case 'c':
				if ok {
					t.showConvertForm(sel)
				}
				return nil
			case 'o':
				if ok {
					t.showOfferForm(&models.Offer{LeadID: sel.ID})
				}
				return nil
			}
			return ev
		},
	})
}

// leadDetail renders a lead and its offers; the offer read is cheap and runs
// only when a row is highlighted (mirrors contactDetail's per-row deal read).
func (t *tui) leadDetail(l models.Lead) string {
	offers, _ := t.store.OffersForLead(l.ID)
	return leadDetail(l, t.companyName, offers)
}

// deleteLeads deletes the targeted leads after a single batch confirm.
func (t *tui) deleteLeads(targets []models.Lead) {
	t.confirm("Delete leads", confirmDeleteText("lead", len(targets), targets[0].Name), true, func() {
		t.mutate(func() error {
			for _, l := range targets {
				if _, err := t.store.DeleteLead(l.ID); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// showLeadForm opens the create (existing==nil) or edit lead form.
func (t *tui) showLeadForm(existing *models.Lead) {
	l := models.Lead{Status: models.StatusNew}
	title := "New Lead"
	if existing != nil {
		l = *existing
		title = "Edit Lead"
	}
	sources := sourceOptions()
	statuses := statusOptions()

	f := newForm(t, title, t.closeForm)
	f.input("Name", l.Name, required("Name"))
	f.companyPicker("Company", t.companies.items, l.CompanyID)
	f.input("Email", l.Email, nil)
	f.input("Phone", l.Phone, nil)
	f.input("Tags", strings.Join(l.Tags, ", "), nil)
	f.numInput("Quality", qualityField(l.Quality), acceptInt, qualityValidator)
	f.dropdown("Source", sources, indexOf(sources, string(l.Source)))
	f.dropdown("Status", statuses, indexOf(statuses, string(l.Status)))
	f.input("Notes", l.Notes, nil)
	f.onSave(func(v map[string]string) {
		base := l
		base.Name = v["Name"]
		base.CompanyID = parseUint(v["Company"])
		base.Email = v["Email"]
		base.Phone = v["Phone"]
		base.Tags = splitTags(v["Tags"])
		base.Quality = int(parseUint(v["Quality"]))
		base.Source = models.Source(v["Source"])
		base.Status = models.LeadStatus(v["Status"])
		base.Notes = v["Notes"]
		t.mutate(func() error {
			if base.ID == 0 {
				_, err := t.store.CreateLead(base)
				return err
			}
			_, err := t.store.UpdateLead(base)
			return err
		})
	})
	t.openForm(f)
}

// showConvertForm opens the lead-conversion form (UC-5).
func (t *tui) showConvertForm(lead models.Lead) {
	f := newForm(t, "Convert: "+lead.Name, t.closeForm)
	f.checkbox("Create deal", false)
	f.input("Deal title", "", nil)
	f.numInput("Deal value", "", acceptFloat, nil)
	f.input("Deal currency", "EUR", nil)
	f.onSave(func(v map[string]string) {
		opts := db.ConvertOptions{
			MakeDeal:     v["Create deal"] == "true",
			DealTitle:    v["Deal title"],
			DealValue:    parseFloat(v["Deal value"]),
			DealCurrency: v["Deal currency"],
		}
		t.mutate(func() error {
			_, err := t.store.Convert(lead.ID, opts)
			return err
		})
	})
	t.openForm(f)
}
