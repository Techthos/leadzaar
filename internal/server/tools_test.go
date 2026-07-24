package server_test

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/techthos/leadzaar/internal/models"
)

// TestEveryToolHappyPath exercises each remaining CRUD tool once so the full
// registered surface is covered end-to-end through the in-process client.
func TestEveryToolHappyPath(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	// --- Leads: create, list, update, delete ---
	res := callTool(t, c, ctx, "create_lead", map[string]any{"name": "L1", "source": "web"})
	var lead models.Lead
	mustJSON(t, res, &lead)

	if r := callTool(t, c, ctx, "list_leads", map[string]any{}); r.IsError {
		t.Fatalf("list_leads: %s", resultText(t, r))
	}
	if r := callTool(t, c, ctx, "list_leads", map[string]any{"status": "new"}); r.IsError {
		t.Fatalf("list_leads(new): %s", resultText(t, r))
	}

	upd := callTool(t, c, ctx, "update_lead", map[string]any{
		"id": lead.ID, "name": "L1b", "status": "contacted",
	})
	var updatedLead models.Lead
	mustJSON(t, upd, &updatedLead)
	if updatedLead.Status != models.StatusContacted || updatedLead.Name != "L1b" {
		t.Errorf("update_lead result: %+v", updatedLead)
	}

	if r := callTool(t, c, ctx, "delete_lead", map[string]any{"id": lead.ID}); r.IsError {
		t.Fatalf("delete_lead: %s", resultText(t, r))
	}

	// --- Contacts: create, list, get, update ---
	cres := callTool(t, c, ctx, "create_contact", map[string]any{"name": "C1", "email": "c1@x.io", "tags": []string{"vip"}})
	var contact models.Contact
	mustJSON(t, cres, &contact)

	for _, args := range []map[string]any{
		{}, {"query": "C1"}, {"email": "c1@x.io"}, {"tag": "vip"},
	} {
		if r := callTool(t, c, ctx, "list_contacts", args); r.IsError {
			t.Fatalf("list_contacts(%v): %s", args, resultText(t, r))
		}
	}
	if r := callTool(t, c, ctx, "get_contact", map[string]any{"id": contact.ID}); r.IsError {
		t.Fatalf("get_contact: %s", resultText(t, r))
	}
	if r := callTool(t, c, ctx, "update_contact", map[string]any{"id": contact.ID, "name": "C1b"}); r.IsError {
		t.Fatalf("update_contact: %s", resultText(t, r))
	}

	// --- Deals: create, list, get, update, delete ---
	dres := callTool(t, c, ctx, "create_deal", map[string]any{
		"title": "D1", "contact_id": contact.ID, "value": 1000.0, "currency": "EUR", "stage": "qualification",
	})
	var deal models.Deal
	mustJSON(t, dres, &deal)

	for _, args := range []map[string]any{
		{}, {"stage": "qualification"}, {"contact_id": contact.ID},
	} {
		if r := callTool(t, c, ctx, "list_deals", args); r.IsError {
			t.Fatalf("list_deals(%v): %s", args, resultText(t, r))
		}
	}
	if r := callTool(t, c, ctx, "get_deal", map[string]any{"id": deal.ID}); r.IsError {
		t.Fatalf("get_deal: %s", resultText(t, r))
	}
	updDeal := callTool(t, c, ctx, "update_deal", map[string]any{
		"id": deal.ID, "title": "D1", "contact_id": contact.ID, "value": 2000.0, "currency": "EUR", "stage": "won",
	})
	var updatedDeal models.Deal
	mustJSON(t, updDeal, &updatedDeal)
	if updatedDeal.Stage != models.StageWon {
		t.Errorf("update_deal stage = %q, want won", updatedDeal.Stage)
	}
	if r := callTool(t, c, ctx, "delete_deal", map[string]any{"id": deal.ID}); r.IsError {
		t.Fatalf("delete_deal: %s", resultText(t, r))
	}

	// Lead resource read path (the other resource handlers are covered elsewhere).
	lead2 := callTool(t, c, ctx, "create_lead", map[string]any{"name": "L2"})
	var l2 models.Lead
	mustJSON(t, lead2, &l2)
	if r := callTool(t, c, ctx, "get_lead", map[string]any{"id": l2.ID}); r.IsError {
		t.Fatalf("get_lead: %s", resultText(t, r))
	}
}

