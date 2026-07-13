package server

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

type createOfferArgs struct {
	LeadID      uint64 `json:"lead_id" jsonschema:"Owning lead id (must exist)"`
	Title       string `json:"title" jsonschema:"Offer title (required)"`
	Description string `json:"description,omitempty" jsonschema:"Short description of the offer"`
	Subject     string `json:"subject,omitempty" jsonschema:"Email subject line"`
	Body        string `json:"body,omitempty" jsonschema:"Raw email body content (may be long, multi-line)"`
}

type listOffersArgs struct {
	LeadID   uint64 `json:"lead_id,omitempty" jsonschema:"Filter by owning lead id (0 = all)"`
	Query    string `json:"query,omitempty" jsonschema:"Substring match on title/subject (blank = all)"`
	SortBy   string `json:"sort_by,omitempty" jsonschema:"Order by: updated (default) or created"`
	Order    string `json:"order,omitempty" jsonschema:"Sort direction: desc (default, most-recently-updated first) or asc"`
	Page     int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"Results per page, 1-50 (default 50; higher values are clamped to 50)"`
}

// updateOfferArgs is a partial update: id and lead_id are required (lead_id
// confirms/sets the owning lead, per the spec); the remaining editable fields
// are pointers so an omitted one keeps its stored value (see setIf).
type updateOfferArgs struct {
	ID          uint64  `json:"id" jsonschema:"Offer id (required)"`
	LeadID      uint64  `json:"lead_id" jsonschema:"Owning lead id (required, must exist)"`
	Title       *string `json:"title,omitempty" jsonschema:"Offer title; omit to keep, must be non-empty if set"`
	Description *string `json:"description,omitempty" jsonschema:"Short description of the offer; omit to keep"`
	Subject     *string `json:"subject,omitempty" jsonschema:"Email subject line; omit to keep"`
	Body        *string `json:"body,omitempty" jsonschema:"Raw email body content; omit to keep"`
}

func (h *handlers) registerOfferTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"create_offer",
		mcp.WithDescription("Create an offer for an existing lead."),
		mcp.WithInputSchema[createOfferArgs](),
	), mcp.NewTypedToolHandler(h.createOffer))

	s.AddTool(mcp.NewTool(
		"list_offers",
		mcp.WithDescription("List offers (minimal fields; use get_offer for the full record incl. body), optionally filtered by owning lead and/or substring query, with ordering (updated default/created) and pagination (max page size 50). Returns the page plus total/total_pages/has_more."),
		mcp.WithInputSchema[listOffersArgs](),
	), mcp.NewTypedToolHandler(h.listOffers))

	s.AddTool(mcp.NewTool(
		"get_offer",
		mcp.WithDescription("Fetch a single offer by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.getOffer))

	s.AddTool(mcp.NewTool(
		"update_offer",
		mcp.WithDescription("Update an offer's editable fields."),
		mcp.WithInputSchema[updateOfferArgs](),
	), mcp.NewTypedToolHandler(h.updateOffer))

	s.AddTool(mcp.NewTool(
		"delete_offer",
		mcp.WithDescription("Delete an offer by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.deleteOffer))
}

func (h *handlers) createOffer(_ context.Context, _ mcp.CallToolRequest, a createOfferArgs) (*mcp.CallToolResult, error) {
	o, err := h.store.CreateOffer(models.Offer{
		LeadID: a.LeadID, Title: a.Title, Description: a.Description, Subject: a.Subject, Body: a.Body,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(o)
}

func (h *handlers) listOffers(_ context.Context, _ mcp.CallToolRequest, a listOffersArgs) (*mcp.CallToolResult, error) {
	page, err := h.store.QueryOffers(db.OfferQuery{
		LeadID:   a.LeadID,
		Search:   a.Query,
		SortBy:   db.OfferSort(a.SortBy),
		Asc:      strings.EqualFold(strings.TrimSpace(a.Order), "asc"),
		Page:     a.Page,
		PageSize: a.PageSize,
	})
	if err != nil {
		return toolErr(err)
	}
	return pageResult("offers", toOfferListItems(page.Offers),
		page.Page, page.PageSize, page.Total, page.TotalPages, page.HasMore)
}

func (h *handlers) getOffer(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	o, err := h.store.GetOffer(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(o)
}

func (h *handlers) updateOffer(_ context.Context, _ mcp.CallToolRequest, a updateOfferArgs) (*mcp.CallToolResult, error) {
	o, err := h.store.GetOffer(a.ID)
	if err != nil {
		return toolErr(err)
	}
	o.LeadID = a.LeadID
	setIf(&o.Title, a.Title)
	setIf(&o.Description, a.Description)
	setIf(&o.Subject, a.Subject)
	setIf(&o.Body, a.Body)
	updated, err := h.store.UpdateOffer(o)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(updated)
}

func (h *handlers) deleteOffer(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	if err := h.store.DeleteOffer(a.ID); err != nil {
		return toolErr(err)
	}
	return jsonResult(map[string]any{"deleted": a.ID})
}
