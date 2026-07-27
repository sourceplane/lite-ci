package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/integrationscli"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// `orun integrations <provider> secret create <KEY>` — integration-namespaced
// secret authoring (saas-secrets-platform SP5, ownership-model.md Surface 3).
//
// The ownership boundary: `orun secrets` views/manages ALL secrets and creates
// static (human) ones; AUTHORING an integration-bound secret lives under the
// integration's own namespace, mirroring the console. Providers are dynamic —
// the CLI never carries a provider or template catalog. Everything authoring-
// shaped (which providers, which templates, which modes, which delivery
// targets) is derived at runtime from the org's bulk capability read
// (GET …/integrations/secrets-capabilities, SP-A1/SP-A7); validation errors
// list what IS declared so the capability read doubles as the help surface.
//
// Invariant: value-less. No secret value is ever read, sent, or printed here —
// the server mints from the connected parent (at resolve for brokered; once +
// on schedule for rotated).

var (
	integrationsTemplateFlag string
	integrationsModeFlag     string
	integrationsParamFlags   []string
	integrationsBaseFlag     string
	integrationsNameFlag     string
	integrationsDescFlag     string
)

// integrationsResources / integrationsVerbs are the STATIC halves of the
// grammar (the provider positional is dynamic). Kept as data so the typo
// suggester and the usage error speak from one list.
var (
	integrationsResources     = []string{"secret", "templates", "status"}
	integrationsVerbs         = []string{"create"}
	integrationsTemplateVerbs = []string{"list", "create", "retire", "reactivate"}
)

const integrationsUsageLine = `orun integrations list
  orun integrations <provider> status
  orun integrations <provider> secret create <KEY> --connection <int_…> --template <id> [--mode brokered|rotated]
  orun integrations <provider> templates list
  orun integrations <provider> templates create <ID> --base <id> --name <s> [--description <s>]
  orun integrations <provider> templates retire|reactivate <ID>`

func registerIntegrationsCommand(root *cobra.Command) {
	integrationsCmd, state := newIntegrationsCommand()
	root.AddCommand(integrationsCmd)
	// The dynamic layer (orun-integrations-cli ICL1): when a cached registry
	// exists for the resolved workspace, provider verb trees mount as real
	// subcommands. Guarded so non-integrations invocations pay nothing.
	maybeMountDynamicIntegrations(integrationsCmd, state)
}