// TestCompanyToolsAndUnlink exercises the company CRUD tools, links a lead/contact
// to a company, then deletes the company through the tool and verifies the
// unlink count and the cleared reference + the company resource read path.
func TestCompanyToolsAndUnlink(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	comp := callTool(t, c, ctx, "create_company", map[string]any{
		"name": "Acme", "website": "acme.io", "industry": "Tech",
	})
	var company models.Company
	mustJSON(t, comp, &company)
	if company.ID == 0 || company.Name != "Acme" {
		t.Fatalf("create_company result: %+v", company)
	}

	if r := callTool(t, c, ctx, "list_companies", map[string]any{"query": "acme"}); r.IsError {
		t.Fatalf("list_companies: %s", resultText(t, r))
	}
	if r := callTool(t, c, ctx, "get_company", map[string]any{"id": company.ID}); r.IsError {
		t.Fatalf("get_company: %s", resultText(t, r))
	}
	if r := callTool(t, c, ctx, "update_company", map[string]any{"id": company.ID, "name": "Acme Corp"}); r.IsError {
		t.Fatalf("update_company: %s", resultText(t, r))
	}

	// Link a lead and a contact to the company.
	lres := callTool(t, c, ctx, "create_lead", map[string]any{"name": "L", "company_id": company.ID})
	var lead models.Lead
	mustJSON(t, lres, &lead)
	if lead.CompanyID != company.ID {
		t.Errorf("lead CompanyID = %d, want %d", lead.CompanyID, company.ID)
	}
	cres := callTool(t, c, ctx, "create_contact", map[string]any{"name": "C", "company_id": company.ID})
	var contact models.Contact
	mustJSON(t, cres, &contact)

	// Linking to a non-existent company is a tool error.
	if r := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Bad", "company_id": 99999}); !r.IsError {
		t.Errorf("create_lead with bad company_id: expected tool error, got %s", resultText(t, r))
	}

	// Read the company resource.
	rreq := mcp.ReadResourceRequest{}
	rreq.Params.URI = "crm://companies/" + itoa(company.ID)
	if _, err := c.ReadResource(ctx, rreq); err != nil {
		t.Fatalf("ReadResource(company): %v", err)
	}

	// Delete the company → unlink count of 2 (lead + contact).
	del := callTool(t, c, ctx, "delete_company", map[string]any{"id": company.ID})
	var payload struct {
		Unlinked int `json:"unlinked"`
	}
	mustJSON(t, del, &payload)
	if payload.Unlinked != 2 {
		t.Errorf("unlinked = %d, want 2", payload.Unlinked)
	}

	got := callTool(t, c, ctx, "get_lead", map[string]any{"id": lead.ID})
	var gotLead models.Lead
	mustJSON(t, got, &gotLead)
	if gotLead.CompanyID != 0 {
		t.Errorf("lead still linked after company delete: CompanyID = %d", gotLead.CompanyID)
	}
}

