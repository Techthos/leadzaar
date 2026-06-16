package models

import "time"

// Lead is a raw, unqualified prospect — the inbox of the funnel. Identity is the
// surrogate ID (assigned by internal/db via NextSequence). Email is optional and
// non-unique. On conversion, ContactID (and optionally DealID) back-reference the
// records created from this lead and Status becomes StatusConverted.
type Lead struct {
	ID        uint64     `json:"id"`
	Name      string     `json:"name"`
	CompanyID uint64     `json:"companyId,omitempty"` // 0 = no linked Company
	Email     string     `json:"email,omitempty"`
	Phone     string     `json:"phone,omitempty"`
	Tags      []string   `json:"tags,omitempty"`
	Quality   int        `json:"quality,omitempty"` // lead score 1–10; 0 = unscored
	Source    Source     `json:"source,omitempty"`
	Status    LeadStatus `json:"status"`
	Notes     string     `json:"notes,omitempty"`
	ContactID uint64     `json:"contactId,omitempty"` // 0 until converted
	DealID    uint64     `json:"dealId,omitempty"`    // 0 unless a deal was made on conversion
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// Contact is a known person being actively dealt with. SourceLeadID records the
// Lead it was converted from (0 if created directly).
type Contact struct {
	ID           uint64    `json:"id"`
	Name         string    `json:"name"`
	CompanyID    uint64    `json:"companyId,omitempty"` // 0 = no linked Company
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	Notes        string    `json:"notes,omitempty"`
	SourceLeadID uint64    `json:"sourceLeadId,omitempty"` // 0 if created directly
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Deal is an opportunity owned by exactly one Contact. Value is paired with a
// per-deal Currency code (no cross-currency conversion — see spec Non-Goals).
type Deal struct {
	ID        uint64    `json:"id"`
	Title     string    `json:"title"`
	ContactID uint64    `json:"contactId"`
	CompanyID uint64    `json:"companyId,omitempty"` // 0 = no linked Company
	Value     float64   `json:"value"`
	Currency  string    `json:"currency,omitempty"`
	Stage     DealStage `json:"stage"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Offer is an email-style proposal made to a Lead. A Lead has zero-or-more
// Offers (1:N via OfferLeadID); deleting a Lead cascades to its Offers. Subject
// and Body carry the raw email content (Body may be long, multi-line).
type Offer struct {
	ID          uint64    `json:"id"`
	LeadID      uint64    `json:"leadId"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Subject     string    `json:"subject,omitempty"`
	Body        string    `json:"body,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Company is an organization that Leads, Contacts, and Deals may optionally link
// to by ID (CompanyID). Identity is the surrogate ID. Deleting a Company unlinks
// it from any referencing records (their CompanyID is reset to 0); the records
// themselves are retained. Company has no funnel state — it is reference data.
type Company struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Website   string    `json:"website,omitempty"`
	Industry  string    `json:"industry,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
