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

type createContactArgs struct {
	Name      string   `json:"name" jsonschema:"Contact name (required)"`
	CompanyID uint64   `json:"company_id,omitempty" jsonschema:"Linked Company id (0 or omitted = none)"`
	Email     string   `json:"email,omitempty" jsonschema:"Email address"`
	Phone     string   `json:"phone,omitempty" jsonschema:"Phone number"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Freeform tags"`
	Notes     string   `json:"notes,omitempty" jsonschema:"Freeform notes"`
}

type listContactsArgs struct {
	Query    string `json:"query,omitempty" jsonschema:"Substring match on name/company/email/tag (blank = all)"`
	Email    string `json:"email,omitempty" jsonschema:"Exact email lookup via index"`
	Tag      string `json:"tag,omitempty" jsonschema:"Match contacts carrying this tag"`
	SortBy   string `json:"sort_by,omitempty" jsonschema:"Order by: updated (default) or created"`
	Order    string `json:"order,omitempty" jsonschema:"Sort direction: desc (default, most-recently-updated first) or asc"`
	Page     int    `json:"page,omitempty" jsonschema:"1-based page number (default 1)"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"Results per page, 1-50 (default 50; higher values are clamped to 50)"`
}

// updateContactArgs is a partial update: only id is required; omitted editable
// fields keep their stored value (see setIf and h.updateContact).
type updateContactArgs struct {
	ID        uint64   `json:"id" jsonschema:"Contact id (required)"`
	Name      *string  `json:"name,omitempty" jsonschema:"Contact name; omit to keep, must be non-empty if set"`
	CompanyID *uint64  `json:"company_id,omitempty" jsonschema:"Linked Company id (0 = unlink); omit to keep"`
	Email     *string  `json:"email,omitempty" jsonschema:"Email address; omit to keep"`
	Phone     *string  `json:"phone,omitempty" jsonschema:"Phone number; omit to keep"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Freeform tags; omit to keep, send [] to clear"`
	Notes     *string  `json:"notes,omitempty" jsonschema:"Freeform notes; omit to keep"`
}

func (h *handlers) registerContactTools(s *server.MCPServer) {
	s.AddTool(mcp.NewTool(
		"create_contact",
		mcp.WithDescription("Create a new contact directly (not via lead conversion)."),
		mcp.WithInputSchema[createContactArgs](),
	), mcp.NewTypedToolHandler(h.createContact))

	listContacts := mcp.NewTool(
		"list_contacts",
		mcp.WithDescription("List or search contacts (minimal fields; use get_contact for the full record) by query, exact email, or tag, with ordering (updated default/created) and pagination (max page size 50). Returns the page plus total/total_pages/has_more."),
		mcp.WithInputSchema[listContactsArgs](),
	)
	listContacts.Meta = uiToolMeta(appContacts) // surfaces list_contacts as an MCP App (contacts table template)
	s.AddTool(listContacts, mcp.NewTypedToolHandler(h.listContacts))

	s.AddTool(mcp.NewTool(
		"get_contact",
		mcp.WithDescription("Fetch a single contact by id."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.getContact))

	s.AddTool(mcp.NewTool(
		"update_contact",
		mcp.WithDescription("Update a contact's editable fields."),
		mcp.WithInputSchema[updateContactArgs](),
	), mcp.NewTypedToolHandler(h.updateContact))

	s.AddTool(mcp.NewTool(
		"delete_contact",
		mcp.WithDescription("Delete a contact and cascade-delete all of its deals."),
		mcp.WithInputSchema[idArg](),
	), mcp.NewTypedToolHandler(h.deleteContact))
}

func (h *handlers) createContact(_ context.Context, _ mcp.CallToolRequest, a createContactArgs) (*mcp.CallToolResult, error) {
	c, err := h.store.CreateContact(models.Contact{
		Name: a.Name, CompanyID: a.CompanyID, Email: a.Email, Phone: a.Phone, Tags: a.Tags, Notes: a.Notes,
	})
	if err != nil {
		errs := contactFieldErrors(err)
		res := formErrorResult(errs, err.Error())
		embedWidget(res, contactForm("create_contact", createContactValues(a), errs))
		return res, nil
	}
	res := okResult(c, fmt.Sprintf("Contact #%d created.", c.ID))
	embedWidget(res, contactForm("update_contact", contactValues(c), nil))
	return res, nil
}

func (h *handlers) listContacts(_ context.Context, _ mcp.CallToolRequest, a listContactsArgs) (*mcp.CallToolResult, error) {
	page, err := h.store.QueryContacts(db.ContactQuery{
		Email:    a.Email,
		Tag:      a.Tag,
		Search:   a.Query,
		SortBy:   db.ContactSort(a.SortBy),
		Asc:      strings.EqualFold(strings.TrimSpace(a.Order), "asc"),
		Page:     a.Page,
		PageSize: a.PageSize,
	})
	if err != nil {
		return toolErr(err)
	}
	items := toContactListItems(page.Contacts)
	res := pageResult(contactsRowsKey, listStatus(page.Total, "contact", "contacts"), items,
		page.Page, page.PageSize, page.Total, page.TotalPages, page.HasMore)
	embedCardList(res, func(rows []map[string]any) *gadget.CardList { return contactsCardList("Contacts", rows) }, items)
	return res, nil
}

func (h *handlers) getContact(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	c, err := h.store.GetContact(a.ID)
	if err != nil {
		return toolErr(err)
	}
	res := okResult(c, fmt.Sprintf("Contact #%d.", c.ID))
	embedCard(res, func(rows []map[string]any) *gadget.Card {
		return contactCard(fmt.Sprintf("Contact #%d", c.ID), rows)
	}, []contactListItem{toContactListItem(c)})
	return res, nil
}

func (h *handlers) updateContact(_ context.Context, _ mcp.CallToolRequest, a updateContactArgs) (*mcp.CallToolResult, error) {
	c, err := h.store.GetContact(a.ID)
	if err != nil {
		return toolErr(err)
	}
	setIf(&c.Name, a.Name)
	setIf(&c.CompanyID, a.CompanyID)
	setIf(&c.Email, a.Email)
	setIf(&c.Phone, a.Phone)
	setIf(&c.Notes, a.Notes)
	if a.Tags != nil {
		c.Tags = a.Tags
	}
	updated, err := h.store.UpdateContact(c)
	if err != nil {
		errs := contactFieldErrors(err)
		res := formErrorResult(errs, err.Error())
		embedWidget(res, contactForm("update_contact", contactValues(c), errs))
		return res, nil
	}
	res := okResult(updated, fmt.Sprintf("Contact #%d updated.", updated.ID))
	embedWidget(res, contactForm("update_contact", contactValues(updated), nil))
	return res, nil
}

func (h *handlers) deleteContact(_ context.Context, _ mcp.CallToolRequest, a idArg) (*mcp.CallToolResult, error) {
	deletedDeals, err := h.store.DeleteContact(a.ID)
	if err != nil {
		return toolErr(err)
	}
	items := h.latestContacts()
	res := okResult(map[string]any{
		"deleted": a.ID, "deleted_deal_ids": deletedDeals, contactsRowsKey: items,
	}, fmt.Sprintf("Contact #%d deleted.", a.ID))
	embedCardList(res, func(rows []map[string]any) *gadget.CardList { return contactsCardList("Contacts", rows) }, items)
	return res, nil
}
