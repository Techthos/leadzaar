package tui

import (
	"fmt"

	"github.com/techthos/leadzaar/internal/models"
)

// newCompaniesScreen builds the companies master-detail screen. Companies are
// reference data that leads, contacts, and deals link to by ID; deleting one
// unlinks (does not delete) any referencing records.
func newCompaniesScreen(t *tui) *listScreen[models.Company] {
	return newListScreen[models.Company](t, screenConfig[models.Company]{
		page:      pageCompanies,
		cols:      companyCols,
		cells:     companyCells,
		detail:    func(c models.Company) string { return companyDetail(c) },
		id:        func(c models.Company) uint64 { return c.ID },
		emptyHint: "No companies yet — press n to add one",
		hints:     "n new · e edit · d delete · / filter · r reload",
		newForm:   func() { t.showCompanyForm(nil) },
		editForm:  func(c models.Company) { t.showCompanyForm(&c) },
		del:       t.deleteCompanies,
	})
}

// deleteCompanies deletes the targeted companies after a batch confirm that
// names how many records will be unlinked (their CompanyID reset to 0).
func (t *tui) deleteCompanies(targets []models.Company) {
	unlinked := t.countCompanyReferences(targets)
	msg := confirmDeleteText("company", len(targets), targets[0].Name)
	if unlinked > 0 {
		msg = fmt.Sprintf("%s\n(%d record(s) will be unlinked, not deleted)", msg, unlinked)
	}
	t.confirm("Delete companies", msg, true, func() {
		t.mutate(func() error {
			for _, c := range targets {
				if _, err := t.store.DeleteCompany(c.ID); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// countCompanyReferences counts how many of the loaded leads/contacts/deals link
// to any of the target companies — for the delete confirm's unlink warning.
func (t *tui) countCompanyReferences(targets []models.Company) int {
	ids := make(map[uint64]bool, len(targets))
	for _, c := range targets {
		ids[c.ID] = true
	}
	var n int
	for _, l := range t.leads.items {
		if ids[l.CompanyID] {
			n++
		}
	}
	for _, c := range t.contacts.items {
		if ids[c.CompanyID] {
			n++
		}
	}
	for _, d := range t.deals.items {
		if ids[d.CompanyID] {
			n++
		}
	}
	return n
}

// showCompanyForm opens the create/edit company form.
func (t *tui) showCompanyForm(existing *models.Company) {
	c := models.Company{}
	title := "New Company"
	if existing != nil {
		c = *existing
		title = "Edit Company"
	}
	f := newForm(t, title, t.closeForm)
	f.input("Name", c.Name, required("Name"))
	f.input("Website", c.Website, nil)
	f.input("Industry", c.Industry, nil)
	f.input("Phone", c.Phone, nil)
	f.input("Notes", c.Notes, nil)
	f.onSave(func(v map[string]string) {
		base := c
		base.Name = v["Name"]
		base.Website = v["Website"]
		base.Industry = v["Industry"]
		base.Phone = v["Phone"]
		base.Notes = v["Notes"]
		t.mutate(func() error {
			if base.ID == 0 {
				_, err := t.store.CreateCompany(base)
				return err
			}
			_, err := t.store.UpdateCompany(base)
			return err
		})
	})
	t.openForm(f)
}