func newIntegrationsCommand() (*cobra.Command, *integrationsDynamicState) {
	state := &integrationsDynamicState{}
	integrationsCmd := &cobra.Command{
		Use:   "integrations [list | <provider> <resource> …]",
		Short: "Integration connections, scope templates, and integration-owned secret authoring",
		Long: `Author integration-bound secrets in the owning integration's namespace
(saas-secrets-platform SP5). The value is never entered: it is minted from the
provider connection — just-in-time at resolve (--mode brokered) or once + on a
schedule (--mode rotated).

Providers, scope templates, modes, and delivery targets are declared by each
integration and read from the platform at runtime; the CLI carries no catalog.
When a provider, template, mode, or target does not validate, the error lists
what the org's integrations actually declare.

Viewing and lifecycle stay on the substrate: use ` + "`orun secrets list/rotate/\nreveal/revoke/versions`" + ` for any secret, of any type.

Examples:
  orun integrations list
  orun integrations cloudflare status
  orun integrations cloudflare templates list
  orun integrations cloudflare templates create deploy-prod --base workers-deploy --name "Deploy prod workers"
  orun integrations cloudflare secret create CF_DEPLOY_TOKEN \
    --connection int_0123… --template workers-deploy --env prod
  orun integrations cloudflare secret create CF_API_TOKEN \
    --connection int_0123… --template workers-deploy --mode rotated \
    --rotation 30d --grace-seconds 3600 --deliver-target cloudflare-worker --env prod`,
		// The provider is a positional (providers are unknown offline), so the
		// static grammar underneath it is parsed by hand: a RunE with free args
		// keeps `orun integrations <anything>` inside this one command while
		// still failing typos loudly with suggestions (SP-A7). With a cached
		// registry mounted (ICL1), known providers dispatch to their rendered
		// subtrees before this RunE ever sees them; what reaches here is then
		// either the listing (no args) or an unknown/dormant provider.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				if state.cache != nil {
					fmt.Print(integrationscli.RenderProviderListing(state.cache.Registry))
					if state.stale {
						fmt.Fprintln(os.Stderr, integrationsStaleNote)
					}
					return nil
				}
				err := cmd.Help()
				fmt.Fprintln(os.Stderr, "\n"+integrationsSyncHint)
				return err
			}
			// `list` sits at the top level (no provider): every connection the
			// workspace can consume, with status. Handled BEFORE the cached-
			// registry branch — with provider subtrees mounted, anything else
			// reaching this RunE is an unknown provider, but `list` is ours.
			if args[0] == "list" {
				if len(args) > 1 {
					return fmt.Errorf("unexpected argument %q after \"list\"\n\nusage:\n  %s", args[1], integrationsUsageLine)
				}
				return runIntegrationsList(cmd)
			}
			if state.cache != nil {
				return unknownIntegrationProvider(state.cache, args[0])
			}
			provider := strings.TrimSpace(args[0])
			if provider == "" {
				return fmt.Errorf("usage:\n  %s", integrationsUsageLine)
			}
			if len(args) < 2 {
				return fmt.Errorf("missing resource after provider %q\n\nusage:\n  %s", provider, integrationsUsageLine)
			}
			switch args[1] {
			case "secret":
				key, err := parseIntegrationsSecretArgs(args)
				if err != nil {
					return err
				}
				return runIntegrationsSecretCreate(cmd, provider, key)
			case "templates":
				return runIntegrationsTemplates(cmd, provider, args[2:])
			case "status":
				if len(args) > 2 {
					return fmt.Errorf("unexpected argument %q after \"status\"\n\nusage:\n  %s", args[2], integrationsUsageLine)
				}
				return runIntegrationsStatus(cmd, provider)
			default:
				return unknownIntegrationsWord("resource", args[1], integrationsResources)
			}
		},
	}
	integrationsCmd.PersistentFlags().StringVar(&secretsBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	integrationsCmd.PersistentFlags().StringVar(&secretsOrgFlag, "org", "", "Workspace slug/id override for scope resolution (defaults to the linked workspace)")
	addSecretsScopeFlags(integrationsCmd)
	addSecretsJSONFlag(integrationsCmd)
	integrationsCmd.Flags().StringVar(&secretsConnection, "connection", "", "Integration connection public id (int_…) the value is minted against (required)")
	integrationsCmd.Flags().StringVar(&integrationsTemplateFlag, "template", "", "Scope template id declared by the provider (required; errors list the declared templates)")
	integrationsCmd.Flags().StringVar(&integrationsModeFlag, "mode", "brokered", "Secret mode: brokered (minted at resolve, never stored) or rotated (stored + re-minted on schedule)")
	integrationsCmd.Flags().StringArrayVar(&integrationsParamFlags, "param", nil, "Template param as key=value (repeatable; the template declares which are required)")
	integrationsCmd.Flags().StringVar(&secretsRotation, "rotation", "", "Rotation cadence for a rotated secret (e.g. 30d)")
	integrationsCmd.Flags().IntVar(&secretsGraceSeconds, "grace-seconds", 0, "Overlap seconds the prior token stays valid after a rotation (rotated mode; default: server 24h)")
	integrationsCmd.Flags().StringVar(&secretsDeliverTarget, "deliver-target", "", "Materialize target re-delivered on rotation for a long-lived consumer (rotated mode)")
	integrationsCmd.Flags().StringVar(&secretsDisplayName, "display-name", "", "Human display name for the key")
	integrationsCmd.Flags().StringVar(&integrationsBaseFlag, "base", "", "Declared base template a custom template derives from (templates create; required)")
	integrationsCmd.Flags().StringVar(&integrationsNameFlag, "name", "", "Display name for a custom template (templates create; required)")
	integrationsCmd.Flags().StringVar(&integrationsDescFlag, "description", "", "Description for a custom template (templates create)")
	integrationsCmd.AddCommand(newIntegrationsSyncCommand())
	return integrationsCmd, state
}

