package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const mimeJSON = "application/json"

// registerResources exposes read-only views of records and the pipeline summary
// by URI. Parameterized records use resource templates; the pipeline is a static
// resource.
func (h *handlers) registerResources(s *server.MCPServer) {
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("crm://leads/{id}", "Lead",
			mcp.WithTemplateDescription("A single lead as JSON"),
			mcp.WithTemplateMIMEType(mimeJSON)),
		h.readLead,
	)

	s.AddResourceTemplate(
		mcp.NewResourceTemplate("crm://contacts/{id}", "Contact",
			mcp.WithTemplateDescription("A single contact as JSON"),
			mcp.WithTemplateMIMEType(mimeJSON)),
		h.readContact,
	)

	s.AddResourceTemplate(
		mcp.NewResourceTemplate("crm://deals/{id}", "Deal",
			mcp.WithTemplateDescription("A single deal as JSON"),
			mcp.WithTemplateMIMEType(mimeJSON)),
		h.readDeal,
	)

	s.AddResourceTemplate(
		mcp.NewResourceTemplate("crm://companies/{id}", "Company",
			mcp.WithTemplateDescription("A single company as JSON"),
			mcp.WithTemplateMIMEType(mimeJSON)),
		h.readCompany,
	)

	s.AddResource(
		mcp.NewResource("crm://pipeline", "Pipeline summary",
			mcp.WithResourceDescription("Funnel + pipeline aggregate as JSON"),
			mcp.WithMIMEType(mimeJSON)),
		h.readPipeline,
	)
}

// jsonResource marshals v and returns it as a single text resource at uri.
func jsonResource(uri string, v any) ([]mcp.ResourceContents, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal resource %q: %w", uri, err)
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{URI: uri, MIMEType: mimeJSON, Text: string(b)},
	}, nil
}

func (h *handlers) readLead(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id, err := parseIDFromURI(req.Params.URI, "crm://leads/")
	if err != nil {
		return nil, err
	}
	lead, err := h.store.GetLead(id)
	if err != nil {
		return nil, err
	}
	return jsonResource(req.Params.URI, lead)
}

func (h *handlers) readContact(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id, err := parseIDFromURI(req.Params.URI, "crm://contacts/")
	if err != nil {
		return nil, err
	}
	c, err := h.store.GetContact(id)
	if err != nil {
		return nil, err
	}
	return jsonResource(req.Params.URI, c)
}

func (h *handlers) readDeal(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id, err := parseIDFromURI(req.Params.URI, "crm://deals/")
	if err != nil {
		return nil, err
	}
	d, err := h.store.GetDeal(id)
	if err != nil {
		return nil, err
	}
	return jsonResource(req.Params.URI, d)
}

func (h *handlers) readCompany(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	id, err := parseIDFromURI(req.Params.URI, "crm://companies/")
	if err != nil {
		return nil, err
	}
	c, err := h.store.GetCompany(id)
	if err != nil {
		return nil, err
	}
	return jsonResource(req.Params.URI, c)
}

func (h *handlers) readPipeline(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	summary, err := h.store.PipelineSummary()
	if err != nil {
		return nil, err
	}
	return jsonResource(req.Params.URI, summary)
}
