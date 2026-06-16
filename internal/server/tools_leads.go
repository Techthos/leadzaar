package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
)

// Lead tool argument structs. JSON tags drive both the generated input schema
// and argument binding.

type createLeadArgs struct {
	Name      string   `json:"name" jsonschema:"Lead name (required)"`
	CompanyID uint64   `json:"company_id,omitempty" jsonschema:"Linked Company id (0 or omitted = none)"`
	Email     string   `json:"email,omitempty" jsonschema:"Email address (optional)"`
	Phone     string   `json:"phone,omitempty" jsonschema:"Phone number"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Freeform tags"`
	Quality   int      `json:"quality,omitempty" jsonschema:"Lead quality score 1-10 (0 or omitted = unscored)"`
	Source    string   `json:"source,omitempty" jsonschema:"Lead source: web, referral, event, cold-outreach, or other"`
	Notes     string   `json:"notes,omitempty" jsonschema:"Freeform notes"`
}

type listLeadsArgs struct {
	Status string `json:"status,omitempty" jsonschema:"Filter by status: new, contacted, qualified, converted, lost (blank = all)"`
}

type idArg struct {
	ID uint64 `json:"id" jsonschema:"Record id"`
}

type updateLeadArgs struct {
	ID        uint64   `json:"id" jsonschema:"Lead id"`
	Name      string   `json:"name" jsonschema:"Lead name (required)"`
	CompanyID uint64   `json:"company_id,omitempty" jsonschema:"Linked Company id (0 = unlink)"`
	Email     string   `json:"email,omitempty" jsonschema:"Email address"`
	Phone     string   `json:"phone,omitempty" jsonschema:"Phone number"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Freeform tags"`
	Quality   int      `json:"quality,omitempty" jsonschema:"Lead quality score 1-10 (0 = unscored)"`
	Source    string   `json:"source,omitempty" jsonschema:"Lead source enum"`
	Status    string   `json:"status,omitempty" jsonschema:"Lead status enum"`
	Notes     string   `json:"notes,omitempty" jsonschema:"Freeform notes"`
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
		mcp.WithDescription("List leads newest-first, optionally filtered by status."),
		mcp.WithInputSchema[listLeadsArgs](),
	), mcp.NewTypedToolHandler(h.listLeads))

	s.AddTool(mcp.NewTool(
		"get_lead",
		mcp.WithDescription("Fetch a single lead by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.getLead))

	s.AddTool(mcp.NewTool(
		"update_lead",
		mcp.WithDescription("Update a lead's editable fields and status."),
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
	lead, err := h.store.CreateLead(models.Lead{
		Name: a.Name, CompanyID: a.CompanyID, Email: a.Email, Phone: a.Phone,
		Tags: a.Tags, Quality: a.Quality, Source: models.Source(a.Source), Notes: a.Notes,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(lead)
}

func (h *handlers) listLeads(_ context.Context, _ mcp.CallToolRequest, a listLeadsArgs) (*mcp.CallToolResult, error) {
	leads, err := h.store.ListLeads(models.LeadStatus(a.Status))
	if err != nil {
		return toolErr(err)
	}
	return listResult("leads", leads)
}

func (h *handlers) getLead(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	lead, err := h.store.GetLead(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(lead)
}

func (h *handlers) updateLead(_ context.Context, _ mcp.CallToolRequest, a updateLeadArgs) (*mcp.CallToolResult, error) {
	lead, err := h.store.UpdateLead(models.Lead{
		ID: a.ID, Name: a.Name, CompanyID: a.CompanyID, Email: a.Email, Phone: a.Phone,
		Tags: a.Tags, Quality: a.Quality, Source: models.Source(a.Source), Status: models.LeadStatus(a.Status), Notes: a.Notes,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(lead)
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
	if err := h.store.DeleteLead(a.ID); err != nil {
		return toolErr(err)
	}
	return jsonResult(map[string]any{"deleted": a.ID})
}