// newTemplatesExtensionCommand mounts the scope-template tree on a provider's
// DYNAMIC subtree (ICL3 extension), so the verbs exist whether or not a cached
// registry rendered the provider as a real subcommand. Flags are local — a
// mounted subcommand does not inherit the parent's non-persistent flags.
func newTemplatesExtensionCommand(provider string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates <list|create <ID> --base <id> --name <s>|retire <ID>|reactivate <ID>>",
		Short: "Scope templates: the declared catalog plus org-curated derivations (SP4)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			return runIntegrationsTemplates(c, provider, args)
		},
	}
	cmd.Flags().StringVar(&integrationsBaseFlag, "base", "", "Declared base template the custom template derives from (create; required)")
	cmd.Flags().StringVar(&integrationsNameFlag, "name", "", "Display name for the custom template (create; required)")
	cmd.Flags().StringVar(&integrationsDescFlag, "description", "", "Description for the custom template (create)")
	addSecretsJSONFlag(cmd)
	return cmd
}

// newStatusExtensionCommand mounts the provider status view on the dynamic
// subtree — the provider's connections plus a template-catalog summary.
func newStatusExtensionCommand(provider string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Connections and template summary for this provider",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runIntegrationsStatus(c, provider)
		},
	}
	addSecretsJSONFlag(cmd)
	return cmd
}

// runIntegrationsList renders every connection the workspace can consume
// (owned + inherited account-shared), with status — the CLI twin of the
// console's Integrations page.
func runIntegrationsList(cmd *cobra.Command) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	connections, err := rt.client.ListConnections(ctx, rt.org)
	if err != nil {
		return err
	}
	if secretsJSONOut {
		return emitJSON(connections)
	}
	if len(connections) == 0 {
		fmt.Println("No integrations connected. Connect one in the console (Integrations → Connect).")
		return nil
	}
	sort.Slice(connections, func(i, j int) bool {
		if connections[i].Provider != connections[j].Provider {
			return connections[i].Provider < connections[j].Provider
		}
		return connections[i].ID < connections[j].ID
	})
	headers := []string{"PROVIDER", "CONNECTION", "ACCOUNT", "STATUS", "SHARING", "CONNECTED"}
	rows := make([][]string, 0, len(connections))
	for _, c := range connections {
		sharing := c.Scope
		if c.Inherited {
			sharing += " (inherited)"
		}
		connected := "-"
		if c.ConnectedAt != nil && *c.ConnectedAt != "" {
			connected = formatAge(*c.ConnectedAt)
		}
		rows = append(rows, []string{
			c.Provider,
			c.ID,
			c.AccountLabel(),
			c.Status,
			orDash(sharing),
			connected,
		})
	}
	fmt.Print(renderColumns(headers, rows))
	return nil
}

// runIntegrationsStatus renders one provider's connections plus its template
// catalog summary — "can this provider mint, and from which connection".
func runIntegrationsStatus(cmd *cobra.Command, provider string) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	connections, err := rt.client.ListConnections(ctx, rt.org)
	if err != nil {
		return err
	}
	mine := make([]configsurface.Connection, 0, len(connections))
	for _, c := range connections {
		if c.Provider == provider {
			mine = append(mine, c)
		}
	}
	templates, tplErr := rt.client.ListScopeTemplates(ctx, rt.org, provider)
	if secretsJSONOut {
		return emitJSON(map[string]any{
			"provider":    provider,
			"connections": mine,
			"templates":   templates,
		})
	}
	if len(mine) == 0 {
		known := make(map[string]struct{})
		for _, c := range connections {
			known[c.Provider] = struct{}{}
		}
		names := make([]string, 0, len(known))
		for p := range known {
			names = append(names, p)
		}
		sort.Strings(names)
		if len(names) > 0 {
			return fmt.Errorf("no %s connection in this workspace (connected providers: %s)", provider, strings.Join(names, ", "))
		}
		return fmt.Errorf("no %s connection in this workspace", provider)
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	for _, c := range mine {
		mark := ui.Green(color, "●")
		if c.Status != "active" {
			mark = ui.Red(color, "●")
		}
		fmt.Printf("%s %s — %s · %s · %s\n", mark, c.ID, c.AccountLabel(), c.Status, orDash(c.Scope))
	}
	if tplErr == nil {
		active, custom := 0, 0
		for _, t := range templates {
			if t.Active() {
				active++
			}
			if t.Origin == "custom" {
				custom++
			}
		}
		fmt.Printf("\n%d scope template(s) (%d active, %d custom) — `orun integrations %s templates list`\n", len(templates), active, custom, provider)
	}
	return nil
}

