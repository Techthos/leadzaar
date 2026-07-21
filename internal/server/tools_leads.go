package server

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

// Lead tool argument structs. JSON tags drive both the generated input schema
// and argument binding.

type createLeadArgs struct {
	Name             string   `json:"name" jsonschema:"Lead name (required)"`
	CompanyID        uint64   `json:"company_id,omitempty" jsonschema:"Linked Company id (0 or omitted = none)"`
	Email            string   `json:"email,omitempty" jsonschema:"Email address (optional)"`
	Phone            string   `json:"phone,omitempty" jsonschema:"Phone number"`
	Tags             []string `json:"tags,omitempty" jsonschema:"Freeform tags"`
	Quality          int      `json:"quality,omitempty" jsonschema:"Lead quality score 1-10 (0 or omitted = unscored)"`
	Source           string   `json:"source,omitempty" jsonschema:"Lead source: web, referral, event, cold-outreach, or other"`
	Notes            string   `json:"notes,omitempty" jsonschema:"Freeform notes"`
	UnavailableUntil string   `json:"unavailable_until,omitempty" jsonschema:"Date the lead is unreachable until, as YYYY-MM-DD - e.g. transcribed from an out-of-office autoresponder. Exclusive: the lead is reachable again on that date. Blank = no known block."`
}

type listLeadsArgs struct {
	Status       string `json:"status,omitempty" jsonschema:"Filter by status: new, contacted, contacted-first-touch, contacted-followup-1, contacted-followup-2, contacted-followup-3, qualified, converted, lost (blank = all)"`
	Availability string `json:"availability,omitempty" jsonschema:"Filter by whether the lead is reachable today: available (no block, or it has elapsed) or unavailable (still away). Blank = all."`
	Query        string `json:"query,omitempty" jsonschema:"Case-insensitive substring match on name/company/email/tag (blank = all)"`
	SortBy       string `json:"sort_by,omitempty" jsonschema:"Order by: updated (default), created, quality, or unavailable-until. Leads with no block sort last when ascending."`
	Order        string `json:"order,omitempty" jsonschema:"Sort direction: desc (default, newest/highest first) or asc"`
	Page         int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
	PageSize     int    `json:"page_size,omitempty" jsonschema:"Results per page, 1-50 (default 50; higher values are clamped to 50)"`
}

type idArg struct {
	ID uint64 `json:"id" jsonschema:"Record id"`
}

// updateLeadArgs is a partial update: only id is required, and every editable
// field is a pointer (or, for tags, a slice) so an omitted field keeps its
// stored value instead of being reset. See setIf and h.updateLead.
type updateLeadArgs struct {
	ID               uint64   `json:"id" jsonschema:"Lead id (required)"`
	Name             *string  `json:"name,omitempty" jsonschema:"Lead name; omit to keep, must be non-empty if set"`
	CompanyID        *uint64  `json:"company_id,omitempty" jsonschema:"Linked Company id (0 = unlink); omit to keep"`
	Email            *string  `json:"email,omitempty" jsonschema:"Email address; omit to keep"`
	Phone            *string  `json:"phone,omitempty" jsonschema:"Phone number; omit to keep"`
	Tags             []string `json:"tags,omitempty" jsonschema:"Freeform tags; omit to keep, send [] to clear"`
	Quality          *int     `json:"quality,omitempty" jsonschema:"Lead quality score 1-10 (0 = unscored); omit to keep"`
	Source           *string  `json:"source,omitempty" jsonschema:"Lead source enum; omit to keep"`
	Status           *string  `json:"status,omitempty" jsonschema:"Lead status enum; omit to keep"`
	Notes            *string  `json:"notes,omitempty" jsonschema:"Freeform notes; omit to keep"`
	UnavailableUntil *string  `json:"unavailable_until,omitempty" jsonschema:"Date the lead is unreachable until, as YYYY-MM-DD (e.g. from an out-of-office autoresponder); omit to keep, send \"\" to clear the block"`
}

type convertLeadArgs struct {
	ID           uint64  `json:"id" jsonschema:"Lead id to convert"`
	MakeDeal     bool    `json:"make_deal,omitempty" jsonschema:"Also create a deal for the new contact"`
	DealTitle    string  `json:"deal_title,omitempty" jsonschema:"Deal title (required if make_deal)"`
	DealValue    float64 `json:"deal_value,omitempty" jsonschema:"Deal monetary value"`
	DealCurrency string  `json:"deal_currency,omitempty" jsonschema:"Deal 3-letter currency code"`
}

