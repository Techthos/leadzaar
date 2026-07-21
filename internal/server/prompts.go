package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/techthos/leadzaar/internal/models"
)

// registerPrompts exposes guided-workflow prompts.
func (h *handlers) registerPrompts(s *server.MCPServer) {
	s.AddPrompt(mcp.NewPrompt(
		"triage_new_leads",
		mcp.WithPromptDescription("Review leads still in 'new'/'contacted' and suggest the next status or action for each."),
	), h.triageNewLeads)

	s.AddPrompt(mcp.NewPrompt(
		"draft_followup",
		mcp.WithPromptDescription("Draft a follow-up message for a contact, optionally referencing one of their deals."),
		mcp.WithArgument("contact_id", mcp.RequiredArgument(), mcp.ArgumentDescription("Contact id to follow up with")),
		mcp.WithArgument("deal_id", mcp.ArgumentDescription("Optional deal id for context")),
	), h.draftFollowup)
}

func (h *handlers) triageNewLeads(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	newLeads, err := h.store.ListLeads("new")
	if err != nil {
		return nil, err
	}
	contacted, err := h.store.ListLeads("contacted")
	if err != nil {
		return nil, err
	}
	names, err := h.store.CompanyNames()
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("Triage these open leads. For each, recommend the next status (contacted/qualified/lost) and a concrete next action.\n")
	b.WriteString("A lead marked [away until DATE] has an out-of-office block: do not propose contacting them before that date — defer the action instead.\n\n")
	now := time.Now()
	writeLeadLines(&b, "NEW", newLeads, names, now)
	writeLeadLines(&b, "CONTACTED", contacted, names, now)
	if len(newLeads)+len(contacted) == 0 {
		b.WriteString("(No open leads to triage.)\n")
	}
	return mcp.NewGetPromptResult(
		"Triage open leads",
		[]mcp.PromptMessage{mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(b.String()))},
	), nil
}

func (h *handlers) draftFollowup(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	cidStr := req.Params.Arguments["contact_id"]
	cid, err := strconv.ParseUint(strings.TrimSpace(cidStr), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("contact_id %q is not a valid id: %w", cidStr, err)
	}
	contact, err := h.store.GetContact(cid)
	if err != nil {
		return nil, err
	}

	var companyName string
	if contact.CompanyID != 0 {
		if company, cerr := h.store.GetCompany(contact.CompanyID); cerr == nil {
			companyName = company.Name
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Draft a short, friendly follow-up message to %s", contact.Name)
	if companyName != "" {
		fmt.Fprintf(&b, " (%s)", companyName)
	}
	b.WriteString(".\n")
	if contact.Notes != "" {
		fmt.Fprintf(&b, "Context notes: %s\n", contact.Notes)
	}
	if dealStr := strings.TrimSpace(req.Params.Arguments["deal_id"]); dealStr != "" {
		did, err := strconv.ParseUint(dealStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("deal_id %q is not a valid id: %w", dealStr, err)
		}
		deal, err := h.store.GetDeal(did)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "Reference the deal %q (stage: %s, value: %.2f %s).\n",
			deal.Title, deal.Stage, deal.Value, deal.Currency)
	}
	return mcp.NewGetPromptResult(
		"Draft a follow-up",
		[]mcp.PromptMessage{mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(b.String()))},
	), nil
}

// writeLeadLines appends a labelled block of "#id name (company)" lines. The
// company name is resolved from names by the lead's CompanyID. A lead that is
// unavailable at now is tagged "[away until DATE]" so the model does not
// recommend contacting someone whose autoresponder says they are out.
func writeLeadLines(b *strings.Builder, label string, leads []models.Lead, names map[uint64]string, now time.Time) {
	if len(leads) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, l := range leads {
		fmt.Fprintf(b, "  #%d %s", l.ID, l.Name)
		if name := names[l.CompanyID]; name != "" {
			fmt.Fprintf(b, " (%s)", name)
		}
		if !l.Available(now) {
			fmt.Fprintf(b, " [away until %s]", models.FormatDate(l.UnavailableUntil))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}
