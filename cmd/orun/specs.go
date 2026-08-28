package main

// orun spec (orun-tasks D1 — the CLI push leg). Spec docs are markdown files
// in the repo, annotated to EPICS the way component docs are annotated to
// components. Git owns the file; 'spec push' uploads the COMMITTED copy with
// its pointer — repo, path, and the HEAD commit the bytes were read at — so
// the sha banner every surface renders can never describe bytes that were
// never committed. The server is idempotent by content hash: pushing
// unchanged files is free, which is what lets CI run this on every merge.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

func registerSpecCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Spec docs: repo-authored markdown, annotated to epics, pushed with its commit",
		Long: `A spec doc lives in the repository and is annotated to an epic. 'push'
uploads the committed copy (HEAD) with its pointer — repo, path, sha — and
the cloud stores it sealed beside the epic, where the console, MCP and the
tracker sync read it without a git credential. Pushes are idempotent by
content hash; CI can push on every merge with zero ceremony.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSpecPushCommand())
	cmd.AddCommand(newSpecListCommand())
	root.AddCommand(cmd)
}

var specSlugStrip = regexp.MustCompile(`[^a-z0-9-]+`)

// specSlugFor derives the doc's slug from the filename: basename, extension
// stripped, lowercased, everything outside [a-z0-9-] collapsed to a dash.
// Refused (empty) rather than invented when nothing survives.
func specSlugFor(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	slug := specSlugStrip.ReplaceAllString(strings.ToLower(base), "-")
	slug = strings.Trim(slug, "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return slug
}

// specTitleFor reads the first H1 heading — the doc names itself, or not.
func specTitleFor(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "# "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// specRepoIdentity derives the display identity (owner/name) from the origin
// remote. A remote that does not parse degrades to the repo directory's
// basename — display data, never a credential, so a best guess is honest as
// long as it is stable.
func specRepoIdentity(repoRoot string) string {
	origin, err := gitOutIn(repoRoot, "remote", "get-url", "origin")
	if err == nil {
		url := strings.TrimSuffix(strings.TrimSpace(origin), ".git")
		// https://host/owner/name or git@host:owner/name
		if _, after, ok := strings.Cut(url, "://"); ok {
			parts := strings.Split(after, "/")
			if len(parts) >= 3 {
				return strings.Join(parts[len(parts)-2:], "/")
			}
		} else if _, after, ok := strings.Cut(url, ":"); ok {
			parts := strings.Split(after, "/")
			if len(parts) >= 2 {
				return strings.Join(parts[len(parts)-2:], "/")
			}
		}
	}
	return filepath.Base(repoRoot)
}

// gitOutIn runs git in a specific directory and returns trimmed stdout —
// the sibling of pr.go's cwd-bound gitOut, for commands anchored to the
// spec file's repo rather than wherever the CLI happens to run.
func gitOutIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// specFileAtHead resolves one file to its COMMITTED content and pointer.
// The honest rule: sha and bytes always agree. Working-tree edits are not
// pushed — the caller is told to commit first, never silently served a sha
// that describes different bytes.
type specFile struct {
	RelPath string
	Slug    string
	Title   string
	Content string
	Sha     string
	Dirty   bool
}

func resolveSpecFile(path string) (*specFile, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, "", fmt.Errorf("orun spec push: %s: %w", path, err)
	}
	dir := filepath.Dir(abs)
	repoRoot, err := gitOutIn(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, "", fmt.Errorf("orun spec push: %s is not inside a git repository", path)
	}
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return nil, "", err
	}
	rel = filepath.ToSlash(rel)

	sha, err := gitOutIn(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, "", fmt.Errorf("orun spec push: %s has no commits yet — commit the spec first", repoRoot)
	}
	content, err := gitOutIn(repoRoot, "show", "HEAD:"+rel)
	if err != nil {
		return nil, "", fmt.Errorf("orun spec push: %s is not committed at HEAD — commit it first (the sha must describe the bytes)", rel)
	}

	slug := specSlugFor(rel)
	if slug == "" {
		return nil, "", fmt.Errorf("orun spec push: %s yields no slug — rename the file", rel)
	}

	dirty := false
	if status, serr := gitOutIn(repoRoot, "status", "--porcelain", "--", rel); serr == nil && status != "" {
		dirty = true
	}

	return &specFile{
		RelPath: rel,
		Slug:    slug,
		Title:   specTitleFor(content),
		Content: content,
		Sha:     sha,
		Dirty:   dirty,
	}, repoRoot, nil
}

func newSpecPushCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		epicRef    string
		repoName   string
	)
	cmd := &cobra.Command{
		Use:   "push <file>...",
		Short: "Push committed spec files to an epic — idempotent by content hash",
		Long: `Push one or more markdown files to an epic. Each file is read AS COMMITTED
