package remotestate

import (
	"context"
	"net/http"
)

// Epic docs client (orun-tasks D1 — the CLI push leg). Git owns the spec
// file; the cloud stores the pointer (repo + path + the commit the content
// was read at) beside the sealed snapshot, so console/MCP/tracker reads need
// no git credential. The push is idempotent by content hash on the server:
// re-pushing unchanged bytes is one read and no write, which is what lets CI
// run it on every merge with zero ceremony.

// EpicDocPushRequest mirrors PushEpicDocRequest.
type EpicDocPushRequest struct {
	Repo    string `json:"repo"`
	Path    string `json:"path"`
	Sha     string `json:"sha"`
	Content string `json:"content"`
	Title   string `json:"title,omitempty"`
}

// EpicDocSeal mirrors PushEpicDocResponse: the server's own recomputation of
// the content hash, and whether this push changed anything.
type EpicDocSeal struct {
	Updated     bool   `json:"updated"`
	ContentHash string `json:"contentHash"`
}

// EpicDoc mirrors PublicEpicDoc — the pointer and the seal, never content
// (the snapshot travels only on the single read).
type EpicDoc struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	GitSha      string `json:"gitSha"`
	ContentHash string `json:"contentHash"`
	PushedBy    string `json:"pushedBy"`
	PushedAt    string `json:"pushedAt"`
	SizeBytes   int    `json:"sizeBytes"`
}

// EpicDocsList mirrors ListEpicDocsResponse.
type EpicDocsList struct {
	Docs []EpicDoc `json:"docs"`
}

func epicDocsPathFor(org, epicRef, suffix string) string {
	return orgPath(org, "/tasks/epics/"+urlSegment(epicRef)+"/docs"+suffix)
}

// PushEpicDoc uploads one spec file's committed content with its pointer.
// Retried freely: the server is idempotent by content hash.
func (c *Client) PushEpicDoc(ctx context.Context, org, epicRef, slug string, req EpicDocPushRequest) (*EpicDocSeal, error) {
	var resp EpicDocSeal
	if err := c.doJSON(ctx, http.MethodPut, epicDocsPathFor(org, epicRef, "/"+urlSegment(slug)), req, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListEpicDocs fetches an epic's doc pointers (seals, no content).
func (c *Client) ListEpicDocs(ctx context.Context, org, epicRef string) (*EpicDocsList, error) {
	var resp EpicDocsList
	if err := c.doJSON(ctx, http.MethodGet, epicDocsPathFor(org, epicRef, ""), nil, &resp, true); err != nil {
		return nil, err
	}
	return &resp, nil
}