// runIntegrationsTemplates dispatches the `templates` verb tree: the manage
// view of the provider's scope-template catalog (declared + org customs) and
// the org-curated authoring verbs (SP4 — create derives from a declared base;
// retire is soft, reactivate undoes it).
func runIntegrationsTemplates(cmd *cobra.Command, provider string, rest []string) error {
	if len(rest) == 0 {
		return fmt.Errorf("missing verb after \"templates\"\n\nusage:\n  %s", integrationsUsageLine)
	}
	verb := rest[0]
	// The whole grammar gate runs BEFORE auth/network so a typo never
	// round-trips (the secrets tree's fail-fast dialect, SP-A7).
	switch verb {
	case "list", "create", "retire", "reactivate":
	default:
		return unknownIntegrationsWord("verb", verb, integrationsTemplateVerbs)
	}
	if verb != "list" && len(rest) < 2 {
		return fmt.Errorf("missing <ID>\n\nusage:\n  %s", integrationsUsageLine)
	}
	if verb == "create" && (strings.TrimSpace(integrationsBaseFlag) == "" || strings.TrimSpace(integrationsNameFlag) == "") {
		return fmt.Errorf("templates create requires --base <declared template id> and --name <display name>")
	}
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	switch verb {
	case "list":
		if len(rest) > 1 {
			return fmt.Errorf("unexpected argument %q after \"list\"\n\nusage:\n  %s", rest[1], integrationsUsageLine)
		}
		templates, err := rt.client.ListScopeTemplates(ctx, rt.org, provider)
		if err != nil {
			return err
		}
		if secretsJSONOut {
			return emitJSON(templates)
		}
		if len(templates) == 0 {
			fmt.Printf("Provider %s declares no scope templates.\n", provider)
			return nil
		}
		headers := []string{"ID", "NAME", "ORIGIN", "STATUS", "PARAMS", "BASE"}
		rows := make([][]string, 0, len(templates))
		for _, t := range templates {
			origin := t.Origin
			if origin == "" {
				origin = "declared"
			}
			status := t.Status
			if status == "" {
				status = "active"
			}
			rows = append(rows, []string{
				t.ID,
				t.DisplayName,
				origin,
				status,
				orDash(strings.Join(t.Params, ",")),
				orDash(t.BaseTemplate),
			})
		}
		fmt.Print(renderColumns(headers, rows))
		return nil
	case "create":
		tpl, err := rt.client.CreateScopeTemplate(ctx, rt.org, provider, configsurface.CreateScopeTemplateRequest{
			TemplateID:   rest[1],
			BaseTemplate: integrationsBaseFlag,
			DisplayName:  integrationsNameFlag,
			Description:  integrationsDescFlag,
		})
		if err != nil {
			return err
		}
		if secretsJSONOut {
			return emitJSON(tpl)
		}
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Printf("%s created template %s (from %s)\n", ui.Green(color, "✓"), tpl.ID, tpl.BaseTemplate)
		return nil
	case "retire", "reactivate":
		status := "retired"
		if verb == "reactivate" {
			status = "active"
		}
		tpl, err := rt.client.UpdateScopeTemplate(ctx, rt.org, provider, rest[1], configsurface.UpdateScopeTemplateRequest{Status: status})
		if err != nil {
			return err
		}
		if secretsJSONOut {
			return emitJSON(tpl)
		}
		color := ui.ColorEnabledForWriter(os.Stdout)
		fmt.Printf("%s %s template %s\n", ui.Green(color, "✓"), verb+"d", tpl.ID)
		return nil
	default:
		return unknownIntegrationsWord("verb", verb, integrationsTemplateVerbs)
	}
}