// TestOfferToolsAndLeadCascade exercises the offer CRUD tools, reads the offer
// resource, then deletes the owning lead and verifies the cascade returns the
// offer id and the offer is gone.
func TestOfferToolsAndLeadCascade(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	lres := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Prospect"})
	var lead models.Lead
	mustJSON(t, lres, &lead)

	ores := callTool(t, c, ctx, "create_offer", map[string]any{
		"lead_id": lead.ID, "title": "Q3 Proposal", "subject": "Our proposal", "body": "Dear customer,\nhere is our offer.",
	})
	var offer models.Offer
	mustJSON(t, ores, &offer)
	if offer.ID == 0 || offer.LeadID != lead.ID {
		t.Fatalf("create_offer result: %+v", offer)
	}

	for _, args := range []map[string]any{{}, {"lead_id": lead.ID}} {
		if r := callTool(t, c, ctx, "list_offers", args); r.IsError {
			t.Fatalf("list_offers(%v): %s", args, resultText(t, r))
		}
	}
	if r := callTool(t, c, ctx, "get_offer", map[string]any{"id": offer.ID}); r.IsError {
		t.Fatalf("get_offer: %s", resultText(t, r))
	}
	upd := callTool(t, c, ctx, "update_offer", map[string]any{
		"id": offer.ID, "lead_id": lead.ID, "title": "Q3 Proposal v2", "body": "Updated body.",
	})
	var updatedOffer models.Offer
	mustJSON(t, upd, &updatedOffer)
	if updatedOffer.Title != "Q3 Proposal v2" || updatedOffer.Body != "Updated body." {
		t.Errorf("update_offer result: %+v", updatedOffer)
	}

	// Creating an offer for a non-existent lead is a tool error.
	if r := callTool(t, c, ctx, "create_offer", map[string]any{"lead_id": 99999, "title": "Bad"}); !r.IsError {
		t.Errorf("create_offer with bad lead_id: expected tool error, got %s", resultText(t, r))
	}

	// Read the offer resource.
	rreq := mcp.ReadResourceRequest{}
	rreq.Params.URI = "crm://offers/" + itoa(offer.ID)
	if _, err := c.ReadResource(ctx, rreq); err != nil {
		t.Fatalf("ReadResource(offer): %v", err)
	}

	// Deleting the lead cascades to its offers.
	del := callTool(t, c, ctx, "delete_lead", map[string]any{"id": lead.ID})
	var payload struct {
		DeletedOfferIDs []uint64 `json:"deleted_offer_ids"`
	}
	mustJSON(t, del, &payload)
	if len(payload.DeletedOfferIDs) != 1 || payload.DeletedOfferIDs[0] != offer.ID {
		t.Errorf("deleted_offer_ids = %v, want [%d]", payload.DeletedOfferIDs, offer.ID)
	}
	if r := callTool(t, c, ctx, "get_offer", map[string]any{"id": offer.ID}); !r.IsError {
		t.Errorf("offer survived lead cascade: %s", resultText(t, r))
	}
}

