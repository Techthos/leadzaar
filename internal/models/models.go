package models

import (
	"fmt"
	"strings"
	"time"
)

// DateLayout is the wire format for the date-only fields in this package
// (Lead.UnavailableUntil): a calendar date with no time or zone component.
const DateLayout = "2006-01-02"

// ParseDate parses a DateLayout date into midnight UTC. A blank (or all-space)
// input yields the zero time and no error, so callers can clear a date field by
// passing "". Any other malformed input is an error.
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q (want %s): %w", s, DateLayout, err)
	}
	return t, nil
}

// FormatDate renders a date-only value in DateLayout. The zero time formats as
// "" so an unset date round-trips through ParseDate unchanged.
func FormatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(DateLayout)
}

// TruncateDate normalizes t to midnight UTC on its own calendar date, dropping
// any time-of-day. internal/db applies it on write so a date-only field holds
// one canonical instant per date no matter what a caller supplies. The zero
// time passes through unchanged.
func TruncateDate(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

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
	// UnavailableUntil records a date the lead is known to be unreachable until —
	// typically transcribed from an out-of-office autoresponder ("away until the
	// 15th") so a follow-up or offer can be held back rather than wasted. It is
	// date-only (normalized to midnight UTC by internal/db) and **exclusive**: the
	// lead is reachable again from that instant on. Zero = no known block. It is
	// independent of Status — an unavailable lead keeps its funnel position.
	UnavailableUntil time.Time `json:"unavailableUntil,omitzero"`
	ContactID        uint64    `json:"contactId,omitempty"` // 0 until converted
	DealID           uint64    `json:"dealId,omitempty"`    // 0 unless a deal was made on conversion
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Available reports whether the lead is reachable at instant t: true when no
// unavailability window is recorded, or when the recorded one has elapsed.
// UnavailableUntil is exclusive, so a lead "away until the 15th" is available
// from the very start of the 15th.
func (l Lead) Available(t time.Time) bool {
	return !l.UnavailableUntil.After(t)
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
