package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// newLeadsScreen builds the leads table and its key handling (UC-1,2,4,5,6).
func (t *tui) newLeadsScreen() *tview.Table {
	table := newListTable("Leads — n new · enter edit · c convert · d delete")
	table.SetSelectedFunc(func(row, _ int) {
		if lead, ok := t.selectedLead(row); ok {
			t.showLeadForm(&lead)
		}
	})
	table.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		row, _ := table.GetSelection()
		switch ev.Rune() {
		case 'n':
			t.showLeadForm(nil)
			return nil
		case 'c':
			if lead, ok := t.selectedLead(row); ok {
				t.showConvertForm(lead)
			}
			return nil
		case 'd':
			if lead, ok := t.selectedLead(row); ok {
				t.mutate(pageLeads, t.leadsTable, func() error { return t.store.DeleteLead(lead.ID) })
			}
			return nil
		}
		return ev
	})
	return table
}

// selectedLead maps a table row (1-based; row 0 is the header) to its lead.
func (t *tui) selectedLead(row int) (models.Lead, bool) {
	idx := row - 1
	if idx < 0 || idx >= len(t.leads) {
		return models.Lead{}, false
	}
	return t.leads[idx], true
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

	form := tview.NewForm()
	form.AddInputField("Name", l.Name, 32, nil, nil)
	form.AddInputField("Company", l.Company, 32, nil, nil)
	form.AddInputField("Email", l.Email, 32, nil, nil)
	form.AddInputField("Phone", l.Phone, 32, nil, nil)
	form.AddDropDown("Source", sources, indexOf(sources, string(l.Source)), nil)
	form.AddDropDown("Status", statuses, indexOf(statuses, string(l.Status)), nil)
	form.AddInputField("Notes", l.Notes, 32, nil, nil)
	form.AddButton("Save", func() {
		base := l
		base.Name = formText(form, "Name")
		base.Company = formText(form, "Company")
		base.Email = formText(form, "Email")
		base.Phone = formText(form, "Phone")
		base.Source = models.Source(formDropdown(form, "Source"))
		base.Status = models.LeadStatus(formDropdown(form, "Status"))
		base.Notes = formText(form, "Notes")
		t.mutate(pageLeads, t.leadsTable, func() error {
			if base.ID == 0 {
				_, err := t.store.CreateLead(base)
				return err
			}
			_, err := t.store.UpdateLead(base)
			return err
		})
	})
	form.AddButton("Cancel", func() { t.closeOverlay(pageLeads, t.leadsTable) })
	form.SetButtonsAlign(tview.AlignCenter)
	form.SetBorder(true).SetTitle(" " + title + " · Esc cancels ")
	form.SetCancelFunc(func() { t.closeOverlay(pageLeads, t.leadsTable) })
	t.showOverlay(form, 52, 20)
}

// showConvertForm opens the lead-conversion form (UC-5).
func (t *tui) showConvertForm(lead models.Lead) {
	form := tview.NewForm()
	form.AddCheckbox("Create deal", false, nil)
	form.AddInputField("Deal title", "", 32, nil, nil)
	form.AddInputField("Deal value", "", 16, tview.InputFieldFloat, nil)
	form.AddInputField("Deal currency", "EUR", 8, nil, nil)
	form.AddButton("Convert", func() {
		makeDeal := form.GetFormItemByLabel("Create deal").(*tview.Checkbox).IsChecked()
		opts := db.ConvertOptions{
			MakeDeal:     makeDeal,
			DealTitle:    formText(form, "Deal title"),
			DealValue:    parseFloat(formText(form, "Deal value")),
			DealCurrency: formText(form, "Deal currency"),
		}
		t.mutate(pageLeads, t.leadsTable, func() error {
			_, err := t.store.Convert(lead.ID, opts)
			return err
		})
	})
	form.AddButton("Cancel", func() { t.closeOverlay(pageLeads, t.leadsTable) })
	form.SetButtonsAlign(tview.AlignCenter)
	form.SetBorder(true).SetTitle(" Convert: " + lead.Name + " · Esc cancels ")
	form.SetCancelFunc(func() { t.closeOverlay(pageLeads, t.leadsTable) })
	t.showOverlay(form, 52, 14)
}
