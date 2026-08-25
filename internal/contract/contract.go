// Package contract is the agent runtime's task contract — the goal, the
// blast-radius ceiling, the done-when list and the gates a run is briefed
// against (internal/agent/brief.go).
//
// It was carved out of internal/worklens at the work-plane teardown
// (orun-work-teardown WT2, invariant WT-1: the agent runtime does not
// regress). The contract is not work-plane furniture — it is the shape of
// what an agent is asked to do, and it outlives the tracker that used to
// mint it. Like internal/catalogmodel and the worklens it came from, this
// package imports no other internal/* package.
package contract

// Contract is the task contract: the spec-milestone convention as schema.
// All fields are optional individually; Complete derives readiness — the
// same predicate the brief assembler and the dispatcher both read.
type Contract struct {
	Goal       string   `json:"goal,omitempty"`
	Affects    []string `json:"affects,omitempty"`
	DoneWhen   []string `json:"doneWhen,omitempty"`
	Gates      []string `json:"gates,omitempty"`
	DesignRefs []string `json:"designRefs,omitempty"`
	Deps       []string `json:"deps,omitempty"`

	// Secrets and Envs are the enforcement fields (orun-tasks O1, design
	// §4): secret-key globs and environment names this work may resolve.
	// Under narrow-only enforcement a contract can only ever SHRINK what
	// policy already allows — effective = resolved_policy ∩ contract — so
	// absent means "no narrowing on that axis", never "allow everything".
	Secrets []string `json:"secrets,omitempty"`
	Envs    []string `json:"envs,omitempty"`

	// GatesDefined distinguishes an explicit empty gate set (merge alone
	// may finish the work) from gates simply never having been declared
	// (gates unknown — the work is not agent-ready).
	GatesDefined bool `json:"gatesDefined,omitempty"`
}

// Complete reports contract completeness: goal + ≥1 affects + ≥1 doneWhen +
// gates declared. Completeness is one definition of "actionable", shared by
// humans and agents.
func (c *Contract) Complete() bool {
	if c == nil {
		return false
	}
	gatesDeclared := c.GatesDefined || len(c.Gates) > 0
	return c.Goal != "" && len(c.Affects) > 0 && len(c.DoneWhen) > 0 && gatesDeclared
}
