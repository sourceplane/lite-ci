package main

import (
	"context"
	"net/url"
	"strings"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/runner"
)

// attachTfBackend wires the terraform HTTP state-backend env provider onto the
// runner (SB1, de-AWS): every job step gets TF_HTTP_* addressing that job's
// (component, environment) state on the platform state plane, with the run
// token as the backend password (the edge normalizes basic → bearer on tfstate
// paths). Terraform's `backend "http"` reads the env directly, so compositions
// carry no -backend-config plumbing and no AWS credentials step.
//
// Best-effort to wire: without a resolved (org, project) scope there is no
// address to build — terraform init then fails loudly on the empty backend
// config rather than silently targeting the wrong tenant.
func attachTfBackend(r *runner.Runner, backendURL string, tokenSrc remotestate.TokenSource, org, project string) {
	org = strings.TrimSpace(org)
	project = strings.TrimSpace(project)
	if org == "" || project == "" {
		return
	}
	base := strings.TrimRight(backendURL, "/") +
		"/v1/organizations/" + url.PathEscape(org) +
		"/projects/" + url.PathEscape(project) +
		"/state/tfstate/"
	r.TfBackendEnv = func(ctx context.Context, component, environment string) map[string]string {
		token, err := tokenSrc.Token(ctx)
		if err != nil || strings.TrimSpace(token) == "" {
			// No token, no backend env: terraform fails loudly at init instead
			// of running against a half-configured backend.
			return nil
		}
		if r.Redactor != nil {
			// The backend password is the run token — keep it out of any step
			// output that echoes the environment.
			r.Redactor.Add(token)
		}
		addr := base + url.PathEscape(component) + "/" + url.PathEscape(environment)
		return map[string]string{
			"TF_HTTP_ADDRESS":        addr,
			"TF_HTTP_LOCK_ADDRESS":   addr,
			"TF_HTTP_UNLOCK_ADDRESS": addr,
			"TF_HTTP_LOCK_METHOD":    "LOCK",
			"TF_HTTP_UNLOCK_METHOD":  "UNLOCK",
			"TF_HTTP_USERNAME":       "orun",
			"TF_HTTP_PASSWORD":       token,
			"TF_HTTP_RETRY_MAX":      "4",
		}
	}
}
