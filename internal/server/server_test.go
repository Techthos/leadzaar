package server_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/techthos/microapp-crm/internal/db"
	"github.com/techthos/microapp-crm/internal/models"
	"github.com/techthos/microapp-crm/internal/server"
)

// itoa formats a record id for use in a resource URI.
func itoa(id uint64) string { return strconv.FormatUint(id, 10) }

// setup builds a store-backed in-process MCP client, initialized and ready.
func setup(t *testing.T) (*client.Client, context.Context) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "crm.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	c, err := client.NewInProcessClient(server.New(store, "test"))
	if err != nil {
		t.Fatalf("NewInProcessClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := t.Context()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return c, ctx
}

// callTool invokes a tool and returns the result; it fails on transport errors.
func callTool(t *testing.T, c *client.Client, ctx context.Context, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("CallTool(%s): transport error %v", name, err)
	}
	return res
}

// resultText extracts the text payload of a tool result.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("first content is not text: %T", res.Content[0])
	}
	return tc.Text
}

func TestToolSurfaceRegistered(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)
	res, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if got := len(res.Tools); got != 27 {
		t.Errorf("registered tools = %d, want 27", got)
	}
}

func TestLeadToolRoundTrip(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	res := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Jane", "source": "web"})
	if res.IsError {
		t.Fatalf("create_lead errored: %s", resultText(t, res))
	}
	var lead models.Lead
	if err := json.Unmarshal([]byte(resultText(t, res)), &lead); err != nil {
		t.Fatalf("unmarshal lead: %v", err)
	}
	if lead.ID == 0 || lead.Status != models.StatusNew {
		t.Errorf("unexpected lead: %+v", lead)
	}

	got := callTool(t, c, ctx, "get_lead", map[string]any{"id": lead.ID})
	if got.IsError {
		t.Fatalf("get_lead errored: %s", resultText(t, got))
	}
}

func TestInvalidInputSurfacesAsToolError(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	res := callTool(t, c, ctx, "create_lead", map[string]any{"name": "X", "source": "linkedin"})
	if !res.IsError {
		t.Errorf("expected tool error for invalid source, got success: %s", resultText(t, res))
	}

	missing := callTool(t, c, ctx, "get_contact", map[string]any{"id": 99999})
	if !missing.IsError {
		t.Errorf("expected tool error for unknown contact, got: %s", resultText(t, missing))
	}
}

func TestConvertAndPipelineThroughTools(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	res := callTool(t, c, ctx, "create_lead", map[string]any{"name": "Jane", "email": "jane@example.com"})
	var lead models.Lead
	_ = json.Unmarshal([]byte(resultText(t, res)), &lead)

	conv := callTool(t, c, ctx, "convert_lead", map[string]any{
		"id": lead.ID, "make_deal": true, "deal_title": "First", "deal_value": 1000.0, "deal_currency": "EUR",
	})
	if conv.IsError {
		t.Fatalf("convert_lead errored: %s", resultText(t, conv))
	}
	var convRes db.ConvertResult
	if err := json.Unmarshal([]byte(resultText(t, conv)), &convRes); err != nil {
		t.Fatalf("unmarshal convert result: %v", err)
	}
	if convRes.Deal == nil || convRes.Contact.ID == 0 {
		t.Fatalf("convert did not produce contact+deal: %+v", convRes)
	}

	summaryRes := callTool(t, c, ctx, "pipeline_summary", map[string]any{})
	var summary models.PipelineSummary
	if err := json.Unmarshal([]byte(resultText(t, summaryRes)), &summary); err != nil {
		t.Fatalf("unmarshal summary: %v", err)
	}
	var qualEUR float64
	for _, ss := range summary.DealsByStage {
		if ss.Stage == models.StageQualification {
			for _, ct := range ss.Totals {
				if ct.Currency == "EUR" {
					qualEUR = ct.Total
				}
			}
		}
	}
	if qualEUR != 1000 {
		t.Errorf("qualification EUR total = %v, want 1000", qualEUR)
	}
}

