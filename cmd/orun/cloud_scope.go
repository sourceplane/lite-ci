package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// The workspace-scoped cloud client and the three flags every command that
// speaks to a workspace carries. Carved out of the work-plane CLI group at
// the teardown (orun-work-teardown WT2): `orun skills` and `orun pr` were
// built on these helpers and outlive the group that happened to define
// them.

// cloudClient resolves scope + auth and builds the workspace-scoped cloud
// client — the same preamble as catalog push (flag > env > intent > cached
// link).
func cloudClient(ctx context.Context, backendURLFlag, orgFlag string) (*remotestate.Client, error) {
	backendURL, err := requireBackendURL(nil, backendURLFlag)
	if err != nil {
		return nil, err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return nil, err
	}
	linkOrg, linkProject := "", ""
	if repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	intentOrg, intentProject, _ := intentScope(loadIntentForCloudConfig())
	scope := resolveScope(orgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
	if scope.OrgID == "" {
		return nil, fmt.Errorf("no workspace resolved; pass --workspace or link the repo (orun auth login)")
	}
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  termIsInteractive(),
		RequireLogin: true,
		Org:          scope.OrgID,
	})
	if err != nil {
		if isNoLoginErr(err) {
			return nil, errNotLoggedIn()
		}
		return nil, fmt.Errorf("remote state auth: %w", err)
	}
	return remotestate.NewClientWithScope(backendURL, version, tokenSrc, scope), nil
}

// addCloudScopeFlags registers the three flags a workspace-scoped
// subcommand carries: --workspace, --backend-url, --json.
func addCloudScopeFlags(cmd *cobra.Command, workspace, backendURL *string, asJSON *bool) {
	cmd.Flags().StringVar(workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(asJSON, "json", false, "emit JSON")
}

// encodeJSON is the one JSON idiom these commands share: pretty-encoded
// response structs.
func encodeJSON(cmd *cobra.Command, v interface{}) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
