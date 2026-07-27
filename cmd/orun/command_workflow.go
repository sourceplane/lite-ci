package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"path/filepath"

	"github.com/sourceplane/orun/internal/flow"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

var (
	workflowRunSet         []string
	workflowRunConnections []string
	workflowRunResume      string
)

// workflowCmd is the standalone authoring on-ramp for torkflow workflows
// (specs/orun-workflows WF6): validate / digest / run / view a workflow file
// directly, before dropping it into a `workflow:` plan step or blueprint hook.
// run and view front the pinned engine, sharing WF0's engine-resolution path.
var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Validate, digest, run, or view a torkflow workflow file",
	Long: `Author and debug torkflow workflows standalone, before wiring them into a
workflow: plan step or blueprint hook.

  orun workflow validate <file>   check the file parses as a torkflow workflow
  orun workflow digest   <file>   print the content digest orun would pin
  orun workflow run      <file>   run it through the pinned engine (ORUN_TORKFLOW_ENGINE)
  orun workflow view     <file>   render its DAG via the pinned engine`,
}

var workflowValidateCmd = &cobra.Command{
	Use:   "validate <file>",
	Short: "Check a workflow file parses as a torkflow workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowValidate(cmd, args[0])
	},
}

var workflowDigestCmd = &cobra.Command{
	Use:   "digest <file>",
	Short: "Print the content digest orun would pin for a workflow file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := flow.Load(args[0]); err != nil {
			return err
		}
		digest, err := flow.Digest(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), digest)
		return nil
	},
}

var workflowRunCmd = &cobra.Command{
	Use:   "run <file>",
	Short: "Run a workflow through the pinned engine",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowRun(cmd.Context(), cmd, args[0])
	},
}

var workflowEngineDigestCmd = &cobra.Command{
	Use:   "engine-digest",
	Short: "Print the resolved workflow engine's content digest (for intent execution.workflowEngine)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := workflowbackend.ResolveEngine(workflowbackend.EngineOptions{})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), eng.Digest())
		return nil
	},
}

var workflowViewCmd = &cobra.Command{
	Use:   "view <file>",
	Short: "Render a workflow's DAG via the pinned engine",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWorkflowView(cmd.Context(), cmd, args[0])
	},
}

func registerWorkflowCommand(root *cobra.Command) {
	root.AddCommand(workflowCmd)
	workflowCmd.AddCommand(workflowValidateCmd)
	workflowCmd.AddCommand(workflowDigestCmd)
	workflowCmd.AddCommand(workflowRunCmd)
	workflowCmd.AddCommand(workflowViewCmd)
	workflowCmd.AddCommand(workflowEngineDigestCmd)

	workflowRunCmd.Flags().StringArrayVar(&workflowRunSet, "set", nil, "Set a workflow input as key=value (repeatable)")
	workflowRunCmd.Flags().StringArrayVar(&workflowRunConnections, "connection", nil, "Grant a connection field as name.field=value (repeatable)")
	workflowRunCmd.Flags().StringVar(&workflowRunResume, "resume", "", "Resume a prior exec id, re-executing only non-succeeded steps")
}

// runWorkflowValidate is the real compile check (orun-workflows-v3 WA1): DAG
// shape, verb exclusivity, action resolution, CEL compilation, and declared-
// name reference checks. A file that validates here is a file the engine will
// accept — the parse-only era (and torkflow/v1) is over.
func runWorkflowValidate(cmd *cobra.Command, path string) error {
	if _, err := flow.Load(path); err != nil {
		return err
	}
	digest, err := flow.Digest(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ok: %s (%s)\n", path, digest)
	return nil
}

func runWorkflowRun(ctx context.Context, cmd *cobra.Command, path string) error {
	wf, err := flow.Load(path)
	if err != nil {
		return err
	}
	inputs, err := parseSetFlags(workflowRunSet)
	if err != nil {
		return err
	}
	conns, err := parseConnectionFlags(workflowRunConnections)
	if err != nil {
		return err
	}
	digest, err := flow.Digest(path)
	if err != nil {
		return err
	}
	res, err := flow.Run(ctx, wf, flow.RunOptions{
		Dir:         filepath.Dir(path),
		Inputs:      inputs,
		Connections: conns,
		ExecID:      workflowRunResume,
		Digest:      digest,
		Log:         cmd.OutOrStdout(),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "workflow %s: %s (exec %s)\n", path, res.Status, res.ExecID)
	for name, v := range res.Outputs {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s = %v\n", name, v)
	}
	if res.Status != "succeeded" {
		return fmt.Errorf("workflow failed")
	}
	return nil
}

// parseConnectionFlags turns --connection name.field=value grants into the
// engine's connection map. The flag IS the grant (design §9) — a standalone
// run has no calling step to carry one.
func parseConnectionFlags(pairs []string) (map[string]map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := map[string]map[string]string{}
	for _, p := range pairs {
		key, v, ok := strings.Cut(p, "=")
		name, field, ok2 := strings.Cut(key, ".")
		if !ok || !ok2 {
			return nil, fmt.Errorf("invalid --connection %q: expected name.field=value", p)
		}
		if out[name] == nil {
			out[name] = map[string]string{}
		}
		out[name][field] = v
	}
	return out, nil
}

// runWorkflowView renders the DAG in needs order — in-process, no engine.
func runWorkflowView(_ context.Context, cmd *cobra.Command, path string) error {
	wf, err := flow.Load(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "workflow %s (%d steps)\n", wf.Metadata.Name, len(wf.Steps))
	for _, st := range wf.Steps {
		needs := ""
		if len(st.Needs) > 0 {
			needs = " ← " + strings.Join(st.Needs, ", ")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s [%s]%s\n", st.Name, st.Verb(), needs)
	}
	return nil
}

// parseSetFlags turns repeated key=value flags into a Trigger inputs map.
func parseSetFlags(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --set %q: expected key=value", p)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}
