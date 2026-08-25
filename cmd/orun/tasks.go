package main

// orun task (orun-tasks O2) — the CLI face of the task plane, split exactly
// along the epic's boundary: identity comes from the cloud (create/attach/
// show/list — the allocator is the single writer of keys, TK-I), while the
// contract is authored in the repo (tasks/<KEY>.TaskContract.yaml) and
// checked OFFLINE (check — the design §3.3 loop). The check verb is
// advisory by construction: plan-side evaluation guides, the workspace
// decides at enforcement (E1/E2), and effective access is always
// resolved_policy ∩ contract — narrower than either input (TK-7).

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/contract"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/taskfile"
	"github.com/sourceplane/orun/internal/taskobj"
)

func registerTaskCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Tasks: cloud-issued identity, repo-authored contracts, offline checks",
		Long: `A task binds work to an enforceable contract. The cloud allocator issues
every key (adopt > derive > mint — never invented client-side); the
contract lives in the repository as tasks/<KEY>.TaskContract.yaml and is
sealed by content hash wherever it travels. 'check' runs entirely offline.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newTaskCreateCommand())
	cmd.AddCommand(newTaskAttachCommand())
	cmd.AddCommand(newTaskListCommand())
	cmd.AddCommand(newTaskShowCommand())
	cmd.AddCommand(newTaskCheckCommand())
	root.AddCommand(cmd)
}

// taskDocRoot is where tasks/ documents are looked up: the repo root the
// intent file anchors (the same resolution the policy commands use).
func taskDocRoot() string {
	_, repoRoot := policyIntentContext()
	if repoRoot == "" {
		return "."
	}
	return repoRoot
}

func newTaskCreateCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		adoptKey   string
		derive     string
		mintPrefix string
		title      string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Ask the allocator for a task; attach the repo's contract if one exists",
		Long: `Create a task. The key comes from the cloud allocator's ladder — adopt a
