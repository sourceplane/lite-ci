package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/worklens"
)

// orun initiatives (orun-initiatives IN6) — the work plane's CLI group,
// replacing `orun work` (which survives one release as a hidden deprecated
// alias forwarding to the same run functions; see work.go). Lifecycle stays
// a derived query over the two logs: nothing in this group can set a status.

func registerInitiativesCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "initiatives",
		Short: "The work plane: portfolio, trees, tasks, activity, designs",
		Long: `The work plane (specs/orun-initiatives; substrate specs/orun-work).

Work lifecycle is a derived query over two append-only logs, never a stored
status. This group is the terminal face of the Initiatives surface: the
portfolio, one initiative's tree, task detail with evidence, the tagged
activity tail, and the design/import/doc plumbing.

Subcommands:
  list      Portfolio: key, title, status, progress, needs-you, target
  view      One initiative's tree as an indented ladder
  create    Create an initiative envelope (--title, --why …)
  edit      Edit an item's envelope (title/description/owner/target/…)
  cancel    Retire an item (task or epic) — the append-only "delete"
  import    Map a specs/ tree to the hierarchy and apply to Orun Cloud
  task      Task detail (view), creation (create), voice (done, note)
  activity  The tagged activity tail for any noun
  doc       Pull an epic spec / design doc as markdown
  design    List or propose design runs on an initiative

State and the loop (orun-initiatives-v2 — initiative status is a stored
speech act now; task rungs stay derived):
  start | pause | resume | complete | reopen   The five-state machine
  update / updates    Post / read the attributed health headlines
  context   The any-key context bundle (item, ancestry, activity, needs-you)
  assign    Assign a subject to any noun (task, design, epic, initiative)
  review    request | verdict — eyes and opinions on epics and designs
  adopt     Adopt a design (interactive confirm = the signature)
  now       The live board: in-flight tasks × latest worklog note × seat
  yours     Your addressed queue: everything that waits on you, one list

Run 'orun initiatives <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newInitiativesListCommand())
	cmd.AddCommand(newInitiativesViewCommand())
	cmd.AddCommand(newInitiativesCreateCommand())
	cmd.AddCommand(newItemEditCommand())
	cmd.AddCommand(newItemCancelCommand())
	cmd.AddCommand(newWorkImportCommand())
	cmd.AddCommand(newInitiativesTaskCommand())
	cmd.AddCommand(newInitiativesActivityCommand())
	cmd.AddCommand(newInitiativesDocCommand())
	cmd.AddCommand(newInitiativesDesignCommand())
	// orun-initiatives-v2 (IS4) — the state machine, the update cadence,
	// the context bundle, the generalized assign, review/adopt, the board.
	for _, sub := range newInitiativeStatusCommands() {
		cmd.AddCommand(sub)
	}
	cmd.AddCommand(newInitiativesUpdateCommand())
	cmd.AddCommand(newInitiativesUpdatesCommand())
	cmd.AddCommand(newInitiativesContextCommand())
	cmd.AddCommand(newInitiativesAssignCommand())
	cmd.AddCommand(newInitiativesReviewCommand())
	cmd.AddCommand(newInitiativesAdoptCommand())
	cmd.AddCommand(newInitiativesNowCommand())
	cmd.AddCommand(newInitiativesYoursCommand())
	root.AddCommand(cmd)
}

// workClient resolves scope + auth and builds the cloud client — the same
// preamble as catalog push (flag > env > intent > cached link).
func workClient(ctx context.Context, backendURLFlag, orgFlag string) (*remotestate.Client, error) {
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
		return nil, fmt.Errorf("orun work: no workspace resolved; pass --workspace or link the repo (orun auth login)")
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

// addWorkScopeFlags registers the three flags every subcommand of the group
// carries: --workspace, --backend-url, --json.
func addWorkScopeFlags(cmd *cobra.Command, workspace, backendURL *string, asJSON *bool) {
	cmd.Flags().StringVar(workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(asJSON, "json", false, "emit JSON")
}

// encodeJSON is the group's one JSON idiom: pretty-encoded response structs.
func encodeJSON(cmd *cobra.Command, v interface{}) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ── list ─────────────────────────────────────────────────────────────────────

func newInitiativesListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "The portfolio: every initiative with derived status, progress, needs-you",
		Long: `Fetch the portfolio fold from Orun Cloud: one row per initiative with its
