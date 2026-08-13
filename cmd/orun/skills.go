package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/remotestate"
)

// orun skills (orun-initiatives-v2 IS6, design §8) — the human/CI face of
// the hosted skill registry: list the merged default/org view, pull
// revisions as native skill files. Publishing stays with the console (and
// the cloud's PUT — work.approve); nothing here writes to the registry.

func registerSkillsCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Hosted agent playbooks: list the registry, pull native skill files",
		Long: `The skill registry (sealed, hosted policy): content-addressed revisions,
the Sourceplane defaults shadowed by anything your org publishes. Agents
consume these through skills_list/skill_get on the platform MCP and the
files 'orun agent run' materializes; this group is the same surface for
humans and CI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSkillsListCommand())
	cmd.AddCommand(newSkillsPullCommand())
	root.AddCommand(cmd)
}

func newSkillsListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Every skill's latest revision: name, rev, source",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			skills, err := client.ListSkills(cmd.Context(), client.Scope().OrgID)
			if err != nil {
				return fmt.Errorf("orun skills list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, skills)
			}
			rows := make([][]string, 0, len(skills.Skills))
			for _, s := range skills.Skills {
				by := "-"
				if s.PublishedBy != nil {
					by = s.PublishedBy.ID
				}
				rows = append(rows, []string{s.Name, shortRevision(s.Rev), s.Source, by})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"NAME", "REV", "SOURCE", "PUBLISHED BY"}, rows))
			return nil
		},
	}
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newSkillsPullCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		rev        string
		dir        string
	)
	cmd := &cobra.Command{
		Use:   "pull [name]",
		Short: "Materialize skills as native skill files (<dir>/<name>/SKILL.md)",
		Long: `Pull one skill (or, with no name, the whole registry) and write each
revision as a native skill file the harness discovers on its own. The
frontmatter carries the pinned orun-rev — a skill on disk always names
the revision it is.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if rev != "" && len(args) == 0 {
				return fmt.Errorf("orun skills pull: --rev needs a skill name")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			var views []remotestate.SkillView
			if len(args) == 1 {
				view, err := client.GetSkill(cmd.Context(), org, args[0], rev)
				if err != nil {
					return fmt.Errorf("orun skills pull: %w", err)
				}
				views = append(views, *view)
			} else {
				views, err = fetchAllSkills(cmd.Context(), client, org)
				if err != nil {
					return fmt.Errorf("orun skills pull: %w", err)
				}
			}
			pins, err := agent.MaterializeSkills(dir, views)
			if err != nil {
				return fmt.Errorf("orun skills pull: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"dir": dir, "skills": pins})
			}
			for _, p := range pins {
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (%s)\n", filepath.Join(dir, p.Name, "SKILL.md"), shortRevision(p.Rev))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rev, "rev", "", "exact revision sha256:<hex> (single-name pulls)")
	cmd.Flags().StringVar(&dir, "dir", filepath.Join(".claude", "skills"), "target directory for skill files")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// fetchAllSkills lists the registry then reads each latest body.
func fetchAllSkills(ctx context.Context, client *remotestate.Client, org string) ([]remotestate.SkillView, error) {
	list, err := client.ListSkills(ctx, org)
	if err != nil {
		return nil, err
	}
	views := make([]remotestate.SkillView, 0, len(list.Skills))
	for _, s := range list.Skills {
		view, err := client.GetSkill(ctx, org, s.Name, s.Rev)
		if err != nil {
			return nil, err
		}
		views = append(views, *view)
	}
	return views, nil
}

// materializeHarnessSkills is the agent-run/serve hook (IS6): best-effort —
// fetch the registry, write native skill files into the harness workdir,
// and record the pins beside the MCP config for the PR manifest. A cloud
// miss (not linked, not logged in, offline) is a WARNING, never a failed
// session: the skills sharpen a run; their absence doesn't brick it.
func materializeHarnessSkills(ctx context.Context, backendURL, workspace, workdir string, errOut io.Writer) {
	client, err := workClient(ctx, backendURL, workspace)
	if err != nil {
		fmt.Fprintf(errOut, "orun agent: skills not materialized (%v) — continuing without them\n", err)
		return
	}
	views, err := fetchAllSkills(ctx, client, client.Scope().OrgID)
	if err != nil {
		fmt.Fprintf(errOut, "orun agent: skills not materialized (%v) — continuing without them\n", err)
		return
	}
	skillsDir := filepath.Join(workdir, ".claude", "skills")
	pins, err := agent.MaterializeSkills(skillsDir, views)
	if err != nil {
		fmt.Fprintf(errOut, "orun agent: skills not materialized (%v) — continuing without them\n", err)
		return
	}
	if err := agent.WriteSkillPins(filepath.Join(".orun", "agent-mcp", "skills.json"), pins); err != nil {
		fmt.Fprintf(errOut, "orun agent: skill pins not recorded (%v)\n", err)
	}
	fmt.Fprintf(errOut, "orun agent: %d skill(s) materialized into %s (pins recorded for the manifest)\n", len(pins), skillsDir)
}