// parseIntegrationsSecretArgs parses the positional grammar
// `<provider> secret create <KEY>` (the dispatcher has already consumed the
// provider and matched "secret"). The static halves fail loudly with a
// "did you mean" suggestion, extending the secrets tree's typo UX (SP-A7).
func parseIntegrationsSecretArgs(args []string) (key string, err error) {
	if len(args) < 3 {
		return "", fmt.Errorf("missing verb after %q\n\nusage:\n  %s", "secret", integrationsUsageLine)
	}
	if args[2] != "create" {
		return "", unknownIntegrationsWord("verb", args[2], integrationsVerbs)
	}
	if len(args) < 4 {
		return "", fmt.Errorf("missing <KEY>\n\nusage:\n  %s", integrationsUsageLine)
	}
	if len(args) > 4 {
		return "", fmt.Errorf("unexpected argument %q after the key\n\nusage:\n  %s", args[4], integrationsUsageLine)
	}
	return args[3], nil
}

// unknownIntegrationsWord is the typo error for the static grammar words,
// speaking the same "did you mean" dialect as unknownSecretsSubcommand.
func unknownIntegrationsWord(kind, got string, valid []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown %s %q for \"orun integrations\"", kind, got)
	if suggestion := ui.SuggestMatch(got, valid); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  %s", suggestion)
	}
	fmt.Fprintf(&b, "\n\nusage:\n  %s", integrationsUsageLine)
	return fmt.Errorf("%s", b.String())
}

// parseTemplateParams parses the repeatable --param key=value flags into the
// wire map. Purely syntactic — which params are required/accepted is the
// template's declaration, checked in validateAgainstCapability.
func parseTemplateParams(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	params := make(map[string]string, len(raw))
	for _, kv := range raw {
		i := strings.Index(kv, "=")
		if i <= 0 {
			return nil, fmt.Errorf("--param must be key=value, got %q", kv)
		}
		key := strings.TrimSpace(kv[:i])
		if key == "" {
			return nil, fmt.Errorf("--param must be key=value, got %q", kv)
		}
		if _, dup := params[key]; dup {
			return nil, fmt.Errorf("--param %q supplied more than once", key)
		}
		params[key] = kv[i+1:]
	}
	return params, nil
}

// integrationsCreatePreflight collects the local (no-network) flag failures
// so the caller sees the whole gate before auth or the capability read runs.
func integrationsCreatePreflight(key, connection, template, mode string, graceSeconds int, deliverTarget string) error {
	if !secretKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid key %q: keys must match ^[A-Za-z][A-Za-z0-9._-]{0,127}$", key)
	}
	if strings.TrimSpace(connection) == "" {
		return fmt.Errorf("missing --connection <int_…>: the connection the value is minted against (find it on the provider's integration page)")
	}
	if !strings.HasPrefix(connection, "int_") {
		return fmt.Errorf("--connection must be an integration connection public id (int_…)")
	}
	if strings.TrimSpace(template) == "" {
		return fmt.Errorf("missing --template <id>: the provider's scope template to mint against (the create error lists declared templates, or see the provider's integration page)")
	}
	if mode != "brokered" && mode != "rotated" {
		return fmt.Errorf("--mode must be brokered or rotated, got %q", mode)
	}
	if graceSeconds < 0 {
		return fmt.Errorf("--grace-seconds must be non-negative")
	}
	if mode != "rotated" {
		if graceSeconds > 0 {
			return fmt.Errorf("--grace-seconds applies to --mode rotated only (a brokered value is minted per-resolve and never stored)")
		}
		if strings.TrimSpace(deliverTarget) != "" {
			return fmt.Errorf("--deliver-target applies to --mode rotated only (a brokered value is minted per-resolve and never materialized)")
		}
	}
	return nil
}