func (h *handlers) registerLeadTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"create_lead",
		mcp.WithDescription("Create a new lead. Status defaults to 'new'."),
		mcp.WithInputSchema[createLeadArgs](),
	), mcp.NewTypedToolHandler(h.createLead))

	s.AddTool(mcp.NewTool(
		"list_leads",
		mcp.WithDescription("List leads (minimal fields; use get_lead for the full record) with optional status and availability filters, substring search, ordering (updated default/created/quality/unavailable-until), and pagination (max page size 50). Returns the page plus total/total_pages/has_more. To find who is ready for a follow-up now, filter availability=available; to see who is still away and when they return, use availability=unavailable with sort_by=unavailable-until and order=asc."),
		mcp.WithInputSchema[listLeadsArgs](),
	), mcp.NewTypedToolHandler(h.listLeads))

	s.AddTool(mcp.NewTool(
		"get_lead",
		mcp.WithDescription("Fetch a single lead by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.getLead))

	s.AddTool(mcp.NewTool(
		"update_lead",
		mcp.WithDescription("Update a lead's editable fields and status. Partial: omitted fields keep their stored value. Use unavailable_until to record an out-of-office date read from an autoresponder, or send \"\" to clear it."),
		mcp.WithInputSchema[updateLeadArgs](),
	), mcp.NewTypedToolHandler(h.updateLead))

	s.AddTool(mcp.NewTool(
		"convert_lead",
		mcp.WithDescription("Convert a lead into a contact, optionally creating a deal."),
		mcp.WithInputSchema[convertLeadArgs](),
	), mcp.NewTypedToolHandler(h.convertLead))

	s.AddTool(mcp.NewTool(
		"delete_lead",
		mcp.WithDescription("Delete a lead by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.deleteLead))
}

func (h *handlers) createLead(_ context.Context, _ mcp.CallToolRequest, a createLeadArgs) (*mcp.CallToolResult, error) {
	unavailableUntil, err := models.ParseDate(a.UnavailableUntil)
	if err != nil {
		return toolErr(err)
	}
	lead, err := h.store.CreateLead(models.Lead{
		Name: a.Name, CompanyID: a.CompanyID, Email: a.Email, Phone: a.Phone,
		Tags: a.Tags, Quality: a.Quality, Source: models.Source(a.Source), Notes: a.Notes,
		UnavailableUntil: unavailableUntil,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(lead)
}

func (h *handlers) listLeads(_ context.Context, _ mcp.CallToolRequest, a listLeadsArgs) (*mcp.CallToolResult, error) {
	page, err := h.store.QueryLeads(db.LeadQuery{
		Status:       models.LeadStatus(a.Status),
		Availability: db.LeadAvailability(strings.TrimSpace(a.Availability)),
		Search:       a.Query,
		SortBy:       db.LeadSort(a.SortBy),
		Asc:          strings.EqualFold(strings.TrimSpace(a.Order), "asc"),
		Page:         a.Page,
		PageSize:     a.PageSize,
	})
	if err != nil {
		return toolErr(err)
	}
	return pageResult("leads", toLeadListItems(page.Leads),
		page.Page, page.PageSize, page.Total, page.TotalPages, page.HasMore)
}

func (h *handlers) getLead(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	lead, err := h.store.GetLead(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(lead)
}

func (h *handlers) updateLead(_ context.Context, _ mcp.CallToolRequest, a updateLeadArgs) (*mcp.CallToolResult, error) {
	lead, err := h.store.GetLead(a.ID)
	if err != nil {
		return toolErr(err)
	}
	setIf(&lead.Name, a.Name)
	setIf(&lead.CompanyID, a.CompanyID)
	setIf(&lead.Email, a.Email)
	setIf(&lead.Phone, a.Phone)
	setIf(&lead.Quality, a.Quality)
	setIf(&lead.Notes, a.Notes)
	if a.Tags != nil {
		lead.Tags = a.Tags
	}
	if a.UnavailableUntil != nil {
		// ParseDate maps "" to the zero time, so sending "" clears the block.
		until, perr := models.ParseDate(*a.UnavailableUntil)
		if perr != nil {
			return toolErr(perr)
		}
		lead.UnavailableUntil = until
	}
	if a.Source != nil {
		lead.Source = models.Source(*a.Source)
	}
	if a.Status != nil {
		lead.Status = models.LeadStatus(*a.Status)
	}
	updated, err := h.store.UpdateLead(lead)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(updated)
}

func (h *handlers) convertLead(_ context.Context, _ mcp.CallToolRequest, a convertLeadArgs) (*mcp.CallToolResult, error) {
	res, err := h.store.Convert(a.ID, db.ConvertOptions{
		MakeDeal: a.MakeDeal, DealTitle: a.DealTitle, DealValue: a.DealValue, DealCurrency: a.DealCurrency,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(res)
}

func (h *handlers) deleteLead(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	deletedOffers, err := h.store.DeleteLead(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(map[string]any{"deleted": a.ID, "deleted_offer_ids": deletedOffers})
}