// leadPageResult mirrors the paginated list_leads response shape.
type leadPageResult struct {
	Leads      []models.Lead `json:"leads"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	Total      int           `json:"total"`
	TotalPages int           `json:"total_pages"`
	HasMore    bool          `json:"has_more"`
}

// TestListLeadsPaginationAndSearch exercises the search/sort/paginate surface of
// list_leads end-to-end through the in-process client.
func TestListLeadsPaginationAndSearch(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	for _, n := range []struct {
		name    string
		quality int
	}{{"Acme One", 2}, {"Acme Two", 8}, {"Beta", 5}} {
		if r := callTool(t, c, ctx, "create_lead", map[string]any{"name": n.name, "quality": n.quality}); r.IsError {
			t.Fatalf("create_lead: %s", resultText(t, r))
		}
	}

	t.Run("page size clamps and reports has_more", func(t *testing.T) {
		var p leadPageResult
		mustJSON(t, callTool(t, c, ctx, "list_leads", map[string]any{"page_size": 2}), &p)
		if len(p.Leads) != 2 || p.PageSize != 2 || p.Total != 3 || p.TotalPages != 2 || !p.HasMore {
			t.Errorf("page 1 = %+v", p)
		}
	})

	t.Run("search narrows the set", func(t *testing.T) {
		var p leadPageResult
		mustJSON(t, callTool(t, c, ctx, "list_leads", map[string]any{"query": "acme"}), &p)
		if p.Total != 2 {
			t.Errorf("query total = %d, want 2", p.Total)
		}
	})

	t.Run("sort by quality ascending", func(t *testing.T) {
		var p leadPageResult
		mustJSON(t, callTool(t, c, ctx, "list_leads", map[string]any{"sort_by": "quality", "order": "asc"}), &p)
		if len(p.Leads) != 3 || p.Leads[0].Quality != 2 || p.Leads[2].Quality != 8 {
			t.Errorf("quality asc order = %+v", p.Leads)
		}
	})

	t.Run("invalid sort is a tool error", func(t *testing.T) {
		if r := callTool(t, c, ctx, "list_leads", map[string]any{"sort_by": "bogus"}); !r.IsError {
			t.Error("expected tool error for bad sort_by")
		}
	})
}

// TestListToolsMinimalAndUpdatedFirst verifies that list_* omits long/freeform
// fields (lead notes, offer description/body) to keep responses small, that the
// matching get_* still returns the complete record, and that lists default to
// most-recently-updated first.
func TestListToolsMinimalAndUpdatedFirst(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	// Two leads with notes; update the first so it becomes most-recently-updated.
	var leadA, leadB models.Lead
	mustJSON(t, callTool(t, c, ctx, "create_lead", map[string]any{"name": "Alpha", "notes": "secret-alpha"}), &leadA)
	mustJSON(t, callTool(t, c, ctx, "create_lead", map[string]any{"name": "Beta", "notes": "secret-beta"}), &leadB)
	if r := callTool(t, c, ctx, "update_lead", map[string]any{"id": leadA.ID, "name": "Alpha2"}); r.IsError {
		t.Fatalf("update_lead: %s", resultText(t, r))
	}

	listRes := callTool(t, c, ctx, "list_leads", map[string]any{})
	if txt := resultText(t, listRes); strings.Contains(txt, "notes") || strings.Contains(txt, "secret-") {
		t.Errorf("list_leads leaked notes:\n%s", txt)
	}
	var lp leadPageResult
	mustJSON(t, listRes, &lp)
	if len(lp.Leads) != 2 {
		t.Fatalf("list_leads returned %d leads, want 2", len(lp.Leads))
	}
	if lp.Leads[0].ID != leadA.ID {
		t.Errorf("most-recently-updated lead not first: got id %d, want %d", lp.Leads[0].ID, leadA.ID)
	}
	if lp.Leads[0].Notes != "" {
		t.Errorf("list item carried notes: %q", lp.Leads[0].Notes)
	}

	// get_lead returns the full record including notes.
	var full models.Lead
	mustJSON(t, callTool(t, c, ctx, "get_lead", map[string]any{"id": leadB.ID}), &full)
	if full.Notes != "secret-beta" {
		t.Errorf("get_lead notes = %q, want the stored note", full.Notes)
	}

	// Offers: description/body omitted in the list, present via get_offer.
	var offer models.Offer
	mustJSON(t, callTool(t, c, ctx, "create_offer", map[string]any{
		"lead_id": leadB.ID, "title": "Prop", "subject": "Subj", "description": "desc-text", "body": "long-body-text",
	}), &offer)

	offListRes := callTool(t, c, ctx, "list_offers", map[string]any{})
	if txt := resultText(t, offListRes); strings.Contains(txt, "long-body-text") || strings.Contains(txt, "desc-text") {
		t.Errorf("list_offers leaked description/body:\n%s", txt)
	}
	var fullOffer models.Offer
	mustJSON(t, callTool(t, c, ctx, "get_offer", map[string]any{"id": offer.ID}), &fullOffer)
	if fullOffer.Body != "long-body-text" || fullOffer.Description != "desc-text" {
		t.Errorf("get_offer missing full fields: %+v", fullOffer)
	}
}

// TestUpdateToolsPartialPreservesOtherFields is the regression guard for the
// "update one field, the rest get nulled" bug: the update_* tools are partial
// updates, so a call that supplies only some fields must leave every omitted
// field at its stored value. It also confirms an explicitly supplied empty
// string clears a field, and that omitting tags keeps them.
func TestUpdateToolsPartialPreservesOtherFields(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	t.Run("lead: change only status keeps the rest", func(t *testing.T) {
		var lead models.Lead
		mustJSON(t, callTool(t, c, ctx, "create_lead", map[string]any{
			"name": "Jane", "email": "jane@x.io", "phone": "555-1", "tags": []string{"vip", "warm"},
			"quality": 7, "source": "referral", "notes": "met at expo",
		}), &lead)

		var got models.Lead
		mustJSON(t, callTool(t, c, ctx, "update_lead", map[string]any{
			"id": lead.ID, "status": "qualified",
		}), &got)

		if got.Status != models.StatusQualified {
			t.Errorf("status = %q, want qualified", got.Status)
		}
		if got.Name != "Jane" || got.Email != "jane@x.io" || got.Phone != "555-1" ||
			got.Quality != 7 || got.Source != models.SourceReferral || got.Notes != "met at expo" {
			t.Errorf("omitted scalar field not preserved: %+v", got)
		}
		if !equalStrings(got.Tags, []string{"vip", "warm"}) {
			t.Errorf("tags = %v, want [vip warm]", got.Tags)
		}
	})

	t.Run("contact: change only name keeps email/phone/tags/notes", func(t *testing.T) {
		var contact models.Contact
		mustJSON(t, callTool(t, c, ctx, "create_contact", map[string]any{
			"name": "Old", "email": "c@x.io", "phone": "555-2", "tags": []string{"lead"}, "notes": "keep me",
		}), &contact)

		var got models.Contact
		mustJSON(t, callTool(t, c, ctx, "update_contact", map[string]any{
			"id": contact.ID, "name": "New",
		}), &got)

		if got.Name != "New" || got.Email != "c@x.io" || got.Phone != "555-2" || got.Notes != "keep me" {
			t.Errorf("partial update lost a field: %+v", got)
		}
		if !equalStrings(got.Tags, []string{"lead"}) {
			t.Errorf("tags = %v, want [lead]", got.Tags)
		}
	})

	t.Run("deal: change only stage keeps title/value/currency/notes", func(t *testing.T) {
		var contact models.Contact
		mustJSON(t, callTool(t, c, ctx, "create_contact", map[string]any{"name": "Owner"}), &contact)
		var deal models.Deal
		mustJSON(t, callTool(t, c, ctx, "create_deal", map[string]any{
			"title": "Big", "contact_id": contact.ID, "value": 5000.0, "currency": "USD",
			"stage": "proposal", "notes": "hot",
		}), &deal)

		var got models.Deal
		mustJSON(t, callTool(t, c, ctx, "update_deal", map[string]any{
			"id": deal.ID, "stage": "won",
		}), &got)

		if got.Stage != models.StageWon {
			t.Errorf("stage = %q, want won", got.Stage)
		}
		if got.Title != "Big" || got.Value != 5000.0 || got.Currency != "USD" || got.Notes != "hot" || got.ContactID != contact.ID {
			t.Errorf("partial update lost a field: %+v", got)
		}
	})

	t.Run("company: change only website keeps name/industry/phone/notes", func(t *testing.T) {
		var company models.Company
		mustJSON(t, callTool(t, c, ctx, "create_company", map[string]any{
			"name": "Acme", "website": "old.io", "industry": "Tech", "phone": "555-3", "notes": "n",
		}), &company)

		var got models.Company
		mustJSON(t, callTool(t, c, ctx, "update_company", map[string]any{
			"id": company.ID, "website": "new.io",
		}), &got)

		if got.Website != "new.io" || got.Name != "Acme" || got.Industry != "Tech" || got.Phone != "555-3" || got.Notes != "n" {
			t.Errorf("partial update lost a field: %+v", got)
		}
	})

	t.Run("offer: change only body keeps title/description/subject", func(t *testing.T) {
		var lead models.Lead
		mustJSON(t, callTool(t, c, ctx, "create_lead", map[string]any{"name": "P"}), &lead)
		var offer models.Offer
		mustJSON(t, callTool(t, c, ctx, "create_offer", map[string]any{
			"lead_id": lead.ID, "title": "T", "description": "d", "subject": "s", "body": "old",
		}), &offer)

		var got models.Offer
		mustJSON(t, callTool(t, c, ctx, "update_offer", map[string]any{
			"id": offer.ID, "lead_id": lead.ID, "body": "new",
		}), &got)

		if got.Body != "new" || got.Title != "T" || got.Description != "d" || got.Subject != "s" {
			t.Errorf("partial update lost a field: %+v", got)
		}
	})

	t.Run("explicit empty string clears a field", func(t *testing.T) {
		var lead models.Lead
		mustJSON(t, callTool(t, c, ctx, "create_lead", map[string]any{"name": "N", "notes": "remove me"}), &lead)

		var got models.Lead
		mustJSON(t, callTool(t, c, ctx, "update_lead", map[string]any{
			"id": lead.ID, "notes": "",
		}), &got)

		if got.Notes != "" {
			t.Errorf("notes = %q, want cleared", got.Notes)
		}
		if got.Name != "N" {
			t.Errorf("name = %q, want preserved", got.Name)
		}
	})
}

// equalStrings reports whether two string slices have the same elements in order.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mustJSON asserts a successful tool result and unmarshals its structuredContent
// payload into v (widget-bearing tools keep their JSON there; the text block is a
// human status line).
func mustJSON(t *testing.T, res *mcp.CallToolResult, v any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned error: %s", resultText(t, res))
	}
	decodeStructured(t, res, v)
}

// TestLeadUnavailabilityTools covers the unavailable_until round trip through
// the MCP surface: recording a block on create, clearing it on update,
// rejecting a malformed date, and filtering/ordering by availability. The test
// store runs on the real clock, so the fixtures use dates far enough from today
// that the assertions can never flip.
func TestLeadUnavailabilityTools(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	const (
		farFuture  = "2099-03-01"
		nearFuture = "2099-01-15"
		longPast   = "2000-01-01"
	)

	create := func(name, until string) models.Lead {
		t.Helper()
		args := map[string]any{"name": name}
		if until != "" {
			args["unavailable_until"] = until
		}
		res := callTool(t, c, ctx, "create_lead", args)
		if res.IsError {
			t.Fatalf("create_lead(%s): %s", name, resultText(t, res))
		}
		var l models.Lead
		mustJSON(t, res, &l)
		return l
	}

	away := create("Away", farFuture)
	soon := create("Soon", nearFuture)
	expired := create("Expired", longPast)
	open := create("Open", "")

	if got := away.UnavailableUntil.Format(models.DateLayout); got != farFuture {
		t.Errorf("create stored %q, want %q", got, farFuture)
	}
	if !open.UnavailableUntil.IsZero() {
		t.Errorf("omitted unavailable_until stored %v, want zero", open.UnavailableUntil)
	}

	// listLeads decodes the minimal list-item projection the tool returns.
	type listItem struct {
		ID               uint64 `json:"id"`
		UnavailableUntil string `json:"unavailableUntil"`
	}
	listLeads := func(args map[string]any) []listItem {
		t.Helper()
		res := callTool(t, c, ctx, "list_leads", args)
		if res.IsError {
			t.Fatalf("list_leads(%v): %s", args, resultText(t, res))
		}
		var page struct {
			Leads []listItem `json:"leads"`
		}
		decodeStructured(t, res, &page)
		return page.Leads
	}

	idsOf := func(items []listItem) []uint64 {
		out := make([]uint64, len(items))
		for i, it := range items {
			out[i] = it.ID
		}
		return out
	}
	sameSet := func(got, want []uint64) bool {
		if len(got) != len(want) {
			return false
		}
		seen := make(map[uint64]int, len(got))
		for _, id := range got {
			seen[id]++
		}
		for _, id := range want {
			seen[id]--
		}
		for _, n := range seen {
			if n != 0 {
				return false
			}
		}
		return true
	}

	t.Run("list item carries the date", func(t *testing.T) {
		for _, it := range listLeads(map[string]any{}) {
			if it.ID == away.ID && it.UnavailableUntil != farFuture {
				t.Errorf("list item unavailableUntil = %q, want %q", it.UnavailableUntil, farFuture)
			}
			if it.ID == open.ID && it.UnavailableUntil != "" {
				t.Errorf("unblocked list item unavailableUntil = %q, want empty", it.UnavailableUntil)
			}
		}
	})

	t.Run("availability filter", func(t *testing.T) {
		gotAway := idsOf(listLeads(map[string]any{"availability": "unavailable"}))
		if want := []uint64{away.ID, soon.ID}; !sameSet(gotAway, want) {
			t.Errorf("unavailable = %v, want %v", gotAway, want)
		}
		gotOpen := idsOf(listLeads(map[string]any{"availability": "available"}))
		if want := []uint64{expired.ID, open.ID}; !sameSet(gotOpen, want) {
			t.Errorf("available = %v, want %v", gotOpen, want)
		}
	})

	t.Run("sort by unavailable-until ascending", func(t *testing.T) {
		got := idsOf(listLeads(map[string]any{
			"availability": "unavailable", "sort_by": "unavailable-until", "order": "asc",
		}))
		want := []uint64{soon.ID, away.ID}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("order = %v, want %v (soonest return first)", got, want)
		}
	})

	t.Run("empty string clears the block", func(t *testing.T) {
		res := callTool(t, c, ctx, "update_lead", map[string]any{
			"id": away.ID, "unavailable_until": "",
		})
		if res.IsError {
			t.Fatalf("update_lead: %s", resultText(t, res))
		}
		var got models.Lead
		mustJSON(t, res, &got)
		if !got.UnavailableUntil.IsZero() {
			t.Errorf("UnavailableUntil = %v, want zero", got.UnavailableUntil)
		}
		// Restore the fixture for any later subtest.
		callTool(t, c, ctx, "update_lead", map[string]any{"id": away.ID, "unavailable_until": farFuture})
	})

	t.Run("omitting the field keeps the stored date", func(t *testing.T) {
		res := callTool(t, c, ctx, "update_lead", map[string]any{"id": soon.ID, "name": "Soon renamed"})
		if res.IsError {
			t.Fatalf("update_lead: %s", resultText(t, res))
		}
		var got models.Lead
		mustJSON(t, res, &got)
		if s := got.UnavailableUntil.Format(models.DateLayout); s != nearFuture {
			t.Errorf("UnavailableUntil = %q, want %q", s, nearFuture)
		}
	})

	t.Run("malformed dates are tool errors", func(t *testing.T) {
		for _, bad := range []string{"15-08-2099", "next tuesday", "2099-02-30", "2099-01-15T00:00:00Z"} {
			res := callTool(t, c, ctx, "update_lead", map[string]any{"id": soon.ID, "unavailable_until": bad})
			if !res.IsError {
				t.Errorf("update_lead(unavailable_until=%q): expected a tool error", bad)
			}
			res = callTool(t, c, ctx, "create_lead", map[string]any{"name": "Bad", "unavailable_until": bad})
			if !res.IsError {
				t.Errorf("create_lead(unavailable_until=%q): expected a tool error", bad)
			}
		}
	})

	t.Run("invalid availability filter is a tool error", func(t *testing.T) {
		if res := callTool(t, c, ctx, "list_leads", map[string]any{"availability": "maybe"}); !res.IsError {
			t.Error("expected a tool error for availability=maybe")
		}
	})
}

// TestTriagePromptFlagsUnavailableLeads checks that the triage prompt marks a
// lead that is currently away, so the model defers instead of recommending an
// immediate contact, and leaves a reachable lead unmarked.
func TestTriagePromptFlagsUnavailableLeads(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	// The test store runs on the real clock, so the fixtures sit far from today.
	callTool(t, c, ctx, "create_lead", map[string]any{"name": "OnHoliday", "unavailable_until": "2099-03-01"})
	callTool(t, c, ctx, "create_lead", map[string]any{"name": "Reachable"})
	callTool(t, c, ctx, "create_lead", map[string]any{"name": "BlockElapsed", "unavailable_until": "2000-01-01"})

	req := mcp.GetPromptRequest{}
	req.Params.Name = "triage_new_leads"
	res, err := c.GetPrompt(ctx, req)
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(res.Messages) == 0 {
		t.Fatal("expected at least one prompt message")
	}
	text, ok := mcp.AsTextContent(res.Messages[0].Content)
	if !ok {
		t.Fatalf("prompt content is not text: %T", res.Messages[0].Content)
	}

	for _, want := range []string{"OnHoliday [away until 2099-03-01]", "away until DATE"} {
		if !strings.Contains(text.Text, want) {
			t.Errorf("prompt missing %q in:\n%s", want, text.Text)
		}
	}
	// A lead with no block, and one whose block has elapsed, are both reachable
	// and must not carry the marker.
	for _, name := range []string{"Reachable", "BlockElapsed"} {
		if strings.Contains(text.Text, name+" [away") {
			t.Errorf("%s wrongly marked away in:\n%s", name, text.Text)
		}
	}
}
