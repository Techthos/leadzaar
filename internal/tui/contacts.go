package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/techthos/microapp-crm/internal/models"
)

// newContactsScreen builds the contacts table and key handling (UC-7,8,10,11).
func (t *tui) newContactsScreen() *tview.Table {
	table := newListTable("Contacts — n new · enter edit · d delete")
	table.SetSelectedFunc(func(row, _ int) {
		if c, ok := t.selectedContact(row); ok {
			t.showContactForm(&c)
		}
	})
	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		row, _ := table.GetSelection()
		switch ev.Rune() {
		case 'n':
			t.showContactForm(nil)
			return nil
		case 'd':
			if c, ok := t.selectedContact(row); ok {
				t.confirmDeleteContact(c)
			}
			return nil
		}
		return ev
	})
	return table
}

func (t *tui) selectedContact(row int) (models.Contact, bool) {
	idx := row - 1
	if idx < 0 || idx >= len(t.contacts) {
		return models.Contact{}, false
	}
	return t.contacts[idx], true
}

// showContactForm opens the create/edit contact form.
func (t *tui) showContactForm(existing *models.Contact) {
	c := models.Contact{}
	title := "New Contact"
	if existing != nil {
		c = *existing
		title = "Edit Contact"
	}
	form := tview.NewForm()
	form.AddInputField("Name", c.Name, 32, nil, nil)
	form.AddInputField("Company", c.Company, 32, nil, nil)
	form.AddInputField("Email", c.Email, 32, nil, nil)
	form.AddInputField("Phone", c.Phone, 32, nil, nil)
	form.AddInputField("Notes", c.Notes, 32, nil, nil)
	form.AddButton("Save", func() {
		base := c
		base.Name = formText(form, "Name")
		base.Company = formText(form, "Company")
		base.Email = formText(form, "Email")
		base.Phone = formText(form, "Phone")
		base.Notes = formText(form, "Notes")
		t.mutate(pageContacts, t.contactsTable, func() error {
			if base.ID == 0 {
				_, err := t.store.CreateContact(base)
				return err
			}
			_, err := t.store.UpdateContact(base)
			return err
		})
	})
	form.AddButton("Cancel", func() { t.closeOverlay(pageContacts, t.contactsTable) })
	form.SetButtonsAlign(tview.AlignCenter)
	form.SetBorder(true).SetTitle(" " + title + " · Esc cancels ")
	form.SetCancelFunc(func() { t.closeOverlay(pageContacts, t.contactsTable) })
	t.showOverlay(form, 52, 16)
}

// confirmDeleteContact shows a cascade-delete confirmation modal (UC-11).
func (t *tui) confirmDeleteContact(c models.Contact) {
	deals, _ := t.store.DealsForContact(c.ID)
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Delete contact %q and its %d deal(s)?\nThis cannot be undone.", c.Name, len(deals))).
		AddButtons([]string{"Delete", "Cancel"}).
		SetDoneFunc(func(_ int, label string) {
			if label == "Delete" {
				t.mutate(pageContacts, t.contactsTable, func() error {
					_, err := t.store.DeleteContact(c.ID)
					return err
				})
				return
			}
			t.closeOverlay(pageContacts, t.contactsTable)
		})
	t.showModal(modal)
}
