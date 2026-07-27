package configsurface

// Integration-connection + scope-template reads/writes for the CLI's
// integrations tree (saas-secrets-platform SP5). Everything here is safe
// metadata — a connection row or template descriptor, never a credential.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Connection is one integration connection (contracts PublicConnection, the
// subset the CLI renders). Nullable wire fields are pointers so a provider
// that never set them decodes cleanly.
type Connection struct {
	ID                   string  `json:"id"`
	Provider             string  `json:"provider"`
	Status               string  `json:"status"`
	Scope                string  `json:"scope,omitempty"`
	ShareMode            string  `json:"shareMode,omitempty"`
	DisplayName          *string `json:"displayName"`
	ExternalAccountLogin *string `json:"externalAccountLogin"`
	ExternalAccountType  *string `json:"externalAccountType"`
	ConnectedAt          *string `json:"connectedAt"`
	// Inherited marks an account-shared connection resolved from the parent
	// account rather than owned by this workspace.
	Inherited bool `json:"inherited,omitempty"`
}

// AccountLabel is the best human label for the connected provider account.
func (c Connection) AccountLabel() string {
	if c.DisplayName != nil && strings.TrimSpace(*c.DisplayName) != "" {
		return *c.DisplayName
	}
	if c.ExternalAccountLogin != nil && strings.TrimSpace(*c.ExternalAccountLogin) != "" {
		return *c.ExternalAccountLogin
	}
	return "—"
}

// ListConnections calls GET /v1/organizations/{org}/integrations — every
// connection the workspace can consume (owned + inherited account-shared).
func (c *Client) ListConnections(ctx context.Context, org string) ([]Connection, error) {
	if strings.TrimSpace(org) == "" {
		return nil, fmt.Errorf("configsurface: connection listing needs an organization")
	}
	path := "/v1/organizations/" + urlSegment(org) + "/integrations"
	var payload struct {
		Connections []Connection `json:"connections"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload, true); err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	return payload.Connections, nil
}

// ListScopeTemplates calls the provider's manage view
// (GET …/providers/{provider}/scope-templates, SP4): the declared catalog
// plus every org-curated template, active AND retired.
func (c *Client) ListScopeTemplates(ctx context.Context, org, provider string) ([]ScopeTemplate, error) {
	if strings.TrimSpace(org) == "" || strings.TrimSpace(provider) == "" {
		return nil, fmt.Errorf("configsurface: template listing needs an organization and provider")
	}
	path := "/v1/organizations/" + urlSegment(org) + "/integrations/providers/" + urlSegment(provider) + "/scope-templates"
	var payload struct {
		Templates []ScopeTemplate `json:"templates"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload, true); err != nil {
		return nil, fmt.Errorf("list scope templates: %w", err)
	}
	return payload.Templates, nil
}

// CreateScopeTemplateRequest mirrors contracts CreateScopeTemplateRequest:
// a custom template derives from a declared base — the base supplies every
// mint semantic, so a custom can never exceed what its base grants.
type CreateScopeTemplateRequest struct {
	TemplateID   string `json:"templateId"`
	BaseTemplate string `json:"baseTemplate"`
	DisplayName  string `json:"displayName"`
	Description  string `json:"description,omitempty"`
}

// CreateScopeTemplate calls POST …/providers/{provider}/scope-templates.
func (c *Client) CreateScopeTemplate(ctx context.Context, org, provider string, req CreateScopeTemplateRequest) (*ScopeTemplate, error) {
	path := "/v1/organizations/" + urlSegment(org) + "/integrations/providers/" + urlSegment(provider) + "/scope-templates"
	var payload struct {
		Template ScopeTemplate `json:"template"`
	}
	if err := c.doJSON(ctx, http.MethodPost, path, req, &payload, false); err != nil {
		return nil, fmt.Errorf("create scope template: %w", err)
	}
	return &payload.Template, nil
}

// UpdateScopeTemplateRequest mirrors contracts UpdateScopeTemplateRequest.
// Status soft-retires/reactivates; there is no hard delete (SP-A6).
type UpdateScopeTemplateRequest struct {
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

// UpdateScopeTemplate calls PATCH …/scope-templates/{templateId}.
func (c *Client) UpdateScopeTemplate(ctx context.Context, org, provider, templateID string, req UpdateScopeTemplateRequest) (*ScopeTemplate, error) {
	path := "/v1/organizations/" + urlSegment(org) + "/integrations/providers/" + urlSegment(provider) + "/scope-templates/" + urlSegment(templateID)
	var payload struct {
		Template ScopeTemplate `json:"template"`
	}
	if err := c.doJSON(ctx, http.MethodPatch, path, req, &payload, false); err != nil {
		return nil, fmt.Errorf("update scope template: %w", err)
	}
	return &payload.Template, nil
}
