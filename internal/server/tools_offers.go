package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

type createOfferArgs struct {
	LeadID      uint64 `json:"lead_id" jsonschema:"Owning lead id (must exist)"`
	Title       string `json:"title" jsonschema:"Offer title (required)"`
	Description string `json:"description,omitempty" jsonschema:"Short description of the offer"`
	Subject     string `json:"subject,omitempty" jsonschema:"Email subject line"`
	Body        string `json:"body,omitempty" jsonschema:"Raw email body content (may be long, multi-line)"`
}

type listOffersArgs struct {
	LeadID uint64 `json:"lead_id,omitempty" jsonschema:"Filter by owning lead id (0 = all)"`
}

type updateOfferArgs struct {
	ID          uint64 `json:"id" jsonschema:"Offer id"`
	LeadID      uint64 `json:"lead_id" jsonschema:"Owning lead id (must exist)"`
	Title       string `json:"title" jsonschema:"Offer title (required)"`
	Description string `json:"description,omitempty" jsonschema:"Short description of the offer"`
	Subject     string `json:"subject,omitempty" jsonschema:"Email subject line"`
	Body        string `json:"body,omitempty" jsonschema:"Raw email body content"`
}

func (h *handlers) registerOfferTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"create_offer",
		mcp.WithDescription("Create an offer for an existing lead."),
		mcp.WithInputSchema[createOfferArgs](),
	), mcp.NewTypedToolHandler(h.createOffer))

	s.AddTool(mcp.NewTool(
		"list_offers",
		mcp.WithDescription("List offers, optionally filtered by owning lead."),
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
	offers, err := h.store.ListOffers(db.OfferFilter{LeadID: a.LeadID})
	if err != nil {
		return toolErr(err)
	}
	return listResult("offers", offers)
}

func (h *handlers) getOffer(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	o, err := h.store.GetOffer(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(o)
}

func (h *handlers) updateOffer(_ context.Context, _ mcp.CallToolRequest, a updateOfferArgs) (*mcp.CallToolResult, error) {
	o, err := h.store.UpdateOffer(models.Offer{
		ID: a.ID, LeadID: a.LeadID, Title: a.Title, Description: a.Description, Subject: a.Subject, Body: a.Body,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(o)
}

func (h *handlers) deleteOffer(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	if err := h.store.DeleteOffer(a.ID); err != nil {
		return toolErr(err)
	}
	return jsonResult(map[string]any{"deleted": a.ID})
}
