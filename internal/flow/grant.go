package flow

import (
	"fmt"
	"sort"
)

// ConnectionNames returns the workflow's declared connection names, sorted —
// the compile-time inspection surface the connections grant validates against
// (design §9: only names are intent).
func (w *Workflow) ConnectionNames() []string {
	names := make([]string, 0, len(w.Connections))
	for name := range w.Connections {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// OutputNames returns the workflow's declared output names, sorted.
func (w *Workflow) OutputNames() []string {
	names := make([]string, 0, len(w.Outputs))
	for name := range w.Outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateGrant enforces the connections grant (design §9) against a workflow's
// declared connections: every connection the workflow declares MUST be mapped,
// and every mapping MUST name a declared connection. The returned error prints
// the exact block to paste (S-8).
func ValidateGrant(where string, declared []string, granted map[string]map[string]string) error {
	grantedSet := map[string]struct{}{}
	for name := range granted {
		grantedSet[name] = struct{}{}
	}
	var missing []string
	declaredSet := map[string]struct{}{}
	for _, name := range declared {
		declaredSet[name] = struct{}{}
		if _, ok := grantedSet[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		msg := where + ": workflow declares connections that are not granted.\nAdd a connections: block mapping each to a secret reference, e.g.:\n\n  connections:"
		for _, name := range missing {
			msg += "\n    " + name + ":\n      token: secret://<workspace>/<project>/<env>/<KEY>"
		}
		return fmt.Errorf("%s", msg)
	}
	for name := range grantedSet {
		if _, ok := declaredSet[name]; !ok {
			return fmt.Errorf("%s: connections grant names %q, but the workflow declares no such connection (stale or misspelled grant)", where, name)
		}
	}
	return nil
}