derived status (planning until the first approved epic, the health fold
afterwards), two-segment progress, and the needs-you reasons that wait on a
human. Nothing shown here is a stored status.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			portfolio, err := client.ListInitiatives(cmd.Context())
			if err != nil {
				return fmt.Errorf("orun initiatives list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, portfolio)
			}
			fmt.Fprint(cmd.OutOrStdout(), renderPortfolio(portfolio))
			return nil
		},
	}
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// renderPortfolio renders the portfolio table plus the fold-stats footer.
func renderPortfolio(p *remotestate.WorkPortfolio) string {
	rows := make([][]string, 0, len(p.Initiatives))
	for _, ini := range p.Initiatives {
		needs := "-"
		if len(ini.NeedsYou) > 0 {
			needs = ini.NeedsYou[0].Text
			if extra := len(ini.NeedsYou) - 1; extra > 0 {
				needs += fmt.Sprintf(" (+%d)", extra)
			}
		}
		target := orDash(ini.TargetDate)
		rows = append(rows, []string{
			ini.Key,
			ini.Title,
			ini.Status,
			fmt.Sprintf("%d/%d", ini.Progress.Done, ini.Progress.Total),
			needs,
			target,
		})
	}
	out := renderColumns([]string{"KEY", "TITLE", "STATUS", "PROGRESS", "NEEDS YOU", "TARGET"}, rows)
	out += fmt.Sprintf("\nopen tasks: %d · needs you: %d", p.Stats.OpenTasks, p.Stats.NeedsYou)
	if p.Stats.AgentsLive > 0 {
		out += fmt.Sprintf(" · agents live: %d", p.Stats.AgentsLive)
	}
	return out + "\n"
}

// ── view ─────────────────────────────────────────────────────────────────────

func newInitiativesViewCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "view <key>",
		Short: "One initiative's tree: epics, milestones, tasks with evidence",
		Long: `Fetch one initiative's full hierarchy and render it as an indented
ladder: the initiative header (status, owner, target, progress), each epic
with its intent state and approved revision (drift named when present), each
milestone with derived state and progress, and each task with its rung and
the evidence that put it there.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			tree, err := client.GetInitiativeTree(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("orun initiatives view: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, tree)
			}
			fmt.Fprint(cmd.OutOrStdout(), renderInitiativeTree(tree))
			return nil
		},
	}
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// renderInitiativeTree renders the tree as an indented ladder, modeled on
// the plain evidence-bearing style of the old `orun work list`.
func renderInitiativeTree(t *remotestate.WorkInitiativeTree) string {
	var b strings.Builder
	ini := t.Initiative
	fmt.Fprintf(&b, "%s — %s  [%s]", ini.Key, ini.Title, ini.Status)
	if ini.Owner != "" {
		fmt.Fprintf(&b, "  owner %s", ini.Owner)
	}
	if ini.TargetDate != "" {
		fmt.Fprintf(&b, "  target %s", ini.TargetDate)
	}
	fmt.Fprintf(&b, "  %d/%d done", ini.ProgressView.Done, ini.ProgressView.Total)
	if ini.ProgressView.Active > 0 {
		fmt.Fprintf(&b, ", %d active", ini.ProgressView.Active)
	}
	b.WriteString("\n")
	for _, r := range ini.NeedsYou {
		fmt.Fprintf(&b, "  needs you: %s\n", r.Text)
	}
	for _, e := range t.Epics {
		fmt.Fprintf(&b, "\n  %s — %s  %s  %d/%d\n", e.Key, e.Title, intentChip(e.Intent), e.Progress.Done, e.Progress.Total)
		for _, m := range e.Milestones {
			fmt.Fprintf(&b, "    %s — %s  [%s]  %d/%d\n", m.Key, m.Title, m.State, m.Progress.Done, m.Progress.Total)
			for _, task := range m.Tasks {
				b.WriteString(renderTreeTask(task))
			}
		}
		if len(e.Backlog) > 0 {
			b.WriteString("    backlog:\n")
			for _, task := range e.Backlog {
				b.WriteString(renderTreeTask(task))
			}
		}
		for _, d := range e.Docs {
			rev := ""
			if d.Revision != "" {
				rev = " @" + shortRevision(d.Revision)
			}
			threads := ""
			if d.Threads != nil && d.Threads.Open > 0 {
				threads = fmt.Sprintf("  %d open thread(s)", d.Threads.Open)
			}
			fmt.Fprintf(&b, "    doc  %s %s%s [%s] — %s%s\n", d.Kind, d.Subject, rev, d.State, d.Title, threads)
		}
	}
	if len(t.Designs) > 0 {
		b.WriteString("\n  designs:\n")
		for _, d := range t.Designs {
			fmt.Fprintf(&b, "    %s — %s  [%s]\n", d.Key, d.Title, designStateOf(d))
		}
	}
	return b.String()
}

func renderTreeTask(t remotestate.WorkTreeTaskRow) string {
	line := fmt.Sprintf("      %-10s %-12s %s", t.Key, t.Rung, t.Title)
	if hint := evidenceHint(t.Evidence); hint != "" {
		line += "  (" + hint + ")"
	}
	return line + "\n"
}

// intentChip renders the intent ladder chip: state, the approved revision
// (approval never renders without it — V4-2), and drift when present.
func intentChip(i remotestate.WorkEpicIntentView) string {
	chip := "intent " + i.State
	if i.Approval != nil && i.Approval.Revision != "" {
		chip += " @" + shortRevision(i.Approval.Revision)
	}
	if i.DocDrifted || i.LadderDrifted {
		chip += " (drifted)"
	}
	return chip
}

// shortRevision shortens a sha256:<hex> doc digest to its familiar 8-hex form.
func shortRevision(rev string) string {
	r := strings.TrimPrefix(rev, "sha256:")
	if len(r) > 8 {
		r = r[:8]
	}
	return r
}

// evidenceHint folds the evidence view into the short parenthetical hint the
// tree and task views print. Empty when the logs are silent — never invented.
func evidenceHint(ev remotestate.WorkTaskEvidenceView) string {
	var parts []string
	if ev.Branch != nil {
		parts = append(parts, "branch "+ev.Branch.Name)
	}
	if ev.PR != nil {
		pr := "PR #" + ev.PR.Number
		if ev.PR.Merged {
			pr += " merged"
		} else {
			pr += " open"
		}
		if ev.PR.ChecksTotal > 0 {
			pr += fmt.Sprintf(", checks %d/%d", ev.PR.ChecksPassed, ev.PR.ChecksTotal)
		}
		parts = append(parts, pr)
	} else if ev.Checks != nil {
		parts = append(parts, fmt.Sprintf("checks %d/%d", ev.Checks.Passed, ev.Checks.Total))
	}
	return strings.Join(parts, "; ")
}

// designStateOf extracts the folded intent state from a design view.
func designStateOf(d remotestate.WorkDesignView) string {
	var intent struct {
		State string `json:"state"`
	}
	if len(d.Intent) > 0 && json.Unmarshal(d.Intent, &intent) == nil && intent.State != "" {
		return intent.State
	}
	return "draft"
}

// ── create ───────────────────────────────────────────────────────────────────

func newInitiativesCreateCommand() *cobra.Command {
	var (
		workspace   string
		backendURL  string
		asJSON      bool
		title       string
		why         []string
		description string
		owner       string
		target      string
		slug        string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an initiative envelope (--title, --why …)",
		Long: `Create an initiative: the strategic envelope grouping epics. Pure intent —
an initiative has no lifecycle and no contract; its status, progress, and
health derive from its member epics' logs.

