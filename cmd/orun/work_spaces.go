package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// orun work spaces / orun work epics (orun-work-spaces WK4). The Space is
// what remains of the initiative: a key namespace — prefix, title,
// advisory owner team — that carries NO status, health or dates; those
// live on the Epic (WK2), which is what `orun work epics` reads.

func newWorkSpacesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spaces",
		Short: "The namespaces: list | show | create | update",
		Long: `The work plane's Spaces — where keys are minted and epics are filed.
A Space carries no state: creating one starts no work, and retiring one
(the console's narrow DELETE) erases nothing. The prefix is the canonical
key and is never re-mintable once used (IS-C).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newWorkSpacesListCommand())
	cmd.AddCommand(newWorkSpacesShowCommand())
	cmd.AddCommand(newWorkSpacesCreateCommand())
	cmd.AddCommand(newWorkSpacesUpdateCommand())
	return cmd
}

func newWorkSpacesListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		archived   bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Every Space: prefix, title, owner team, epic count",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			spaces, err := client.ListSpaces(cmd.Context(), archived)
			if err != nil {
				return fmt.Errorf("orun work spaces list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, spaces)
			}
			if len(spaces.Spaces) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no spaces)")
				return nil
			}
			rows := make([][]string, 0, len(spaces.Spaces))
			for _, s := range spaces.Spaces {
				rows = append(rows, []string{s.Prefix, s.Title, s.OwnerTeamID, strconv.Itoa(s.EpicCount)})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"PREFIX", "TITLE", "OWNER TEAM", "EPICS"}, rows))
			return nil
		},
	}
	cmd.Flags().BoolVar(&archived, "archived", false, "list retired/archived Spaces")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newWorkSpacesShowCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "show <prefix>",
		Short: "One Space: the namespace record plus its epics as context rows",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			detail, err := client.GetSpace(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("orun work spaces show: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, detail)
			}
			s := detail.Space
			fmt.Fprintf(cmd.OutOrStdout(), "%s — %s\n", s.Prefix, s.Title)
			if s.Description != "" {
				fmt.Fprintln(cmd.OutOrStdout(), s.Description)
			}
			if s.OwnerTeamID != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "owner team: %s (advisory — no policy reads it)\n", s.OwnerTeamID)
			}
			if len(detail.Epics) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no epics filed here yet — `orun work epics` reads state)")
				return nil
			}
			rows := make([][]string, 0, len(detail.Epics))
			for _, e := range detail.Epics {
				rows = append(rows, []string{e.Key, e.Title, e.TargetDate})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"EPIC", "TITLE", "TARGET"}, rows))
			return nil
		},
	}
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newWorkSpacesCreateCommand() *cobra.Command {
	var (
		workspace   string
		backendURL  string
		asJSON      bool
		title       string
		prefix      string
		description string
		ownerTeam   string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Open a namespace (--title; prefix auto-suggests)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if title == "" {
				return fmt.Errorf("orun work spaces create: --title is required")
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			out, err := client.CreateSpace(cmd.Context(), remotestate.CreateWorkSpaceRequest{
				Title: title, Prefix: prefix, Description: description, OwnerTeamID: ownerTeam,
			})
			if err != nil {
				return fmt.Errorf("orun work spaces create: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created space %s — %s (seq %d)\n", out.Space.Prefix, out.Space.Title, out.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "Space title (required)")
	cmd.Flags().StringVar(&prefix, "prefix", "", "typed-key prefix (auto-suggested when omitted; WRK reserved)")
	cmd.Flags().StringVar(&description, "description", "", "the why — one honest paragraph")
	cmd.Flags().StringVar(&ownerTeam, "owner-team", "", "advisory owner team (team_…)")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newWorkSpacesUpdateCommand() *cobra.Command {
	var (
		workspace   string
		backendURL  string
		asJSON      bool
		title       string
		description string
		ownerTeam   string
		clearOwner  bool
	)
	cmd := &cobra.Command{
		Use:   "update <prefix>",
		Short: "Edit the namespace record: title, description, owner team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := remotestate.PatchWorkSpaceRequest{Title: title, Description: description}
			if clearOwner {
				empty := ""
				req.OwnerTeamID = &empty
			} else if ownerTeam != "" {
				req.OwnerTeamID = &ownerTeam
			}
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			out, err := client.PatchSpace(cmd.Context(), args[0], req)
			if err != nil {
				return fmt.Errorf("orun work spaces update: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "updated space %s (seq %d)\n", args[0], out.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description")
	cmd.Flags().StringVar(&ownerTeam, "owner-team", "", "advisory owner team (team_…)")
	cmd.Flags().BoolVar(&clearOwner, "clear-owner-team", false, "clear the advisory owner team")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}

func newWorkEpicsCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		space      string
		state      string
		health     string
		archived   bool
	)
	cmd := &cobra.Command{
		Use:   "epics",
		Short: "The Work home read: epic rows with their three truth sources",
		Long: `Every epic row names its three truth sources (WV-2): the authored state
(the stored five-state machine), the asserted health (the latest update's
headline, staleness derived at read), and the derived execution rollup
(total/complete/blocked — never editable). Filter by --space, --state,
--health; --archived reads the shelf.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := cloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			epics, err := client.ListEpics(cmd.Context(), remotestate.WorkEpicsOptions{
				Space: space, State: state, Health: health, Archived: archived,
			})
			if err != nil {
				return fmt.Errorf("orun work epics: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, epics)
			}
			if len(epics.Epics) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no epics)")
				return nil
			}
			rows := make([][]string, 0, len(epics.Epics))
			for _, e := range epics.Epics {
				spacePrefix := ""
				if e.Space != nil {
					spacePrefix = e.Space.Prefix
				}
				health := e.Health
				if health != "" && e.HealthStale {
					health += " · stale"
				}
				rows = append(rows, []string{
					e.Key, e.Title, spacePrefix, e.State, health,
					fmt.Sprintf("%d/%d", e.Execution.Complete, e.Execution.Total),
					e.TargetDate,
				})
			}
			fmt.Fprint(cmd.OutOrStdout(), renderColumns([]string{"EPIC", "TITLE", "SPACE", "STATE", "HEALTH", "DONE", "TARGET"}, rows))
			return nil
		},
	}
	cmd.Flags().StringVar(&space, "space", "", "filter to one Space prefix")
	cmd.Flags().StringVar(&state, "state", "", "planning | active | paused | completed | canceled")
	cmd.Flags().StringVar(&health, "health", "", "on_track | at_risk | off_track")
	cmd.Flags().BoolVar(&archived, "archived", false, "read the archived shelf")
	addCloudScopeFlags(cmd, &workspace, &backendURL, &asJSON)
	return cmd
}
