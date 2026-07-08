package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/leadzaar/internal/db"
	"github.com/techthos/leadzaar/internal/models"
)

type createDealArgs struct {
	Title     string  `json:"title" jsonschema:"Deal title (required)"`
	ContactID uint64  `json:"contact_id" jsonschema:"Owning contact id (must exist)"`
	CompanyID uint64  `json:"company_id,omitempty" jsonschema:"Linked Company id (0 or omitted = none)"`
	Value     float64 `json:"value,omitempty" jsonschema:"Monetary value"`
	Currency  string  `json:"currency,omitempty" jsonschema:"3-letter currency code (required for non-zero value)"`
	Stage     string  `json:"stage" jsonschema:"Stage: qualification, proposal, negotiation, won, lost"`
	Notes     string  `json:"notes,omitempty" jsonschema:"Freeform notes"`
}

type listDealsArgs struct {
	Stage     string `json:"stage,omitempty" jsonschema:"Filter by stage (blank = all)"`
	ContactID uint64 `json:"contact_id,omitempty" jsonschema:"Filter by owning contact id (0 = all)"`
}

// updateDealArgs is a partial update: only id is required; omitted editable
// fields keep their stored value (see setIf and h.updateDeal).
type updateDealArgs struct {
	ID        uint64   `json:"id" jsonschema:"Deal id (required)"`
	Title     *string  `json:"title,omitempty" jsonschema:"Deal title; omit to keep, must be non-empty if set"`
	ContactID *uint64  `json:"contact_id,omitempty" jsonschema:"Owning contact id; omit to keep"`
	CompanyID *uint64  `json:"company_id,omitempty" jsonschema:"Linked Company id (0 = unlink); omit to keep"`
	Value     *float64 `json:"value,omitempty" jsonschema:"Monetary value; omit to keep"`
	Currency  *string  `json:"currency,omitempty" jsonschema:"3-letter currency code; omit to keep"`
	Stage     *string  `json:"stage,omitempty" jsonschema:"Stage enum; omit to keep"`
	Notes     *string  `json:"notes,omitempty" jsonschema:"Freeform notes; omit to keep"`
}

func (h *handlers) registerDealTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"create_deal",
		mcp.WithDescription("Create a deal for an existing contact."),
		mcp.WithInputSchema[createDealArgs](),
	), mcp.NewTypedToolHandler(h.createDeal))

	s.AddTool(mcp.NewTool(
		"list_deals",
		mcp.WithDescription("List deals, optionally filtered by stage and/or contact."),
		mcp.WithInputSchema[listDealsArgs](),
	), mcp.NewTypedToolHandler(h.listDeals))

	s.AddTool(mcp.NewTool(
		"get_deal",
		mcp.WithDescription("Fetch a single deal by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.getDeal))

	s.AddTool(mcp.NewTool(
		"update_deal",
		mcp.WithDescription("Update a deal's editable fields and stage."),
		mcp.WithInputSchema[updateDealArgs](),
	), mcp.NewTypedToolHandler(h.updateDeal))

	s.AddTool(mcp.NewTool(
		"delete_deal",
		mcp.WithDescription("Delete a deal by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.deleteDeal))
}

func (h *handlers) createDeal(_ context.Context, _ mcp.CallToolRequest, a createDealArgs) (*mcp.CallToolResult, error) {
	d, err := h.store.CreateDeal(models.Deal{
		Title: a.Title, ContactID: a.ContactID, CompanyID: a.CompanyID, Value: a.Value, Currency: a.Currency,
		Stage: models.DealStage(a.Stage), Notes: a.Notes,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(d)
}

func (h *handlers) listDeals(_ context.Context, _ mcp.CallToolRequest, a listDealsArgs) (*mcp.CallToolResult, error) {
	deals, err := h.store.ListDeals(db.DealFilter{ContactID: a.ContactID, Stage: models.DealStage(a.Stage)})
	if err != nil {
		return toolErr(err)
	}
	return listResult("deals", deals)
}

func (h *handlers) getDeal(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	d, err := h.store.GetDeal(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(d)
}

func (h *handlers) updateDeal(_ context.Context, _ mcp.CallToolRequest, a updateDealArgs) (*mcp.CallToolResult, error) {
	d, err := h.store.GetDeal(a.ID)
	if err != nil {
		return toolErr(err)
	}
	setIf(&d.Title, a.Title)
	setIf(&d.ContactID, a.ContactID)
	setIf(&d.CompanyID, a.CompanyID)
	setIf(&d.Value, a.Value)
	setIf(&d.Currency, a.Currency)
	setIf(&d.Notes, a.Notes)
	if a.Stage != nil {
		d.Stage = models.DealStage(*a.Stage)
	}
	updated, err := h.store.UpdateDeal(d)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(updated)
}

func (h *handlers) deleteDeal(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	if err := h.store.DeleteDeal(a.ID); err != nil {
		return toolErr(err)
	}
	return jsonResult(map[string]any{"deleted": a.ID})
}

func (h *handlers) registerSummaryTool(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"pipeline_summary",
		mcp.WithDescription("Funnel + pipeline aggregate: deal counts and per-currency value totals by stage, plus lead counts by status."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		summary, err := h.store.PipelineSummary()
		if err != nil {
			return toolErr(err)
		}
		return jsonResult(summary)
	})
}
