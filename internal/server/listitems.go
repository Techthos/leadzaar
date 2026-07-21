package server

import (
	"time"

	"github.com/techthos/leadzaar/internal/models"
)

// Minimal list-item projections. The list_* tools return these instead of the
// full models so responses stay small: every long/freeform field (notes on
// lead/contact/deal/company, and offer description/body) is dropped. Callers that
// need the complete record use the matching get_* tool or crm://.../{id} resource.
// JSON tags match the model fields so shapes stay consistent.

// UnavailableUntil is kept in the lead projection (unlike the dropped freeform
// fields) because it drives the "can I contact this lead yet?" decision the list
// exists to answer — omitting it would force a get_lead per row.
type leadListItem struct {
	ID               uint64            `json:"id"`
	Name             string            `json:"name"`
	CompanyID        uint64            `json:"companyId,omitempty"`
	Email            string            `json:"email,omitempty"`
	Phone            string            `json:"phone,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	Quality          int               `json:"quality,omitempty"`
	Source           models.Source     `json:"source,omitempty"`
	Status           models.LeadStatus `json:"status"`
	UnavailableUntil string            `json:"unavailableUntil,omitempty"` // YYYY-MM-DD; "" = no known block
	ContactID        uint64            `json:"contactId,omitempty"`
	DealID           uint64            `json:"dealId,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

func toLeadListItem(l models.Lead) leadListItem {
	return leadListItem{
		ID: l.ID, Name: l.Name, CompanyID: l.CompanyID, Email: l.Email, Phone: l.Phone,
		Tags: l.Tags, Quality: l.Quality, Source: l.Source, Status: l.Status,
		UnavailableUntil: models.FormatDate(l.UnavailableUntil),
		ContactID:        l.ContactID, DealID: l.DealID, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func toLeadListItems(ls []models.Lead) []leadListItem {
	out := make([]leadListItem, len(ls))
	for i, l := range ls {
		out[i] = toLeadListItem(l)
	}
	return out
}

type contactListItem struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	CompanyID    uint64    `json:"companyId,omitempty"`
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	SourceLeadID uint64    `json:"sourceLeadId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func toContactListItem(c models.Contact) contactListItem {
	return contactListItem{
		ID: c.ID, Name: c.Name, CompanyID: c.CompanyID, Email: c.Email, Phone: c.Phone,
		Tags: c.Tags, SourceLeadID: c.SourceLeadID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toContactListItems(cs []models.Contact) []contactListItem {
	out := make([]contactListItem, len(cs))
	for i, c := range cs {
		out[i] = toContactListItem(c)
	}
	return out
}

type dealListItem struct {
	ID        uint64           `json:"id"`
	Title     string           `json:"title"`
	ContactID uint64           `json:"contactId"`
	CompanyID uint64           `json:"companyId,omitempty"`
	Value     float64          `json:"value"`
	Currency  string           `json:"currency,omitempty"`
	Stage     models.DealStage `json:"stage"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

func toDealListItem(d models.Deal) dealListItem {
	return dealListItem{
		ID: d.ID, Title: d.Title, ContactID: d.ContactID, CompanyID: d.CompanyID,
		Value: d.Value, Currency: d.Currency, Stage: d.Stage, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func toDealListItems(ds []models.Deal) []dealListItem {
	out := make([]dealListItem, len(ds))
	for i, d := range ds {
		out[i] = toDealListItem(d)
	}
	return out
}

type companyListItem struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Website   string    `json:"website,omitempty"`
	Industry  string    `json:"industry,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toCompanyListItem(c models.Company) companyListItem {
	return companyListItem{
		ID: c.ID, Name: c.Name, Website: c.Website, Industry: c.Industry, Phone: c.Phone,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

func toCompanyListItems(cs []models.Company) []companyListItem {
	out := make([]companyListItem, len(cs))
	for i, c := range cs {
		out[i] = toCompanyListItem(c)
	}
	return out
}

type offerListItem struct {
	ID        uint64    `json:"id"`
	LeadID    uint64    `json:"leadId"`
	Title     string    `json:"title"`
	Subject   string    `json:"subject,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toOfferListItem(o models.Offer) offerListItem {
	return offerListItem{
		ID: o.ID, LeadID: o.LeadID, Title: o.Title, Subject: o.Subject,
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
}

func toOfferListItems(os []models.Offer) []offerListItem {
	out := make([]offerListItem, len(os))
	for i, o := range os {
		out[i] = toOfferListItem(o)
	}
	return out
}
