// Package secretref is the deprecated secret-only face of the scope-reference
// grammar. It now delegates entirely to [scoperef], which implements the shared
// grammar for all three value planes (secret, config, flag).
//
// Deprecated: use [github.com/sourceplane/orun/internal/scoperef] directly.
// Retained for one release so external callers and in-flight branches keep
// building; the types are aliases, so there is no conversion cost and nothing
// is lost — a Ref parsed here carries the Component dimension exactly as
// scoperef does.
//
//	secret://[<workspace>/][<dim>:<value>/]*<KEY>[@<version>]
package secretref

import (
	"github.com/sourceplane/orun/internal/scoperef"
)

// Scheme is the URI scheme prefix for secret references.
const Scheme = "secret://"

// Ref is a parsed secret reference.
//
// Deprecated: use [scoperef.Ref].
type Ref = scoperef.Ref

// IsRef reports whether s is secret-reference-shaped (has the scheme prefix).
// It does not validate the body — use Parse for that. This is the leak-guard
// primitive: anything in a secretEnv slot that is not IsRef is a literal.
//
// Deprecated: use [scoperef.IsSchemeRef] with [scoperef.SchemeSecret].
func IsRef(s string) bool {
	return scoperef.IsSchemeRef(s, scoperef.SchemeSecret)
}

// Parse parses and validates a secret:// reference.
//
// Error messages never echo the input: a failing value in a secret-shaped slot
// may BE a pasted secret, and errors end up in logs and CI output. Callers
// identify the offending slot by key (which they know), not by value.
//
// Deprecated: use [scoperef.Parse], which also accepts config:// and flag://.
func Parse(s string) (Ref, error) {
	ref, err := scoperef.Parse(s)
	if err != nil {
		return Ref{}, err
	}
	if ref.Scheme != scoperef.SchemeSecret {
		return Ref{}, &scoperef.ParseError{
			Class:   scoperef.ErrUnknownScheme,
			Message: "not a secret reference",
		}
	}
	return ref, nil
}
