package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/provenance"
)

// orun pr — the provenance pen: the branch names the task, the body
// carries the machine-readable manifest, commits carry the Orun-Task
// trailer, and the cloud's orun/compliance check verifies what the pen
// wrote. `orun pr check` is the local preflight of the same rules —
// prevention over detection.
//
// The pen is repo-local by construction: it reads git and writes GitHub.
// The work plane it was born beside is gone (orun-work-teardown WT2), and
// with it the two gestures that spoke to it — `--quick`, which minted a
// triage task inline, and `pr link`, which commented a URL onto a task's
// coordination log. Everything the compliance check verifies is here.

func registerPrCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "pr",
		Short: "The provenance pen: open and preflight task-carrying PRs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPrOpenCommand())
	cmd.AddCommand(newPrCheckCommand())
	root.AddCommand(cmd)

	hooks := &cobra.Command{
		Use:   "githooks",
		Short: "Repository hooks that keep provenance effortless",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	hooks.AddCommand(newGithooksInstallCommand())
	root.AddCommand(hooks)
}

// loadSessionSkillPins reads the pins `orun agent run`/`serve` recorded
// (IS6a) — the manifest names exactly what the session ran under.
func loadSessionSkillPins() []provenance.SkillPin {
	raw, err := os.ReadFile(filepath.Join(".orun", "agent-mcp", "skills.json"))
	if err != nil {
		return nil
	}
	var rec struct {
		Skills []provenance.SkillPin `json:"skills"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return nil
	}
	return rec.Skills
}

func newPrOpenCommand() *cobra.Command {
	var (
		asJSON  bool
		task    string
		title   string
		base    string
		draft   bool
		session string
	)
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the task's PR: grammar branch, push, manifest in the body",
		Long: `Open a PR that carries its lineage: the branch renamed onto the grammar
(orun/<task-key>-<slug>) when needed, pushed, and the machine-readable
manifest block written into the body — the task, the skill revisions the
session ran under, and the session id.

With a GitHub credential ambient (GITHUB_TOKEN / GH_TOKEN / gh auth) the
PR opens via the API; without one the pen still prepares everything and
prints the compare URL plus the body to paste — honest either way.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if task == "" {
				return fmt.Errorf("orun pr open: --task is required — a PR opens FOR a task")
			}
			manifest := provenance.Manifest{Version: provenance.ManifestVersion,
				Skills: loadSessionSkillPins(), Session: session}
			pen := &provenance.Pen{Workdir: ".", Token: cliauth.GitHubTokenFromEnv}
			out, err := pen.Open(ctx, provenance.OpenRequest{
				TaskKey: task, Title: title, Base: base, Draft: draft, Manifest: manifest,
			})
			if err != nil {
				if out != nil && out.CompareURL != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "branch %s pushed; open manually: %s\n", out.Branch, out.CompareURL)
				}
				return fmt.Errorf("orun pr open: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, out)
			}
			if out.Opened {
				fmt.Fprintf(cmd.OutOrStdout(), "opened %s (branch %s)\n", out.URL, out.Branch)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "branch %s pushed — no GitHub credential ambient.\nopen it here: %s\n\nbody to paste (the manifest is the lineage):\n\n%s", out.Branch, out.CompareURL, out.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&task, "task", "", "the task this PR closes (one task, one PR)")
	cmd.Flags().StringVar(&title, "title", "", "PR title (default: the task key)")
	cmd.Flags().StringVar(&base, "base", "main", "base branch")
	cmd.Flags().BoolVar(&draft, "draft", false, "open as a draft")
	cmd.Flags().StringVar(&session, "session", os.Getenv("ORUN_SESSION"), "session id for the manifest")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func newPrCheckCommand() *cobra.Command {
	var (
		base   string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "check [task-key]",
		Short: "Local preflight of the provenance rules (exit 1 on errors)",
		Long: `Run the same rules the cloud's orun/compliance check verifies (IS7 pins
the two engines byte-identical on shared fixtures): the branch grammar,
the Orun-Task trailer on every commit ahead of the base, one task per PR.
Fix the lineage before the PR exists — prevention over detection.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch, err := gitOut(cmd.Context(), "rev-parse", "--abbrev-ref", "HEAD")
			if err != nil {
				return fmt.Errorf("orun pr check: %w", err)
			}
			messages, err := commitsAhead(cmd.Context(), base)
			if err != nil {
				return fmt.Errorf("orun pr check: %w", err)
			}
			in := provenance.CheckInput{Branch: branch, CommitMessages: messages,
				HasSkillPins: len(loadSessionSkillPins()) > 0}
			if len(args) == 1 {
				in.TaskKey = args[0]
			}
			findings := provenance.Verify(in)
			if asJSON {
				if err := encodeJSON(cmd, map[string]any{"branch": branch, "findings": findings}); err != nil {
					return err
				}
			} else if len(findings) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "clean: %s carries its lineage (%d commit(s) checked)\n", branch, len(messages))
			} else {
				for _, f := range findings {
					fmt.Fprintf(cmd.OutOrStdout(), "%-5s %-16s %s\n", f.Level, f.Rule, f.Text)
				}
			}
			if provenance.HasErrors(findings) {
				return fmt.Errorf("orun pr check: the lineage is wrong — fix it before opening")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&base, "base", "main", "base branch to diff against")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

// githooksMarker identifies our hook so install never clobbers a foreign one.
const githooksMarker = "# orun githooks (orun-initiatives-v2 IS6)"

const commitMsgHook = `#!/bin/sh
` + githooksMarker + ` — stamp the Orun-Task trailer from the branch grammar.
branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null) || exit 0
case "$branch" in orun/*) ;; *) exit 0 ;; esac
key=$(printf '%s' "$branch" | sed -n 's|^orun/\([A-Z][A-Z0-9]*-[A-Z]\{0,1\}[0-9][0-9]*\).*|\1|p')
[ -n "$key" ] || exit 0
grep -q "^Orun-Task:" "$1" || printf '\nOrun-Task: %s\n' "$key" >> "$1"
`

func newGithooksInstallCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the commit-msg hook that stamps the Orun-Task trailer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			gitDir, err := gitOut(cmd.Context(), "rev-parse", "--git-dir")
			if err != nil {
				return fmt.Errorf("orun githooks install: %w", err)
			}
			path := filepath.Join(gitDir, "hooks", "commit-msg")
			if existing, err := os.ReadFile(path); err == nil && !strings.Contains(string(existing), githooksMarker) && !force {
				return fmt.Errorf("orun githooks install: %s exists and is not ours — pass --force to replace it", path)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(commitMsgHook), 0o755); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s — commits on orun/* branches gain the Orun-Task trailer\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace a foreign commit-msg hook")
	return cmd
}

// gitOut runs git in the cwd and returns trimmed stdout.
func gitOut(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// commitsAhead lists full commit messages ahead of the base (origin/<base>
// when it exists, <base> otherwise) — the preflight's trailer scope.
func commitsAhead(ctx context.Context, base string) ([]string, error) {
	ref := "origin/" + base
	if _, err := gitOut(ctx, "rev-parse", "--verify", ref); err != nil {
		ref = base
		if _, err := gitOut(ctx, "rev-parse", "--verify", ref); err != nil {
			return nil, fmt.Errorf("base %q not found (nor origin/%s)", base, base)
		}
	}
	raw, err := gitOut(ctx, "log", "--format=%B%x1e", ref+"..HEAD")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range strings.Split(raw, "\x1e") {
		if s := strings.TrimSpace(m); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
