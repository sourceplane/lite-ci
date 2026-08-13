package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// orun initiatives — the orun-initiatives-v2 (IS4) verbs: the stored state
// machine (start/pause/resume/complete/reopen), the update cadence
// (update/updates), the any-key context bundle, the generalized assign,
// review request/verdict, adoption (interactive confirm = the signature),
// the agent's voice (task done / task note), and the live board (now).
// Initiative status is a stored SPEECH ACT now; task rungs stay derived —
// nothing here can move one.

// statusVerbs maps each CLI verb to its target state. resume and reopen
// both aim at active — the server judges legality from the current state
// and answers an illegal move with 409 naming the allowed transitions.
var statusVerbs = []struct {
	use    string
	to     string
	short  string
	long   string
	forced bool // registers --force (complete only: acknowledge open tasks)
}{
	{"start", "active", "Start an initiative: planning → active",
		"Move an initiative into active. Legal from planning (and from paused via resume\n— same target state, the machine knows the difference).", false},
	{"pause", "paused", "Pause an initiative: active → paused (closes the dispatch gate)",
		"Pause an initiative. Agents stop being dispatched into its epics until resume;\nin-flight tasks finish.", false},
	{"resume", "active", "Resume a paused initiative: paused → active",
		"Resume a paused initiative — reopens the dispatch gate.", false},
	{"complete", "completed", "Complete an initiative (terminal; a human signature)",
		"Complete an initiative. A terminal move — human-only on the wire (agent seats\nget the typed human_only refusal). Open member tasks produce a warning, never a\nblock; pass --force to acknowledge them silently.", true},
	{"reopen", "active", "Reopen a completed initiative: completed → active",
		"Reopen a completed initiative — a human signature, like every terminal move.", false},
}

func newInitiativeStatusCommands() []*cobra.Command {
	cmds := make([]*cobra.Command, 0, len(statusVerbs))
	for _, v := range statusVerbs {
		verb := v // capture
		var (
			workspace  string
			backendURL string
			asJSON     bool
			comment    string
			force      bool
		)
		cmd := &cobra.Command{
			Use:   verb.use + " <key>",
			Short: verb.short,
			Long:  verb.long,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := workClient(cmd.Context(), backendURL, workspace)
				if err != nil {
					return err
				}
				resp, err := client.SetInitiativeStatus(cmd.Context(), args[0], remotestate.SetInitiativeStatusRequest{
					To: verb.to, Comment: comment, Force: force,
				})
				if err != nil {
					return fmt.Errorf("orun initiatives %s: %w", verb.use, err)
				}
				if asJSON {
					return encodeJSON(cmd, resp)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s is now %s (seq %d)\n", resp.Key, resp.Status, resp.Seq)
				if resp.Warning != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "warning: %s\n", resp.Warning)
				}
				return nil
			},
		}
		cmd.Flags().StringVarP(&comment, "message", "m", "", "why (recorded on the transition)")
		if verb.forced {
			cmd.Flags().BoolVar(&force, "force", false, "acknowledge open member tasks (the warn never blocks)")
		}
		addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
		cmds = append(cmds, cmd)
	}
	return cmds
}

// ── update / updates — the health cadence ────────────────────────────────────

// healthWordOf accepts both spellings (on-track / on_track) and returns the
// wire form.
func healthWordOf(s string) (string, error) {
	switch strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "_") {
	case "on_track":
		return "on_track", nil
	case "at_risk":
		return "at_risk", nil
	case "off_track":
		return "off_track", nil
	default:
		return "", fmt.Errorf("unknown health %q (on-track | at-risk | off-track)", s)
	}
}

func newInitiativesUpdateCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		health     string
		message    string
	)
	cmd := &cobra.Command{
		Use:   "update <key>",
		Short: "Post an attributed health update (the headline, not a formula)",
		Long: `Post an initiative update: a health word you are prepared to defend plus the
narrative. Health is the latest update's headline — never computed, never set
directly; the derived signals only suggest. Staleness derives at read.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			word, err := healthWordOf(health)
			if err != nil {
				return fmt.Errorf("orun initiatives update: %w", err)
			}
			if strings.TrimSpace(message) == "" {
				return fmt.Errorf("orun initiatives update: -m is required — an update without a narrative is a mood, not an update")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.PostInitiativeUpdate(cmd.Context(), args[0], remotestate.PostInitiativeUpdateRequest{
				Health: word, Body: message,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives update: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "update posted on %s (seq %d) — health headline now %s\n", resp.Key, resp.Seq, resp.Update.Health)
			return nil
		},
	}
	cmd.Flags().StringVar(&health, "health", "", "on-track | at-risk | off-track (required)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "the update narrative (required)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newInitiativesUpdatesCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "updates <key>",
		Short: "The update feed, newest first: attributed health headlines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			feed, err := client.ListInitiativeUpdates(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("orun initiatives updates: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, feed)
			}
			if len(feed.Updates) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no updates posted yet)")
				return nil
			}
			rows := make([][]string, 0, len(feed.Updates))
			for _, u := range feed.Updates {
				rows = append(rows, []string{u.CreatedAt, u.Health, u.Author.ID, u.Body})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"AT", "HEALTH", "BY", "UPDATE"}, rows))
			return nil
		},
	}
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── context — the any-key bundle ─────────────────────────────────────────────

func newInitiativesContextCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		depth      int
		perLevel   int
		activity   int
	)
	cmd := &cobra.Command{
		Use:   "context <key>",
		Short: "The any-key context bundle: item, ancestry, activity, needs-you",
		Long: `Resolve any key — typed (PAY-T14), letterless, milestone (PAY-E2#M1), slug,
alias, or machine id — and print its context bundle: the item, its ancestry to
the root with live states, the recent activity tail, and what currently waits
on a human. Truncation is always echoed (budget lines) — no silent caps.
Use --json for the full typed view.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			bundle, err := client.GetWorkContext(cmd.Context(), args[0], remotestate.WorkContextOptions{
				Depth: depth, PerLevel: perLevel, Activity: activity,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives context: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, bundle)
			}
			fmt.Fprint(cmd.OutOrStdout(), renderWorkContext(bundle))
			return nil
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 0, "subtree depth below the item (server default 2, max 4)")
	cmd.Flags().IntVar(&perLevel, "per-level", 0, "children per level (server default 50, max 200)")
	cmd.Flags().IntVar(&activity, "activity", 0, "activity tail length (server default 20, max 100)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// renderWorkContext renders the bundle: the resolved item header, the
// ancestry trail root-last, needs-you, the budget echoes, and the tail.
// The kind-typed view stays JSON-only (--json) — the ladder renderers own
// the pretty forms and each kind already has a dedicated command.
func renderWorkContext(b *remotestate.WorkContext) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s — %s  [%s]", b.Item.CanonicalKey, b.Item.Title, b.Item.Kind)
	if b.Item.PublicID != "" {
		fmt.Fprintf(&sb, "  (%s)", b.Item.PublicID)
	}
	sb.WriteString("\n")
	if b.MovedFrom != "" {
		fmt.Fprintf(&sb, "resolved from %s\n", b.MovedFrom)
	}
	if len(b.Ancestry) > 0 {
		trail := make([]string, 0, len(b.Ancestry))
		for _, a := range b.Ancestry {
			part := a.CanonicalKey
			if a.State != "" {
				part += " [" + a.State + "]"
			}
			trail = append(trail, part)
		}
		fmt.Fprintf(&sb, "under %s\n", strings.Join(trail, " · "))
	}
	for _, r := range b.NeedsYou {
		fmt.Fprintf(&sb, "needs you: %s\n", r.Text)
	}
	for _, bd := range b.Budget {
		if bd.Returned < bd.Total {
			fmt.Fprintf(&sb, "truncated: %s %d/%d", bd.Level, bd.Returned, bd.Total)
			if bd.Cursor != "" {
				fmt.Fprintf(&sb, " (cursor %s)", bd.Cursor)
			}
			sb.WriteString("\n")
		}
	}
	if len(b.Activity) > 0 {
		sb.WriteString("\nactivity\n")
		for _, e := range b.Activity {
			fmt.Fprintf(&sb, "  %s  %s\n", e.At, e.Text)
		}
	}
	sb.WriteString("\n(full typed view: --json)\n")
	return sb.String()
}

// ── assign — the generalized assign ──────────────────────────────────────────

func newInitiativesAssignCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		unassign   bool
		override   string
	)
	cmd := &cobra.Command{
		Use:   "assign <key> <subject>",
		Short: "Assign a subject to any noun: task, design, epic or initiative (owner)",
		Long: `Assign a membership subject (usr_/sp_/team_) to any noun — a task (claims
work), a design (names the author), an epic or initiative (names the owner).
The dispatch gate is unchanged: an sp_ seat into a non-approved epic still
refuses unless --override supplies the attributed reason.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.AssignWorkItem(cmd.Context(), args[0], remotestate.AssignWorkItemRequest{
				Subject: args[1], Unassign: unassign, Override: override,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives assign: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			verb := "assigned"
			if unassign {
				verb = "unassigned"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s ↔ %s (seq %d)\n", verb, resp.Key, args[1], resp.Seq)
			return nil
		},
	}
	cmd.Flags().BoolVar(&unassign, "unassign", false, "remove the assignment instead")
	cmd.Flags().StringVar(&override, "override", "", "attributed override note for the dispatch gate")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── review request | verdict ─────────────────────────────────────────────────

func newInitiativesReviewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review flow on epics and designs: request eyes, record a verdict",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newReviewRequestCommand())
	cmd.AddCommand(newReviewVerdictCommand())
	return cmd
}

func newReviewRequestCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		note       string
		revision   string
		reviewers  []string
	)
	cmd := &cobra.Command{
		Use:   "request <key>",
		Short: "Request review on an epic or a design",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.RequestWorkReview(cmd.Context(), remotestate.ReviewCollectionOf(args[0]), args[0], remotestate.WorkReviewRequest{
				Revision: revision, Reviewers: reviewers, Note: note,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives review request: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "review requested on %s (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "what to look at")
	cmd.Flags().StringVar(&revision, "revision", "", "doc revision sha256:<hex> under review")
	cmd.Flags().StringArrayVar(&reviewers, "reviewer", nil, "reviewer subject (repeatable)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newReviewVerdictCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		verdict    string
		note       string
		revision   string
	)
	cmd := &cobra.Command{
		Use:   "verdict <key>",
		Short: "Record a review verdict: an opinion, not a decision",
		Long: `Record approve or request_changes with the reasoning. A verdict is an
OPINION — the decision (epic approval, design adoption) is a separate,
signed act.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if verdict != "approve" && verdict != "request_changes" {
				return fmt.Errorf("orun initiatives review verdict: --verdict must be approve or request_changes")
			}
			if strings.TrimSpace(note) == "" {
				return fmt.Errorf("orun initiatives review verdict: -m is required — an opinion without a reason is a vote, not a review")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.SubmitWorkVerdict(cmd.Context(), remotestate.ReviewCollectionOf(args[0]), args[0], remotestate.WorkVerdictRequest{
				Revision: revision, Verdict: verdict, Note: note,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives review verdict: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "verdict %s recorded on %s (seq %d)\n", verdict, resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&verdict, "verdict", "", "approve | request_changes (required)")
	cmd.Flags().StringVarP(&note, "message", "m", "", "the reasoning (required)")
	cmd.Flags().StringVar(&revision, "revision", "", "doc revision the verdict pins")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── adopt — the signature ────────────────────────────────────────────────────

func newInitiativesAdoptCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		yes        bool
		taskPrefix string
		epics      []string
	)
	cmd := &cobra.Command{
		Use:   "adopt <design-key>",
		Short: "Adopt a design: mint its structure and approve the minted epics (a signature)",
		Long: `Adopt a design: mints the proposed epics → milestones → task skeletons AND
approves the minted epics at rev 0, one transaction — one signature covers
what it mints. The confirmation IS the signature: interactive runs confirm,
non-interactive runs require --yes. Human-only server-side.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if !yes {
				if !termIsInteractive() {
					return fmt.Errorf("orun initiatives adopt: confirmation required (pass --yes) — adoption mints structure and approves it")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Adopt %s? This mints the proposed structure and approves the minted epics at rev 0. [y/N] ", key)
				var answer string
				if _, err := fmt.Fscanln(cmd.InOrStdin(), &answer); err != nil || (answer != "y" && answer != "Y" && answer != "yes") {
					return fmt.Errorf("orun initiatives adopt: not confirmed")
				}
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.AdoptDesign(cmd.Context(), key, remotestate.AdoptDesignRequest{
				Epics: epics, TaskPrefix: taskPrefix,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives adopt: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "adopted %s (seq %d)\nminted epics: %s\ntask skeletons: %s\n",
				key, resp.Seq, strings.Join(resp.Minted, ", "), strings.Join(resp.Tasks, ", "))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt (non-interactive signature)")
	cmd.Flags().StringVar(&taskPrefix, "task-prefix", "", "task-key prefix for minted skeletons")
	cmd.Flags().StringArrayVar(&epics, "epic", nil, "proposal epic slug to mint (repeatable; default all)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── task done / task note — the agent's voice ────────────────────────────────

func newInitiativesTaskDoneCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		note       string
	)
	cmd := &cobra.Command{
		Use:   "done <key>",
		Short: "Assert a task is done, with the mandatory why (the weakest voice)",
		Long: `Assert a task is done. The note is mandatory — an assertion without a
reason is a status write. The fold treats the assertion as the WEAKEST
voice: live delivery evidence at in_review or above wins, released stays
evidence-only, and the record names who asserted.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(note) == "" {
				return fmt.Errorf("orun initiatives task done: -m is required — say why the work is done")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.AssertTaskDone(cmd.Context(), args[0], note, "")
			if err != nil {
				return fmt.Errorf("orun initiatives task done: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "done asserted on %s (seq %d) — live evidence at in_review+ wins\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVarP(&note, "message", "m", "", "why the work is done (required)")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newInitiativesTaskNoteCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		text       string
		ref        string
	)
	cmd := &cobra.Command{
		Use:   "note <key>",
		Short: "Append a worklog note — the task's live now line (fold-inert)",
		Long: `Append a worklog note: one present-tense line (≤280 chars) that becomes the
task's live *now* line on the board and the tree. Narration is INERT — it
moves no rung, feeds no health, triggers nothing. Clamped per seat
(1/min/task, daily cap); beyond the clamp comes a typed rate_limited verdict.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("orun initiatives task note: -m is required")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.PostTaskNote(cmd.Context(), args[0], remotestate.PostTaskNoteRequest{
				Text: text, Ref: ref,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives task note: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, resp)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "noted on %s (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVarP(&text, "message", "m", "", "the now line (required)")
	cmd.Flags().StringVar(&ref, "ref", "", "evidence anchor: commit sha, file path, PR number")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

// ── now — the live board ─────────────────────────────────────────────────────

func newInitiativesNowCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		initiative string
		epic       string
		seat       string
		limit      int
		cursor     string
	)
	cmd := &cobra.Command{
		Use:   "now",
		Short: "The live board: every in-flight task × its latest note × its seat",
		Long: `The "what is every agent doing right now" read: in-flight tasks with their
latest worklog line and the seat working them. Quiet marks an assigned,
in-flight task that has been silent past the quiet window.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			board, err := client.GetWorkNow(cmd.Context(), remotestate.WorkNowOptions{
				Initiative: initiative, Epic: epic, Seat: seat, Limit: limit, Cursor: cursor,
			})
			if err != nil {
				return fmt.Errorf("orun initiatives now: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, board)
			}
			if len(board.Rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(nothing in flight)")
				return nil
			}
			rows := make([][]string, 0, len(board.Rows))
			for _, r := range board.Rows {
				nowText, at := "-", "-"
				if r.Now != nil {
					nowText, at = r.Now.Text, r.Now.At
				}
				if r.Quiet {
					nowText += "  [quiet]"
				}
				rows = append(rows, []string{r.Key, r.Rung, orDash(r.Seat), nowText, at})
			}
			out := renderColumns([]string{"KEY", "RUNG", "SEAT", "NOW", "AT"}, rows)
			if board.NextCursor != "" {
				out += fmt.Sprintf("\nmore: re-run with --cursor %s\n", board.NextCursor)
			}
			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().StringVar(&initiative, "initiative", "", "filter to one initiative key")
	cmd.Flags().StringVar(&epic, "epic", "", "filter to one epic key")
	cmd.Flags().StringVar(&seat, "seat", "", "filter to one seat id (sp_…)")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum rows")
	cmd.Flags().StringVar(&cursor, "cursor", "", "resume from a prior page's cursor")
	addWorkScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}