--why is repeatable: each occurrence becomes one success criterion.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(title) == "" {
				return fmt.Errorf("orun initiatives create: --title is required")
			}
			if slug == "" {
				slug = slugFromTitle(title)
			}
			if slug == "" {
				return fmt.Errorf("orun initiatives create: cannot derive a slug from %q; pass --slug", title)
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.CreateInitiative(cmd.Context(), remotestate.CreateWorkInitiativeRequest{
				Slug:            slug,
				Title:           title,
				Description:     description,
				Owner:           owner,
				TargetDate:      target,
				SuccessCriteria: why,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives create: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "initiative title (required)")
	cmd.Flags().StringArrayVar(&why, "why", nil, "success criterion (repeatable; the human-edited why)")
	cmd.Flags().StringVar(&description, "description", "", "initiative description")
	cmd.Flags().StringVar(&owner, "owner", "", "owner subject")
	cmd.Flags().StringVar(&target, "target", "", "target date YYYY-MM-DD")
	cmd.Flags().StringVar(&slug, "slug", "", "initiative slug (default: derived from --title)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// slugFromTitle derives a lowercase hyphenated slug from a title.
func slugFromTitle(title string) string {
	var b strings.Builder
	lastHyphen := true
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ── edit / cancel (ported from the work group, shared with the alias) ────────

func newItemEditCommand() *cobra.Command {
	var (
		workspace   string
		backendURL  string
		asJSON      bool
		title       string
		description string
		owner       string
		target      string
		initiative  string
		criteria    []string
	)
	cmd := &cobra.Command{
		Use:   "edit <key>",
		Short: "Edit an item's envelope (title/description/owner/target/…)",
		Long: `Edit an item's envelope through the one mutator (item_edited): title,
plus the fields the item's kind exposes — description, owner, target date, and
success criteria for an initiative; target date and initiative filing for an
epic; title for a task.

This is intent only. Nothing here can move a rung — lifecycle stays derived
from observations (WP-3). Only the flags you pass are sent; pass an empty
value (e.g. --owner "") to clear a field.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			req := remotestate.EditWorkItemRequest{}
			if f.Changed("title") {
				req.Title = &title
			}
			if f.Changed("description") {
				req.Description = &description
			}
			if f.Changed("owner") {
				req.Owner = &owner
			}
			if f.Changed("target") {
				req.TargetDate = &target
			}
			if f.Changed("initiative") {
				req.Initiative = &initiative
			}
			if f.Changed("criteria") {
				req.SuccessCriteria = criteria
			}
			if req.Title == nil && req.Description == nil && req.Owner == nil &&
				req.TargetDate == nil && req.Initiative == nil && req.SuccessCriteria == nil {
				return fmt.Errorf("orun initiatives edit: nothing to change; pass at least one of --title/--description/--owner/--target/--initiative/--criteria")
			}

			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.EditWorkItem(cmd.Context(), args[0], req)
			if err != nil {
				return fmt.Errorf("orun initiatives edit: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "edited %s (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description (initiatives)")
	cmd.Flags().StringVar(&owner, "owner", "", "owner subject; \"\" clears (initiatives)")
	cmd.Flags().StringVar(&target, "target", "", "target date YYYY-MM-DD; \"\" clears")
	cmd.Flags().StringVar(&initiative, "initiative", "", "file an epic under this initiative key; \"\" unfiles")
	cmd.Flags().StringArrayVar(&criteria, "criteria", nil, "success criterion (repeatable; initiatives)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newItemCancelCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "cancel <key>",
		Short: "Retire an item (task or epic) — the append-only \"delete\"",
		Long: `Retire a task or epic by folding a terminal 'canceled' state onto it.
Cancel is the model's native "delete": a terminal, attributed, append-only
state — the record and its whole history stay, and agents stop picking up its
work. It is effectively permanent (there is no un-cancel).

Initiatives have no lifecycle to cancel — edit their envelope, or retire their
epics, instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if !yes && termIsInteractive() {
				fmt.Fprintf(cmd.OutOrStdout(), "Retire %s? This is terminal and cannot be un-canceled. Re-run with --yes to confirm.\n", key)
				return fmt.Errorf("orun initiatives cancel: confirmation required (pass --yes)")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.CancelWorkItem(cmd.Context(), key)
			if err != nil {
				return fmt.Errorf("orun initiatives cancel: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "retired %s — canceled (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── import (the v2 importer, unchanged wire) ─────────────────────────────────

func toWireContract(c *worklens.Contract) *remotestate.WorkContract {
	if c == nil {
		return nil
	}
	return &remotestate.WorkContract{
		Goal:         c.Goal,
		Affects:      c.Affects,
		DoneWhen:     c.DoneWhen,
		Gates:        c.Gates,
		DesignRefs:   c.DesignRefs,
		Deps:         c.Deps,
		GatesDefined: c.GatesDefined,
	}
}

func toWirePlan(plan *worklens.ImportPlan, prefix string) remotestate.WorkImportRequest {
	req := remotestate.WorkImportRequest{
		Workspace: plan.Workspace,
		Root:      plan.Root,
		Prefix:    prefix,
	}
	for _, i := range plan.Initiatives {
		req.Initiatives = append(req.Initiatives, remotestate.WorkImportInitiative{Slug: i.Slug, Title: i.Title})
	}
	for _, s := range plan.Specs {
		req.Specs = append(req.Specs, remotestate.WorkImportSpec{
			Slug: s.Slug, Title: s.Title, DocPath: s.DocPath, DocSHA256: s.DocSHA256, PlanPath: s.PlanPath,
			Initiative: s.Initiative,
		})
	}
	for _, m := range plan.Milestones {
		req.Milestones = append(req.Milestones, remotestate.WorkImportMilestone{
			SpecSlug: m.SpecSlug, Key: m.Key, Title: m.Title, Goal: m.Goal, DoneWhen: m.DoneWhen, Ordinal: m.Ordinal,
		})
	}
	for _, t := range plan.Tasks {
		req.Tasks = append(req.Tasks, remotestate.WorkImportTask{
			SpecSlug: t.SpecSlug, MilestoneID: t.MilestoneID, Milestone: t.Milestone, Title: t.Title, Contract: toWireContract(t.Contract),
		})
	}
	return req
}

func newWorkImportCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		prefix     string
		dryRun     bool
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "import <specs-dir>",
		Short: "Map a specs tree to the hierarchy (epic READMEs → Epics, plan headings → Milestones)",
		Long: `Parse a repo's specs tree into the work plane's import plan: each epic
folder's README.md becomes an Epic (doc body content-addressed verbatim),
each implementation-plan.md milestone heading becomes a ladder Milestone,
checklist items become Tasks, and roadmap clusters become Initiatives.

Lifecycle is never imported: IMPLEMENTATION-STATUS.md tables stay behind, and
rungs derive from real observations after apply — the point of the exercise.

--dry-run prints the deterministic mapping; without it the plan applies to
Orun Cloud through the work mutators (every event lands as via=import;
re-imports skip existing specs and milestones).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := worklens.ParseSpecTree(args[0], workspace)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if dryRun {
				if asJSON {
					return encodeJSON(cmd, plan)
				}
				fmt.Fprintf(out, "workspace:   %s\n", plan.Workspace)
				if len(plan.Initiatives) > 0 {
					fmt.Fprintf(out, "initiatives: %d\n", len(plan.Initiatives))
				}
				fmt.Fprintf(out, "specs:       %d\n", len(plan.Specs))
				fmt.Fprintf(out, "milestones:  %d\n", len(plan.Milestones))
				fmt.Fprintf(out, "tasks:       %d\n\n", len(plan.Tasks))
				for _, s := range plan.Specs {
					n := 0
					for _, t := range plan.Tasks {
						if t.SpecSlug == s.Slug {
							n++
						}
					}
					fmt.Fprintf(out, "  %-32s %2d tasks  %s\n", s.Slug, n, s.DocSHA256[:19])
				}
				fmt.Fprintln(out, "\n(dry run — nothing written)")
				return nil
			}

			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.ImportWork(cmd.Context(), toWirePlan(plan, prefix))
			if err != nil {
				return fmt.Errorf("orun initiatives import: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			if resp.InitiativesCreated+resp.InitiativesSkipped > 0 {
				fmt.Fprintf(out, "initiatives: %d created, %d skipped\n", resp.InitiativesCreated, resp.InitiativesSkipped)
			}
			fmt.Fprintf(out, "specs:       %d created, %d skipped\n", resp.SpecsCreated, resp.SpecsSkipped)
			fmt.Fprintf(out, "milestones:  %d created, %d skipped\n", resp.MilestonesCreated, resp.MilestonesSkipped)
			fmt.Fprintf(out, "tasks:       %d created, %d skipped, %d migrated into milestones\n", resp.TasksCreated, resp.TasksSkipped, resp.TasksMigrated)
			fmt.Fprintln(out, "\nlifecycle derives from observations — check `orun initiatives list`")
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "WRK", "task-key prefix for imported milestones (2–5 uppercase)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the mapping without writing")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── task view / task create ──────────────────────────────────────────────────

func newInitiativesTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task detail (view), creation (create), and the agent's voice (done, note)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newInitiativesTaskViewCommand())
	cmd.AddCommand(newInitiativesTaskCreateCommand())
	cmd.AddCommand(newInitiativesTaskDoneCommand())
	cmd.AddCommand(newInitiativesTaskNoteCommand())
	return cmd
}

func newInitiativesTaskViewCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "view <key>",
		Short: "One task: rung ladder, evidence, components affected, activity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			detail, err := client.GetTaskDetail(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("orun initiatives task view: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, detail)
			}
			fmt.Fprint(cmd.OutOrStdout(), renderTaskDetail(detail))
			return nil
		},
	}
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// rungLadder is the derived delivery ladder, in fold order. Canceled is a
// terminal side-exit and renders bare.
var rungLadder = []string{"draft", "ready", "in_progress", "in_review", "done", "released"}

// rungLadderLine renders the ladder with the current rung bracketed.
func rungLadderLine(rung string) string {
	parts := make([]string, len(rungLadder))
	found := false
	for i, r := range rungLadder {
		if r == rung {
			parts[i] = "[" + r + "]"
			found = true
		} else {
			parts[i] = r
		}
	}
	if !found {
		return rung
	}
	return strings.Join(parts, " › ")
}

// renderTaskDetail renders the task page: rung ladder, ancestry, evidence,
// components affected, recent activity.
func renderTaskDetail(d *remotestate.WorkTaskDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\n", d.Task.Key, d.Task.Title)
	fmt.Fprintf(&b, "rung  %s\n", rungLadderLine(d.Task.Lifecycle.Rung))
	if d.Task.Lifecycle.Blocked {
		b.WriteString("      [blocked]\n")
	}
	var ancestry []string
	if d.Initiative != nil {
		ancestry = append(ancestry, fmt.Sprintf("initiative %s — %s", d.Initiative.Key, d.Initiative.Title))
	}
	if d.Epic != nil {
		ancestry = append(ancestry, fmt.Sprintf("epic %s — %s", d.Epic.Key, d.Epic.Title))
	}
	if d.Milestone != nil {
		ancestry = append(ancestry, fmt.Sprintf("milestone %s — %s", d.Milestone.Key, d.Milestone.Title))
	}
	if len(ancestry) > 0 {
		fmt.Fprintf(&b, "under %s\n", strings.Join(ancestry, " · "))
	}

	b.WriteString("\nevidence\n")
	printed := false
	if d.Evidence.Branch != nil {
		line := "  branch " + d.Evidence.Branch.Name
		if d.Evidence.Branch.LastPushAt != "" {
			line += "  (last push " + d.Evidence.Branch.LastPushAt + ")"
		}
		b.WriteString(line + "\n")
		printed = true
	}
	if d.Evidence.PR != nil {
		pr := d.Evidence.PR
		state := "open"
		if pr.Merged {
			state = "merged"
			if pr.MergedAt != "" {
				state += " " + pr.MergedAt
			}
		}
		line := fmt.Sprintf("  PR #%s %s", pr.Number, state)
		if pr.ChecksTotal > 0 {
			line += fmt.Sprintf("  checks %d/%d", pr.ChecksPassed, pr.ChecksTotal)
		}
		b.WriteString(line + "\n")
		printed = true
	}
	if d.Evidence.Checks != nil {
		fmt.Fprintf(&b, "  checks %d/%d", d.Evidence.Checks.Passed, d.Evidence.Checks.Total)
		if d.Evidence.Checks.At != "" {
			fmt.Fprintf(&b, "  (%s)", d.Evidence.Checks.At)
		}
		b.WriteString("\n")
		printed = true
	}
	if !printed {
		b.WriteString("  (nothing observed yet)\n")
	}

	if len(d.ComponentsAffected) > 0 {
		b.WriteString("\ncomponents affected\n")
		for _, c := range d.ComponentsAffected {
			line := "  " + c.Path
			if c.Additions > 0 || c.Deletions > 0 {
				line += fmt.Sprintf("  +%d -%d", c.Additions, c.Deletions)
			}
			if c.Summary != "" {
				line += "  " + c.Summary
			}
			b.WriteString(line + "\n")
		}
	}

	if len(d.Activity) > 0 {
		b.WriteString("\nactivity\n")
		for _, e := range d.Activity {
			fmt.Fprintf(&b, "  %s  %s\n", e.At, e.Text)
		}
	}
	return b.String()
}

func newInitiativesTaskCreateCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		prefix     string
		title      string
		epic       string
		milestone  string
		goal       string
		doneWhen   []string
		affects    []string
		gates      []string
		deps       []string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a task through the one mutator surface",
		Long: `Create a task under an epic's milestone (or unscheduled). The contract
flags author the task's definition of done; rungs derive from observations
afterwards — there is no status to set here, ever.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(title) == "" {
				return fmt.Errorf("orun initiatives task create: --title is required")
			}
			var contract *remotestate.WorkContract
			if goal != "" || len(doneWhen) > 0 || len(affects) > 0 || len(gates) > 0 || len(deps) > 0 {
				contract = &remotestate.WorkContract{
					Goal: goal, DoneWhen: doneWhen, Affects: affects, Gates: gates, Deps: deps,
				}
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.CreateWorkTask(cmd.Context(), remotestate.CreateWorkTaskRequest{
				Prefix: prefix, Title: title, SpecKey: epic, Milestone: milestone, Contract: contract,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives task create: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "WRK", "task-key prefix (2–5 uppercase)")
	cmd.Flags().StringVar(&title, "title", "", "task title (required)")
	cmd.Flags().StringVar(&epic, "epic", "", "parent epic slug")
	cmd.Flags().StringVar(&milestone, "milestone", "", "milestone key within --epic")
	cmd.Flags().StringVar(&goal, "contract-goal", "", "contract goal (one or two sentences)")
	cmd.Flags().StringArrayVar(&doneWhen, "contract-done-when", nil, "done-when criterion (repeatable)")
	cmd.Flags().StringArrayVar(&affects, "contract-affects", nil, "affected catalog component key (repeatable)")
	cmd.Flags().StringArrayVar(&gates, "contract-gate", nil, "gate check (repeatable)")
	cmd.Flags().StringArrayVar(&deps, "contract-dep", nil, "dependency task key (repeatable)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── activity ─────────────────────────────────────────────────────────────────

func newInitiativesActivityCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		limit      int
		cursor     string
	)
	cmd := &cobra.Command{
		Use:   "activity <key>",
		Short: "The tagged activity tail for any noun (initiative, epic, task, design)",
		Long: `Fetch the tagged activity tail: both logs folded into one reverse-
chronological list. The tag trail is ancestry — filtering by an epic covers
its milestones' tasks, docs, and designs; an initiative covers its whole
subtree.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			activity, err := client.GetWorkActivity(cmd.Context(), remotestate.WorkActivityOptions{
				Tag: args[0], Limit: limit, Cursor: cursor,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives activity: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, activity)
			}
			fmt.Fprint(cmd.OutOrStdout(), renderActivity(activity))
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum entries to fetch")
	cmd.Flags().StringVar(&cursor, "cursor", "", "resume from a prior page's cursor")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// renderActivity renders "TIME  TEXT  [TAG]" lines plus the next-page hint.
func renderActivity(a *remotestate.WorkActivity) string {
	var b strings.Builder
	for _, e := range a.Entries {
		fmt.Fprintf(&b, "%s  %s  [%s]\n", e.At, e.Text, e.Tag)
	}
	if a.NextCursor != "" {
		fmt.Fprintf(&b, "\nmore: re-run with --cursor %s\n", a.NextCursor)
	}
	return b.String()
}

// ── doc pull ─────────────────────────────────────────────────────────────────

func newInitiativesDocCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Cloud documents: pull an epic spec / design doc as markdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newInitiativesDocPullCommand())
	return cmd
}

func newInitiativesDocPullCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		rev        string
	)
	cmd := &cobra.Command{
		Use:   "pull <key>",
		Short: "Print an item's cloud document (markdown) to stdout",
		Long: `Fetch an item's content-addressed cloud document — an epic's spec or a
design's doc — and print the markdown body to stdout (latest revision unless
--rev pins one). Pipe it wherever you need the text.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			doc, err := client.GetWorkDoc(cmd.Context(), args[0], rev)
			if err != nil {
				return fmt.Errorf("orun initiatives doc pull: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, doc)
			}
			fmt.Fprint(cmd.OutOrStdout(), doc.Body)
			if !strings.HasSuffix(doc.Body, "\n") {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rev, "rev", "", "revision digest sha256:<hex> (default: latest)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── design list / design propose ─────────────────────────────────────────────

func newInitiativesDesignCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "design",
		Short: "Design runs on an initiative: list, propose",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newInitiativesDesignListCommand())
	cmd.AddCommand(newInitiativesDesignProposeCommand())
	return cmd
}

func newInitiativesDesignListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list <initiative>",
		Short: "List an initiative's design runs with their folded intent state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			designs, err := client.ListWorkDesigns(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("orun initiatives design list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, designs)
			}
			rows := make([][]string, 0, len(designs.Designs))
			for _, d := range designs.Designs {
				rows = append(rows, []string{d.Key, d.Title, designStateOf(d), d.CreatedBy.ID})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"KEY", "TITLE", "STATE", "BY"}, rows))
			return nil
		},
	}
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newInitiativesDesignProposeCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		title      string
		docRef     string
		proposal   string
	)
	cmd := &cobra.Command{
		Use:   "propose <initiative>",
		Short: "Start a Draft design run under an initiative",
		Long: `Create a Draft design under an initiative: a document reference plus an
optional structured proposal (epics → milestones → task skeletons). A design
is a PROPOSAL — humans review, compare, and adopt; adoption mints the epics
and stays human-only.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(title) == "" {
				return fmt.Errorf("orun initiatives design propose: --title is required")
			}
			req := remotestate.CreateWorkDesignRequest{Title: title, DocRef: docRef}
			if proposal != "" {
				if !json.Valid([]byte(proposal)) {
					return fmt.Errorf("orun initiatives design propose: --proposal is not valid JSON")
				}
				req.Proposal = json.RawMessage(proposal)
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.CreateWorkDesign(cmd.Context(), args[0], req)
			if err != nil {
				return fmt.Errorf("orun initiatives design propose: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "proposed design %s (seq %d) — a human reviews, compares, and adopts\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "design title (required)")
	cmd.Flags().StringVar(&docRef, "doc-ref", "", "design doc revision sha256:<hex>")
	cmd.Flags().StringVar(&proposal, "proposal", "", "structured proposal JSON ({\"epics\":[…]})")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}