// findSecretsCapability resolves the provider positional against the org's
// declared secret sources. Unknown providers fail with the declared list and
// a "did you mean" — the capability read IS the help surface (SP-A7).
func findSecretsCapability(caps []configsurface.SecretsCapability, provider string) (*configsurface.SecretsCapability, error) {
	names := make([]string, 0, len(caps))
	for i := range caps {
		if caps[i].Provider == provider {
			return &caps[i], nil
		}
		names = append(names, caps[i].Provider)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "provider %q is not a declared secret source for this workspace", provider)
	if suggestion := ui.SuggestMatch(provider, names); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  orun integrations %s secret create …", suggestion)
	}
	if len(names) > 0 {
		fmt.Fprintf(&b, "\n\ndeclared secret sources: %s", strings.Join(names, ", "))
	} else {
		b.WriteString("\n\nno integration declares a secrets capability yet; connect one in the console's integration hub")
	}
	return nil, fmt.Errorf("%s", b.String())
}

// validateAgainstCapability checks one create request against the provider's
// declared capability: mode ∈ supportedModes, template exists and is active
// (SP-A6 soft-retire), the template's declared params are all supplied (and
// nothing undeclared is), and a rotated deliver target is a declared delivery
// target. Every rejection lists what IS declared. Returns the resolved
// template so the caller can echo its identity. Pure — no I/O.
func validateAgainstCapability(cap *configsurface.SecretsCapability, mode, template string, params map[string]string, deliverTarget string) (*configsurface.ScopeTemplate, error) {
	if !containsString(cap.SupportedModes, mode) {
		return nil, fmt.Errorf("provider %q does not support --mode %s; supported modes: %s",
			cap.Provider, mode, strings.Join(cap.SupportedModes, ", "))
	}

	var tpl *configsurface.ScopeTemplate
	active := make([]string, 0, len(cap.ScopeTemplates))
	for i := range cap.ScopeTemplates {
		t := &cap.ScopeTemplates[i]
		if t.Active() {
			active = append(active, t.ID)
		}
		if t.ID == template {
			tpl = t
		}
	}
	sort.Strings(active)
	if tpl != nil && !tpl.Active() {
		return nil, fmt.Errorf("template %q is retired and cannot back a new secret (existing bindings keep resolving); %s templates: %s",
			template, cap.Provider, strings.Join(active, ", "))
	}
	if tpl == nil {
		var b strings.Builder
		fmt.Fprintf(&b, "template %q is not declared by provider %q", template, cap.Provider)
		if suggestion := ui.SuggestMatch(template, active); suggestion != "" {
			fmt.Fprintf(&b, "\n\ndid you mean:\n  --template %s", suggestion)
		}
		if len(active) > 0 {
			fmt.Fprintf(&b, "\n\n%s templates: %s", cap.Provider, strings.Join(active, ", "))
		}
		return nil, fmt.Errorf("%s", b.String())
	}

	var missing []string
	for _, name := range tpl.Params {
		if _, ok := params[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("template %q requires params: %s; pass each as --param <key>=<value> (missing: %s)",
			tpl.ID, strings.Join(tpl.Params, ", "), strings.Join(missing, ", "))
	}
	var unknown []string
	for name := range params {
		if !containsString(tpl.Params, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		accepted := "none"
		if len(tpl.Params) > 0 {
			accepted = strings.Join(tpl.Params, ", ")
		}
		return nil, fmt.Errorf("template %q does not declare param(s): %s; accepted params: %s",
			tpl.ID, strings.Join(unknown, ", "), accepted)
	}

	if mode == "rotated" && deliverTarget != "" && !containsString(cap.DeliveryTargets, deliverTarget) {
		if len(cap.DeliveryTargets) == 0 {
			return nil, fmt.Errorf("provider %q declares no delivery targets (per-run consumers only); drop --deliver-target", cap.Provider)
		}
		return nil, fmt.Errorf("deliver target %q is not declared by provider %q; declared targets: %s",
			deliverTarget, cap.Provider, strings.Join(cap.DeliveryTargets, ", "))
	}
	return tpl, nil
}

func runIntegrationsSecretCreate(cmd *cobra.Command, provider, key string) error {
	mode := strings.TrimSpace(integrationsModeFlag)
	if err := integrationsCreatePreflight(key, secretsConnection, integrationsTemplateFlag, mode, secretsGraceSeconds, secretsDeliverTarget); err != nil {
		return err
	}
	params, err := parseTemplateParams(integrationsParamFlags)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, false)
	if err != nil {
		return err
	}

	// Capability-driven validation (SP-A7): the provider's declaration is the
	// single source of truth for templates/modes/targets — nothing is assumed.
	caps, err := rt.client.ListSecretsCapabilities(ctx, rt.org)
	if err != nil {
		return err
	}
	capability, err := findSecretsCapability(caps, provider)
	if err != nil {
		return err
	}
	tpl, err := validateAgainstCapability(capability, mode, integrationsTemplateFlag, params, secretsDeliverTarget)
	if err != nil {
		return err
	}

	req := configsurface.CreateSecretRequest{
		SecretKey:      key,
		DisplayName:    secretsDisplayName,
		RotationPolicy: secretsRotation,
	}
	switch mode {
	case "brokered":
		req.Binding = &configsurface.SecretBrokerBinding{
			ConnectionID: secretsConnection,
			Template:     tpl.ID,
			Params:       params,
		}
	case "rotated":
		rotation := &configsurface.SecretRotationBinding{
			ConnectionID:  secretsConnection,
			Template:      tpl.ID,
			Params:        params,
			DeliverTarget: secretsDeliverTarget,
		}
		if secretsGraceSeconds > 0 {
			rotation.GraceSeconds = &secretsGraceSeconds
		}
		req.Rotation = rotation
	}

	meta, err := rt.client.CreateSecret(ctx, scope, req)
	if err != nil {
		return renderSecretsWriteError(err, key)
	}
	if secretsJSONOut {
		return emitJSON(meta)
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	detail := fmt.Sprintf("%s %s via %s, %s", provider, mode, tpl.ID, label)
	if meta != nil && meta.Version > 0 {
		detail += fmt.Sprintf(", version %d", meta.Version)
	}
	fmt.Printf("%s created %s (%s)\n", ui.Green(color, "✓"), key, detail)
	return nil
}

// ── --from-broker deprecation (SP-A7) ────────────────────────────────────────

// replacementSpec captures one legacy `secrets set --from-broker` invocation
// so the deprecation notice can print its EXACT namespaced substitute.
type replacementSpec struct {
	Key           string
	Provider      string
	Template      string
	Connection    string
	Rotation      string
	GraceSeconds  int
	DeliverTarget string
	DisplayName   string
	Env           string
	Project       bool
	Workspace     bool
}

// buildReplacementCommand renders the `orun integrations …` command line that
// replaces a deprecated --from-broker invocation, carrying over exactly the
// flags the caller actually passed. Pure — no I/O.
func buildReplacementCommand(spec replacementSpec) string {
	parts := []string{
		"orun", "integrations", spec.Provider, "secret", "create", spec.Key,
		"--connection", spec.Connection,
		"--template", spec.Template,
		"--mode", "rotated",
	}
	if spec.Rotation != "" {
		parts = append(parts, "--rotation", shellQuote(spec.Rotation))
	}
	if spec.GraceSeconds > 0 {
		parts = append(parts, "--grace-seconds", strconv.Itoa(spec.GraceSeconds))
	}
	if spec.DeliverTarget != "" {
		parts = append(parts, "--deliver-target", shellQuote(spec.DeliverTarget))
	}
	if spec.DisplayName != "" {
		parts = append(parts, "--display-name", shellQuote(spec.DisplayName))
	}
	switch {
	case strings.TrimSpace(spec.Env) != "":
		parts = append(parts, "--env", spec.Env)
	case spec.Project:
		parts = append(parts, "--project")
	case spec.Workspace:
		parts = append(parts, "--workspace")
	}
	return strings.Join(parts, " ")
}

// shellQuote single-quotes a value when it would not survive a shell unquoted,
// so the printed replacement command is copy-paste runnable.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// containsString reports whether v is an element of s.
func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// fromBrokerDeprecationNotice is the SP-A7 deprecation line printed (stderr)
// when `secrets set --from-broker` is used: the exact namespaced replacement
// for the caller's invocation. Pure — no I/O.
func fromBrokerDeprecationNotice(spec replacementSpec) string {
	return fmt.Sprintf("deprecated: --from-broker moves to the integration namespace; use '%s'", buildReplacementCommand(spec))
}
