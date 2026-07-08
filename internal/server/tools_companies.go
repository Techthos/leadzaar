package server

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/leadzaar/internal/models"
)

type createCompanyArgs struct {
	Name     string `json:"name" jsonschema:"Company name (required)"`
	Website  string `json:"website,omitempty" jsonschema:"Website URL"`
	Industry string `json:"industry,omitempty" jsonschema:"Industry"`
	Phone    string `json:"phone,omitempty" jsonschema:"Phone number"`
	Notes    string `json:"notes,omitempty" jsonschema:"Freeform notes"`
}

type listCompaniesArgs struct {
	Query string `json:"query,omitempty" jsonschema:"Substring match on name/website/industry (blank = all)"`
}

// updateCompanyArgs is a partial update: only id is required; omitted editable
// fields keep their stored value (see setIf and h.updateCompany).
type updateCompanyArgs struct {
	ID       uint64  `json:"id" jsonschema:"Company id (required)"`
	Name     *string `json:"name,omitempty" jsonschema:"Company name; omit to keep, must be non-empty if set"`
	Website  *string `json:"website,omitempty" jsonschema:"Website URL; omit to keep"`
	Industry *string `json:"industry,omitempty" jsonschema:"Industry; omit to keep"`
	Phone    *string `json:"phone,omitempty" jsonschema:"Phone number; omit to keep"`
	Notes    *string `json:"notes,omitempty" jsonschema:"Freeform notes; omit to keep"`
}

func (h *handlers) registerCompanyTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"create_company",
		mcp.WithDescription("Create a new company that leads, contacts, and deals can link to."),
		mcp.WithInputSchema[createCompanyArgs](),
	), mcp.NewTypedToolHandler(h.createCompany))

	s.AddTool(mcp.NewTool(
		"list_companies",
		mcp.WithDescription("List or search companies by name, website, or industry substring."),
		mcp.WithInputSchema[listCompaniesArgs](),
	), mcp.NewTypedToolHandler(h.listCompanies))

	s.AddTool(mcp.NewTool(
		"get_company",
		mcp.WithDescription("Fetch a single company by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.getCompany))

	s.AddTool(mcp.NewTool(
		"update_company",
		mcp.WithDescription("Update a company's editable fields."),
		mcp.WithInputSchema[updateCompanyArgs](),
	), mcp.NewTypedToolHandler(h.updateCompany))

	s.AddTool(mcp.NewTool(
		"delete_company",
		mcp.WithDescription("Delete a company and unlink it from any referencing leads/contacts/deals (their company link is cleared; the records are kept)."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.deleteCompany))
}

func (h *handlers) createCompany(_ context.Context, _ mcp.CallToolRequest, a createCompanyArgs) (*mcp.CallToolResult, error) {
	c, err := h.store.CreateCompany(models.Company{
		Name: a.Name, Website: a.Website, Industry: a.Industry, Phone: a.Phone, Notes: a.Notes,
	})
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(c)
}

func (h *handlers) listCompanies(_ context.Context, _ mcp.CallToolRequest, a listCompaniesArgs) (*mcp.CallToolResult, error) {
	companies, err := h.store.SearchCompanies(a.Query)
	if err != nil {
		return toolErr(err)
	}
	return listResult("companies", companies)
}

func (h *handlers) getCompany(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	c, err := h.store.GetCompany(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(c)
}

func (h *handlers) updateCompany(_ context.Context, _ mcp.CallToolRequest, a updateCompanyArgs) (*mcp.CallToolResult, error) {
	c, err := h.store.GetCompany(a.ID)
	if err != nil {
		return toolErr(err)
	}
	setIf(&c.Name, a.Name)
	setIf(&c.Website, a.Website)
	setIf(&c.Industry, a.Industry)
	setIf(&c.Phone, a.Phone)
	setIf(&c.Notes, a.Notes)
	updated, err := h.store.UpdateCompany(c)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(updated)
}

func (h *handlers) deleteCompany(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	unlinked, err := h.store.DeleteCompany(a.ID)
	if err != nil {
		return toolErr(err)
	}
	return jsonResult(map[string]any{"deleted": a.ID, "unlinked": unlinked})
}