func TestDeleteContactCascadeThroughTools(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	cres := callTool(t, c, ctx, "create_contact", map[string]any{"name": "Acme"})
	var contact models.Contact
	_ = json.Unmarshal([]byte(resultText(t, cres)), &contact)

	_ = callTool(t, c, ctx, "create_deal", map[string]any{
		"title": "D", "contact_id": contact.ID, "stage": "proposal",
	})

	del := callTool(t, c, ctx, "delete_contact", map[string]any{"id": contact.ID})
	if del.IsError {
		t.Fatalf("delete_contact errored: %s", resultText(t, del))
	}
	var payload struct {
		DeletedDealIDs []uint64 `json:"deleted_deal_ids"`
	}
	if err := json.Unmarshal([]byte(resultText(t, del)), &payload); err != nil {
		t.Fatalf("unmarshal delete payload: %v", err)
	}
	if len(payload.DeletedDealIDs) != 1 {
		t.Errorf("deleted_deal_ids = %v, want 1", payload.DeletedDealIDs)
	}
}

func TestResources(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	cres := callTool(t, c, ctx, "create_contact", map[string]any{"name": "Ada"})
	var contact models.Contact
	_ = json.Unmarshal([]byte(resultText(t, cres)), &contact)

	t.Run("read existing contact resource", func(t *testing.T) {
		req := mcp.ReadResourceRequest{}
		req.Params.URI = "crm://contacts/" + itoa(contact.ID)
		out, err := c.ReadResource(ctx, req)
		if err != nil {
			t.Fatalf("ReadResource: %v", err)
		}
		trc, ok := out.Contents[0].(mcp.TextResourceContents)
		if !ok {
			t.Fatalf("content is not text resource: %T", out.Contents[0])
		}
		var got models.Contact
		if err := json.Unmarshal([]byte(trc.Text), &got); err != nil {
			t.Fatalf("unmarshal resource: %v", err)
		}
		if got.Name != "Ada" {
			t.Errorf("resource Name = %q, want Ada", got.Name)
		}
	})

	t.Run("read pipeline resource", func(t *testing.T) {
		req := mcp.ReadResourceRequest{}
		req.Params.URI = "crm://pipeline"
		if _, err := c.ReadResource(ctx, req); err != nil {
			t.Fatalf("ReadResource(pipeline): %v", err)
		}
	})

	t.Run("unknown id errors", func(t *testing.T) {
		req := mcp.ReadResourceRequest{}
		req.Params.URI = "crm://contacts/99999"
		if _, err := c.ReadResource(ctx, req); err == nil {
			t.Error("expected error for unknown contact resource, got nil")
		}
	})
}

func TestPrompts(t *testing.T) {
	t.Parallel()
	c, ctx := setup(t)

	comp := callTool(t, c, ctx, "create_company", map[string]any{"name": "Analytical"})
	var company models.Company
	_ = json.Unmarshal([]byte(resultText(t, comp)), &company)
	cres := callTool(t, c, ctx, "create_contact", map[string]any{"name": "Ada", "company_id": company.ID})
	var contact models.Contact
	_ = json.Unmarshal([]byte(resultText(t, cres)), &contact)
	_ = callTool(t, c, ctx, "create_lead", map[string]any{"name": "Fresh"})

	t.Run("triage_new_leads", func(t *testing.T) {
		req := mcp.GetPromptRequest{}
		req.Params.Name = "triage_new_leads"
		res, err := c.GetPrompt(ctx, req)
		if err != nil {
			t.Fatalf("GetPrompt: %v", err)
		}
		if len(res.Messages) == 0 {
			t.Fatal("expected at least one prompt message")
		}
	})

	t.Run("draft_followup with contact", func(t *testing.T) {
		req := mcp.GetPromptRequest{}
		req.Params.Name = "draft_followup"
		req.Params.Arguments = map[string]string{"contact_id": itoa(contact.ID)}
		res, err := c.GetPrompt(ctx, req)
		if err != nil {
			t.Fatalf("GetPrompt: %v", err)
		}
		if len(res.Messages) == 0 {
			t.Fatal("expected a drafted message")
		}
	})

	t.Run("draft_followup unknown contact errors", func(t *testing.T) {
		req := mcp.GetPromptRequest{}
		req.Params.Name = "draft_followup"
		req.Params.Arguments = map[string]string{"contact_id": "99999"}
		if _, err := c.GetPrompt(ctx, req); err == nil {
			t.Error("expected error for unknown contact, got nil")
		}
	})
}
