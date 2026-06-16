package tui

import (
	"fmt"
	"strings"

	"github.com/techthos/microapp-crm/internal/models"
)

// newContactsScreen builds the contacts master-detail screen (UC-7,8,10,11). The
// detail pane lists each contact's deals (UC-12).
func newContactsScreen(t *tui) *listScreen[models.Contact] {
	return newListScreen[models.Contact](t, screenConfig[models.Contact]{
		page:      pageContacts,
		cols:      contactCols,
		cells:     func(c models.Contact) []string { return contactCells(c, t.companyName) },
		detail:    t.contactDetail,
		id:        func(c models.Contact) uint64 { return c.ID },
		emptyHint: "No contacts yet — press n to add one",
		hints:     "n new · e edit · d delete · / filter · r reload",
		newForm:   func() { t.showContactForm(nil) },
		editForm:  func(c models.Contact) { t.showContactForm(&c) },
		del:       t.deleteContacts,
	})
}

// contactDetail renders a contact and its deals; the deal read is cheap and runs
// only when a row is highlighted.
func (t *tui) contactDetail(c models.Contact) string {
	deals, _ := t.store.DealsForContact(c.ID)
	return contactDetail(c, deals, t.companyName)
}

// deleteContacts cascade-deletes the targeted contacts after a batch confirm
// that names how many deals will go with them (UC-11).
func (t *tui) deleteContacts(targets []models.Contact) {
	var dealCount int
	for _, c := range targets {
		deals, _ := t.store.DealsForContact(c.ID)
		dealCount += len(deals)
	}
	msg := confirmDeleteText("contact", len(targets), targets[0].Name)
	if dealCount > 0 {
		msg = fmt.Sprintf("%s\n(%d deal(s) will also be deleted)", msg, dealCount)
	}
	t.confirm("Delete contacts", msg, true, func() {
		t.mutate(func() error {
			for _, c := range targets {
				if _, err := t.store.DeleteContact(c.ID); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// showContactForm opens the create/edit contact form.
func (t *tui) showContactForm(existing *models.Contact) {
	c := models.Contact{}
	title := "New Contact"
	if existing != nil {
		c = *existing
		title = "Edit Contact"
	}
	f := newForm(t, title, t.closeForm)
	f.input("Name", c.Name, required("Name"))
	f.companyPicker("Company", t.companies.items, c.CompanyID)
	f.input("Email", c.Email, nil)
	f.input("Phone", c.Phone, nil)
	f.input("Tags", strings.Join(c.Tags, ", "), nil)
	f.input("Notes", c.Notes, nil)
	f.onSave(func(v map[string]string) {
		base := c
		base.Name = v["Name"]
		base.CompanyID = parseUint(v["Company"])
		base.Email = v["Email"]
		base.Phone = v["Phone"]
		base.Tags = splitTags(v["Tags"])
		base.Notes = v["Notes"]
		t.mutate(func() error {
			if base.ID == 0 {
				_, err := t.store.CreateContact(base)
				return err
			}
			_, err := t.store.UpdateContact(base)
			return err
		})
	})
	t.openForm(f)
}