at HEAD (working-tree edits are refused implicitly: the sha must describe
the bytes), the slug derives from the filename, the title from the first
heading. Unchanged content is a no-op on the server — safe on every merge.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if epicRef == "" {
				return fmt.Errorf("orun spec push: --epic is required (epc_… or the epic's slug)")
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			out := cmd.OutOrStdout()

			type pushed struct {
				Slug        string `json:"slug"`
				Path        string `json:"path"`
				Sha         string `json:"sha"`
				Updated     bool   `json:"updated"`
				ContentHash string `json:"contentHash"`
			}
			results := make([]pushed, 0, len(args))
			for _, path := range args {
				file, repoRoot, err := resolveSpecFile(path)
				if err != nil {
					return err
				}
				identity := repoName
				if identity == "" {
					identity = specRepoIdentity(repoRoot)
				}
				req := remotestate.EpicDocPushRequest{
					Repo:    identity,
					Path:    file.RelPath,
					Sha:     file.Sha,
					Content: file.Content,
					Title:   file.Title,
				}
				seal, err := client.PushEpicDoc(cmd.Context(), org, epicRef, file.Slug, req)
				if err != nil {
					return fmt.Errorf("orun spec push: %s: %w", file.RelPath, err)
				}
				results = append(results, pushed{
					Slug: file.Slug, Path: file.RelPath, Sha: file.Sha,
					Updated: seal.Updated, ContentHash: seal.ContentHash,
				})
				if !asJSON {
					verb := "unchanged"
					if seal.Updated {
						verb = "pushed"
					}
					fmt.Fprintf(out, "%s %s → %s (%s as of %s)\n", verb, file.Slug, epicRef, shortRevision(seal.ContentHash), shortRevision(file.Sha))
					if file.Dirty {
						fmt.Fprintf(out, "note: %s has working-tree changes — the COMMITTED copy was pushed; commit and push again to update\n", file.RelPath)
					}
				}
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"epic": epicRef, "docs": results})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&epicRef, "epic", "", "the epic to annotate (epc_… or its slug)")
	cmd.Flags().StringVar(&repoName, "repo", "", "repo display identity (default: derived from the origin remote)")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newSpecListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		epicRef    string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "An epic's spec docs: slug, title, pointer, seal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if epicRef == "" {
				return fmt.Errorf("orun spec list: --epic is required (epc_… or the epic's slug)")
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			list, err := client.ListEpicDocs(cmd.Context(), client.Scope().OrgID, epicRef)
			if err != nil {
				return fmt.Errorf("orun spec list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, list)
			}
			rows := make([][]string, 0, len(list.Docs))
			for _, d := range list.Docs {
				rows = append(rows, []string{
					d.Slug, orDash(d.Title), d.Repo + "/" + d.Path, shortRevision(d.GitSha), orDash(shortRevision(d.ContentHash)),
				})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"SLUG", "TITLE", "FILE", "AS OF", "SEAL"}, rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&epicRef, "epic", "", "the epic to list (epc_… or its slug)")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}
