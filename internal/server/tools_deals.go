package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/gadget"
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
	Query     string `json:"query,omitempty" jsonschema:"Substring match on title/company (blank = all)"`
	SortBy    string `json:"sort_by,omitempty" jsonschema:"Order by: updated (default) or created"`
	Order     string `json:"order,omitempty" jsonschema:"Sort direction: desc (default, most-recently-updated first) or asc"`
	Page      int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"Results per page, 1-50 (default 50; higher values are clamped to 50)"`
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

	listDeals := mcp.NewTool(
		"list_deals",
		mcp.WithDescription("List deals (minimal fields; use get_deal for the full record), optionally filtered by stage, contact, and/or substring query, with ordering (updated default/created) and pagination (max page size 50). Returns the page plus total/total_pages/has_more."),
		mcp.WithInputSchema[listDealsArgs](),
	)
	listDeals.Meta = uiToolMeta(appDeals) // surfaces list_deals as an MCP App (deals table template)
	s.AddTool(listDeals, mcp.NewTypedToolHandler(h.listDeals))

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
		errs := dealFieldErrors(err)
		res := formErrorResult(errs, err.Error())
		embedWidget(res, dealForm("create_deal", createDealValues(a), errs))
		return res, nil
	}
	res := okResult(d, fmt.Sprintf("Deal #%d created.", d.ID))
	embedWidget(res, dealForm("update_deal", dealValues(d), nil))
	return res, nil
}

func (h *handlers) listDeals(_ context.Context, _ mcp.CallToolRequest, a listDealsArgs) (*mcp.CallToolResult, error) {
	page, err := h.store.QueryDeals(db.DealQuery{
		ContactID: a.ContactID,
		Stage:     models.DealStage(a.Stage),
		Search:    a.Query,
		SortBy:    db.DealSort(a.SortBy),
		Asc:       strings.EqualFold(strings.TrimSpace(a.Order), "asc"),
		Page:      a.Page,
		PageSize:  a.PageSize,
	})
	if err != nil {
		return toolErr(err)
	}
	items := toDealListItems(page.Deals)
	res := pageResult(dealsRowsKey, listStatus(page.Total, "deal", "deals"), items,
		page.Page, page.PageSize, page.Total, page.TotalPages, page.HasMore)
	embedTable(res, func(rows []map[string]any) *gadget.Table { return dealsTable("Deals", rows) }, items)
	return res, nil
}

func (h *handlers) getDeal(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	d, err := h.store.GetDeal(a.ID)
	if err != nil {
		return toolErr(err)
	}
	res := okResult(d, fmt.Sprintf("Deal #%d.", d.ID))
	embedTable(res, func(rows []map[string]any) *gadget.Table {
		return dealsTable(fmt.Sprintf("Deal #%d", d.ID), rows)
	}, []dealListItem{toDealListItem(d)})
	return res, nil
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
		errs := dealFieldErrors(err)
		res := formErrorResult(errs, err.Error())
		embedWidget(res, dealForm("update_deal", dealValues(d), errs))
		return res, nil
	}
	res := okResult(updated, fmt.Sprintf("Deal #%d updated.", updated.ID))
	embedWidget(res, dealForm("update_deal", dealValues(updated), nil))
	return res, nil
}

func (h *handlers) deleteDeal(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	if err := h.store.DeleteDeal(a.ID); err != nil {
		return toolErr(err)
	}
	items := h.latestDeals()
	res := okResult(map[string]any{"deleted": a.ID, dealsRowsKey: items},
		fmt.Sprintf("Deal #%d deleted.", a.ID))
	embedTable(res, func(rows []map[string]any) *gadget.Table { return dealsTable("Deals", rows) }, items)
	return res, nil
}

func (h *handlers) registerSummaryTool(s *server.MCPServer) {
	summaryTool := mcp.NewTool(
		"pipeline_summary",
		mcp.WithDescription("Funnel + pipeline aggregate: deal counts and per-currency value totals by stage, plus lead counts by status."),
	)
	summaryTool.Meta = uiToolMeta(appPipeline) // surfaces pipeline_summary as an MCP App (deals-by-stage table template)
	s.AddTool(summaryTool, func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		summary, err := h.store.PipelineSummary()
		if err != nil {
			return toolErr(err)
		}
		dealRows, statusRows := pipelineSummaryRows(summary)
		// The two summary tables re-hydrate (LoadTool: pipeline_summary) from the
		// flattened rows carried under their RowsKeys, so the payload embeds the
		// summary (model contract) plus dealRows/statusRows.
		res := okResult(summaryOutput{PipelineSummary: summary, DealRows: dealRows, StatusRows: statusRows},
			"Pipeline summary.")
		embedPipelineSummary(res, dealRows, statusRows)
		return res, nil
	})
}

// summaryOutput is pipeline_summary's structuredContent: the PipelineSummary
// (model contract) plus the two flattened table row sets under the summary
// tables' RowsKeys, so each read-only table's LoadTool re-hydrates on remount.
type summaryOutput struct {
	models.PipelineSummary
	DealRows   []stageSummaryRow `json:"dealRows"`
	StatusRows []statusCountRow  `json:"statusRows"`
}
