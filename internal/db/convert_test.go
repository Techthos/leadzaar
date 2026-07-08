package db_test

import (
	"errors"
	"testing"

	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

func TestConvert(t *testing.T) {
	t.Parallel()

	t.Run("contact only mirrors lead and marks converted", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, newClock())
		acme, _ := store.CreateCompany(models.Company{Name: "Acme"})
		lead, _ := store.CreateLead(models.Lead{
			Name: "Jane", CompanyID: acme.ID, Email: "jane@example.com", Tags: []string{"vip"},
		})

		res, err := store.Convert(lead.ID, db.ConvertOptions{})
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if res.Deal != nil {
			t.Errorf("expected no deal, got %+v", res.Deal)
		}
		if res.Contact.Name != "Jane" || res.Contact.Email != "jane@example.com" {
			t.Errorf("contact did not mirror lead: %+v", res.Contact)
		}
		if res.Contact.CompanyID != acme.ID {
			t.Errorf("contact CompanyID = %d, want %d (inherited from lead)", res.Contact.CompanyID, acme.ID)
		}
		if res.Contact.SourceLeadID != lead.ID {
			t.Errorf("SourceLeadID = %d, want %d", res.Contact.SourceLeadID, lead.ID)
		}
		if res.Lead.Status != models.StatusConverted {
			t.Errorf("lead status = %q, want converted", res.Lead.Status)
		}
		if res.Lead.ContactID != res.Contact.ID || res.Lead.DealID != 0 {
			t.Errorf("lead back-refs wrong: contactID=%d dealID=%d", res.Lead.ContactID, res.Lead.DealID)
		}
		// Contact is findable by its mirrored email index.
		hits, _ := store.FindContactsByEmail("jane@example.com")
		if len(hits) != 1 {
			t.Errorf("contact not email-indexed: %d hits", len(hits))
		}
	})

	t.Run("with deal creates qualification-stage deal and back-refs it", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, newClock())
		lead, _ := store.CreateLead(models.Lead{Name: "Jane"})

		res, err := store.Convert(lead.ID, db.ConvertOptions{
			MakeDeal: true, DealTitle: "First deal", DealValue: 5000, DealCurrency: "EUR",
		})
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if res.Deal == nil {
			t.Fatal("expected a deal, got nil")
		}
		if res.Deal.Stage != models.StageQualification {
			t.Errorf("deal stage = %q, want qualification", res.Deal.Stage)
		}
		if res.Deal.ContactID != res.Contact.ID {
			t.Errorf("deal not linked to contact")
		}
		if res.Lead.DealID != res.Deal.ID {
			t.Errorf("lead.DealID = %d, want %d", res.Lead.DealID, res.Deal.ID)
		}
		deals, _ := store.DealsForContact(res.Contact.ID)
		if len(deals) != 1 {
			t.Errorf("contact deals = %d, want 1", len(deals))
		}
	})

	t.Run("make-deal without title is rejected", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, newClock())
		lead, _ := store.CreateLead(models.Lead{Name: "Jane"})
		if _, err := store.Convert(lead.ID, db.ConvertOptions{MakeDeal: true}); err == nil {
			t.Fatal("expected error for empty deal title, got nil")
		}
	})

	t.Run("converting twice is rejected and is atomic", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, newClock())
		lead, _ := store.CreateLead(models.Lead{Name: "Jane"})
		if _, err := store.Convert(lead.ID, db.ConvertOptions{}); err != nil {
			t.Fatalf("first Convert: %v", err)
		}
		if _, err := store.Convert(lead.ID, db.ConvertOptions{}); err == nil {
			t.Fatal("expected error on second convert, got nil")
		}
		// Only one contact should exist (second convert rolled back).
		contacts, _ := store.ListContacts()
		if len(contacts) != 1 {
			t.Errorf("contacts = %d, want 1 (no duplicate from rejected convert)", len(contacts))
		}
	})

	t.Run("unknown lead is ErrNotFound", func(t *testing.T) {
		t.Parallel()
		store := openTestStore(t, newClock())
		if _, err := store.Convert(99999, db.ConvertOptions{}); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