tracker key (--adopt), derive from a repo issue (--derive web#123), or
mint from a sequence (--prefix). If tasks/<KEY>.TaskContract.yaml exists
for the issued key it is sealed and attached in the same breath, and the
task is recorded in the local object store (refs/tasks/<KEY>).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			req := remotestate.TaskCreateRequest{
				AdoptKey:    adoptKey,
				MintPrefix:  mintPrefix,
				TitleMirror: title,
			}
			if derive != "" {
				d, err := parseDeriveRef(derive)
				if err != nil {
					return err
				}
				req.Derive = d
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			task, err := client.CreateTask(cmd.Context(), org, req)
			if err != nil {
				return fmt.Errorf("orun task create: %w", err)
			}

			doc, hash, attachErr := attachDocumentIfPresent(cmd.Context(), client, org, task.Key)
			sealNote := sealTaskLocally(cmd.Context(), task, doc)

			if asJSON {
				return encodeJSON(cmd, map[string]any{
					"task":         task,
					"contractHash": hash,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s (%s)\n", task.Key, task.ID)
			switch {
			case attachErr != nil:
				fmt.Fprintf(cmd.OutOrStdout(), "contract not attached: %v\n", attachErr)
			case doc != nil:
				fmt.Fprintf(cmd.OutOrStdout(), "contract %s attached from %s\n", shortRevision(hash), doc.Path)
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "no contract document (%s) — created without narrowing\n", taskfile.PathFor(taskDocRoot(), task.Key))
			}
			if sealNote != "" {
				fmt.Fprintln(cmd.OutOrStdout(), sealNote)
			}
			if attachErr != nil {
				return fmt.Errorf("orun task create: contract attach: %w", attachErr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&adoptKey, "adopt", "", "tracker key to adopt (used only if free — the allocator decides)")
	cmd.Flags().StringVar(&derive, "derive", "", "derive the key from a repo issue (prefix#number, e.g. web#123)")
	cmd.Flags().StringVar(&mintPrefix, "prefix", "", "sequence prefix for minted keys (default TSK)")
	cmd.Flags().StringVar(&title, "title", "", "display title mirror")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskAttachCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "attach <key>",
		Short: "Seal tasks/<KEY>.TaskContract.yaml and upload it to the task",
		Long: `Attach (or revise) a task's contract from the repository document. The
contract is sealed locally (sha256 over canonical JSON), the server
recomputes the hash and refuses a mismatch, and the local object store's
task node moves to the new revision.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			doc, err := taskfile.FindForKey(taskDocRoot(), key)
			if err != nil {
				return fmt.Errorf("orun task attach: %w", err)
			}
			if doc == nil {
				return fmt.Errorf("orun task attach: no document at %s", taskfile.PathFor(taskDocRoot(), key))
			}
			hash, wire, err := contract.ContractID(doc.Contract)
			if err != nil {
				return fmt.Errorf("orun task attach: %w", err)
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			seal, err := client.AttachTaskContract(cmd.Context(), org, key, wire, hash)
			if err != nil {
				return fmt.Errorf("orun task attach: %w", err)
			}
			task, taskErr := client.GetTask(cmd.Context(), org, key)
			sealNote := ""
			if taskErr == nil {
				sealNote = sealTaskLocally(cmd.Context(), task, doc)
			}
			if asJSON {
				return encodeJSON(cmd, seal)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "contract %s attached to %s\n", shortRevision(seal.ContractHash), key)
			if sealNote != "" {
				fmt.Fprintln(cmd.OutOrStdout(), sealNote)
			}
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "The workspace's tasks: key, id, title mirror, contract",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			list, err := client.ListTasks(cmd.Context(), client.Scope().OrgID)
			if err != nil {
				return fmt.Errorf("orun task list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, list)
			}
			rows := make([][]string, 0, len(list.Tasks))
			for _, t := range list.Tasks {
				rows = append(rows, []string{t.Key, t.ID, orDash(t.TitleMirror), orDash(shortRevision(t.ContractHash))})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"KEY", "ID", "TITLE", "CONTRACT"}, rows))
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskShowCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "show <key|tsk_id>",
		Short: "One task with its derived verdict — the rung and the evidence behind it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			org := client.Scope().OrgID
			task, err := client.GetTask(cmd.Context(), org, args[0])
			if err != nil {
				return fmt.Errorf("orun task show: %w", err)
			}
			verdict, err := client.GetTaskVerdict(cmd.Context(), org, args[0])
			if err != nil {
				return fmt.Errorf("orun task show: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, map[string]any{"task": task, "verdict": verdict})
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s  %s\n", task.Key, task.ID)
			if task.TitleMirror != "" {
				fmt.Fprintf(out, "title     %s\n", task.TitleMirror)
			}
			fmt.Fprintf(out, "rung      %s — %s\n", verdict.Verdict.Rung, verdict.Verdict.Evidence.Reason)
			if verdict.Verdict.Pin != nil {
				fmt.Fprintf(out, "pin       %s (active: %t)\n", verdict.Verdict.Pin.Rung, verdict.Verdict.Pin.Active)
			}
			if verdict.Verdict.Dissent != nil {
				fmt.Fprintf(out, "dissent   asserted %s\n", verdict.Verdict.Dissent.Asserted)
			}
			if verdict.Verdict.Blocked {
				fmt.Fprintf(out, "blocked   by %s\n", strings.Join(verdict.Verdict.BlockedBy, ", "))
			}
			fmt.Fprintf(out, "contract  %s\n", orDash(shortRevision(task.ContractHash)))
			for _, d := range verdict.Dependencies {
				fmt.Fprintf(out, "dep       %s (%s)\n", d.Ref, d.State)
			}
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newTaskCheckCommand() *cobra.Command {
	var (
		baseRef string
		headRef string
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "check <key>",
		Short: "Check the repo's contract document offline: validity, completeness, affects vs diff",
		Long: `Check a task's contract without the network — the authoring loop of
design §3.3. Validates tasks/<KEY>.TaskContract.yaml strictly, reports
completeness in the same terms the cloud derives readiness from, and with
--base compares the components the diff actually touched against the
contract's affects ceiling (the same change engine 'plan --changed' uses).

Advisory by construction: the workspace re-decides at enforcement, and
effective access is resolved policy ∩ contract — never wider than either.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskCheck(cmd, args[0], baseRef, headRef, asJSON)
		},
	}
	cmd.Flags().StringVar(&baseRef, "base", "", "base ref: also check the diff's components against affects")
	cmd.Flags().StringVar(&headRef, "head", "", "head ref for --base (default: working tree)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

type taskCheckReport struct {
	Document     string   `json:"document"`
	Key          string   `json:"key"`
	ContractHash string   `json:"contractHash"`
	Complete     bool     `json:"complete"`
	Missing      []string `json:"missing"`
	// Base/Outside are present only when --base ran the change engine.
	Base            string   `json:"base,omitempty"`
	DirectlyChanged []string `json:"directlyChanged,omitempty"`
	Outside         []string `json:"outsideAffects,omitempty"`
	Advisory        string   `json:"advisory"`
}

const taskCheckAdvisory = "advisory: offline check — the workspace decides at enforcement, and effective access is resolved policy ∩ contract (never wider than either)"

func runTaskCheck(cmd *cobra.Command, key, baseRef, headRef string, asJSON bool) error {
	out := cmd.OutOrStdout()
	root := taskDocRoot()
	doc, err := taskfile.FindForKey(root, key)
	if err != nil {
		return fmt.Errorf("orun task check: %w", err)
	}
	if doc == nil {
		fmt.Fprintf(out, "no contract document at %s\n", taskfile.PathFor(root, key))
		fmt.Fprintln(out, "no contract ⇒ no narrowing (the task, if it exists, constrains nothing)")
		return nil
	}
	hash, _, err := contract.ContractID(doc.Contract)
	if err != nil {
		return fmt.Errorf("orun task check: %w", err)
	}
	report := taskCheckReport{
		Document:     doc.Path,
		Key:          doc.Key,
		ContractHash: hash,
		Missing:      taskfile.Missing(doc.Contract),
		Advisory:     taskCheckAdvisory,
	}
	report.Complete = len(report.Missing) == 0

	if baseRef != "" {
		changed, derr := detectChangedComponents(cmd.Context(), baseRef, headRef)
		if derr != nil {
			return derr
		}
		report.Base = baseRef
		report.DirectlyChanged = changed
		allowed := map[string]bool{}
		for _, a := range doc.Contract.Affects {
			allowed[a] = true
		}
		for _, c := range changed {
			if !allowed[c] {
				report.Outside = append(report.Outside, c)
			}
		}
		sort.Strings(report.Outside)
	}

	if asJSON {
		if err := encodeJSON(cmd, report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "document  %s\n", report.Document)
		fmt.Fprintf(out, "sealed    %s\n", report.ContractHash)
		if report.Complete {
			fmt.Fprintln(out, "complete  yes")
		} else {
			fmt.Fprintf(out, "complete  no — missing %s\n", strings.Join(report.Missing, ", "))
		}
		if report.Base != "" {
			if len(report.Outside) == 0 {
				fmt.Fprintf(out, "affects   ok — %d changed component(s), all inside the contract\n", len(report.DirectlyChanged))
			} else {
				fmt.Fprintf(out, "affects   %d component(s) outside the contract: %s\n", len(report.Outside), strings.Join(report.Outside, ", "))
			}
		}
		fmt.Fprintln(out, taskCheckAdvisory)
	}
	if len(report.Outside) > 0 {
		return fmt.Errorf("orun task check: %d changed component(s) outside the contract's affects", len(report.Outside))
	}
	return nil
}

// detectChangedComponents runs the affected engine for the check verb and
// returns the DIRECTLY changed components — what the diff itself touched,
// which is what an affects ceiling is about (dependents rebuild, but the
// contract constrains what the work edits).
func detectChangedComponents(ctx context.Context, baseRef, headRef string) ([]string, error) {
	store, refs, _, err := openObjectModel()
	if err != nil {
		return nil, exitErr(3, "open object model: %w", err)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		if errors.Is(err, objcatalog.ErrNotFound) {
			return nil, exitErr(6, "no catalog found; run 'orun catalog refresh' or 'orun plan' first")
		}
		return nil, exitErr(3, "load catalog: %w", err)
	}
	if view.Ownership == nil {
		return nil, exitErr(6, "catalog has no impact index; run 'orun catalog refresh'")
	}
	opts := git.ChangeOptions{Base: baseRef, Head: headRef}
	if verr := git.ValidateOptions(opts); verr != nil {
		return nil, exitErr(1, "invalid change options: %w", verr)
	}
	res, err := affected.NewDetector(&view, affected.IntentImpact("watch")).
		Detect(ctx, affected.GitChangeSource{Options: opts, IntentPath: "intent.yaml"})
	if err != nil {
		return nil, exitErr(2, "change detection: %w", err)
	}
	return res.DirectlyChanged, nil
}

// attachDocumentIfPresent attaches the repo's contract document for a key,
// when one exists. (nil, "", nil) = no document, which is a legal state.
func attachDocumentIfPresent(ctx context.Context, client *remotestate.Client, org, key string) (*taskfile.Document, string, error) {
	doc, err := taskfile.FindForKey(taskDocRoot(), key)
	if err != nil || doc == nil {
		return nil, "", err
	}
	hash, wire, err := contract.ContractID(doc.Contract)
	if err != nil {
		return doc, "", err
	}
	if _, err := client.AttachTaskContract(ctx, org, key, wire, hash); err != nil {
		return doc, hash, err
	}
	return doc, hash, nil
}

// sealTaskLocally records the issuance (and contract, when present) in the
// local object store — refs/tasks/<key>, the store's first non-derivable
// root (TK-9). Best-effort by design: the cloud holds the identity; a
// missing local store degrades to a note, never a failed create.
func sealTaskLocally(ctx context.Context, task *remotestate.PublicTask, doc *taskfile.Document) string {
	store, refs, _, err := openObjectModel()
	if err != nil {
		return fmt.Sprintf("not recorded in the local object store (%v)", err)
	}
	in := taskobj.SealInput{Key: task.Key, TaskRef: task.ID, Title: task.TitleMirror}
	if doc != nil {
		in.Contract = doc.Contract
	}
	if _, _, err := taskobj.SealTask(ctx, store, refs, in); err != nil {
		return fmt.Sprintf("not recorded in the local object store (%v)", err)
	}
	return fmt.Sprintf("recorded as refs/tasks/%s in the local object store", task.Key)
}

// parseDeriveRef parses --derive's prefix#number form.
func parseDeriveRef(s string) (*remotestate.TaskDerive, error) {
	prefix, num, ok := strings.Cut(s, "#")
	if !ok || prefix == "" {
		return nil, fmt.Errorf("orun task create: --derive wants prefix#number (e.g. web#123), got %q", s)
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("orun task create: --derive issue number %q is not a positive integer", num)
	}
	return &remotestate.TaskDerive{RepoPrefix: prefix, IssueNumber: n}, nil
}
