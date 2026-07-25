// Package scoperef implements the scope-reference grammar — the only
// value-shaped thing that may appear in intent, plans, or any content-addressed
// object. A reference carries no value and no identity; resolution happens
// elsewhere (the runner for secrets, expand for config).
//
//	<scheme>://[<workspace>/][<dim>:<value>/]*<KEY>[@<version>]
//
// This package MIRRORS orun-cloud's packages/contracts/src/scope-ref.ts, and
// both are driven from the same vector file (testdata/refs.vectors.json). A
// vector added on either side fails the other's CI until it is mirrored — that
// is the mechanism replacing two hand-synced regexes across two languages.
//
// # Why the positional tuple had to go
//
// The shipped grammar was a fixed 4-tuple with no room for a fifth dimension.
// References live in an immutable, content-addressed object graph, so adding a
// dimension positionally would rewrite every reference string ever published.
// Named segments grow additively. That, not aesthetics, is the argument.
//
// # Contextual omission
//
// An omitted dimension binds to the RUN CONTEXT at expand time — it is not a
// wildcard and never widens a lookup. One manifest line then works in every
// environment, which removes the need for interpolation inside references (the
// shipped interpolation silently stripped unknown {{…}} to empty, so the
// documented {{env}} form produced an invalid reference).
package scoperef

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scheme is the value plane a reference addresses.
type Scheme string

const (
	SchemeSecret Scheme = "secret"
	SchemeConfig Scheme = "config"
	SchemeFlag   Scheme = "flag"
)

var schemes = []Scheme{SchemeSecret, SchemeConfig, SchemeFlag}

// Dimension is a scope dimension.
type Dimension string

const (
	DimProject   Dimension = "project"
	DimEnv       Dimension = "env"
	DimComponent Dimension = "component"
)

// Dimensions is the canonical render order. A reference's string form must be
// stable, because references are content-addressed.
var Dimensions = []Dimension{DimProject, DimEnv, DimComponent}

// schemeDimensions is which dimensions each plane accepts. Flags take no
// component: a flag's natural targeting dimensions are runtime (user, tenant,
// region) and belong to the targeting layer, not the deploy lattice.
var schemeDimensions = map[Scheme][]Dimension{
	SchemeSecret: {DimProject, DimEnv, DimComponent},
	SchemeConfig: {DimProject, DimEnv, DimComponent},
	SchemeFlag:   {DimProject, DimEnv},
}

var (
	// keyRE mirrors config-worker's secretKey grammar. No ':', which is what
	// makes ':' safe as the dimension delimiter.
	keyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)
	// slugRE matches workspace and dimension values. No ':'.
	slugRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
	// versionRE matches the @<version> pin.
	versionRE = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)
)

// Ref is a parsed scope reference. An empty dimension means "bind to the run
// context" — NOT a wildcard, and never a widening.
type Ref struct {
	Scheme Scheme
	// Workspace is "" when contextual.
	Workspace string
	// Project is "" when contextual.
	Project string
	// Env is "" when contextual.
	Env string
	// Component is "" when contextual.
	Component string
	Key       string
	// Version pins a specific version; 0 means head-at-resolve-time.
	Version int
}

// ErrorClass is a stable parse-failure class, shared with the TypeScript
// implementation and asserted on by the shared vectors.
type ErrorClass string

const (
	ErrNotARef             ErrorClass = "not_a_ref"
	ErrUnknownScheme       ErrorClass = "unknown_scheme"
	ErrUnknownDimension    ErrorClass = "unknown_dimension"
	ErrDuplicateDimension  ErrorClass = "duplicate_dimension"
	ErrDimensionNotAllowed ErrorClass = "dimension_not_allowed"
	ErrBadKey              ErrorClass = "bad_key"
	ErrBadValue            ErrorClass = "bad_value"
	ErrBadVersion          ErrorClass = "bad_version"
	ErrBadShape            ErrorClass = "bad_shape"
)

// ParseError is a typed parse failure.
//
// Its message NEVER echoes the input: a failing value in a secret-shaped slot
// may itself be a pasted secret, and errors reach logs and CI output. Callers
// identify the offending slot by the key they already know.
type ParseError struct {
	Class   ErrorClass
	Message string
}

func (e *ParseError) Error() string { return e.Message }

func parseErr(class ErrorClass, format string, args ...any) error {
	return &ParseError{Class: class, Message: fmt.Sprintf(format, args...)}
}

// ClassOf returns the ErrorClass of a parse failure, or "" for any other error.
func ClassOf(err error) ErrorClass {
	var pe *ParseError
	if errors.As(err, &pe) {
		return pe.Class
	}
	return ""
}

// IsRef reports whether s is reference-SHAPED (has a known scheme prefix). It
// does not validate the body — use Parse for that.
//
// This is the leak-guard primitive: anything in a secretEnv slot that is not a
// reference is a literal, and a literal in a secret slot is a compile error.
func IsRef(s string) bool {
	for _, scheme := range schemes {
		if strings.HasPrefix(s, string(scheme)+"://") {
			return true
		}
	}
	return false
}

// IsSchemeRef reports whether s is a reference of one specific scheme.
func IsSchemeRef(s string, scheme Scheme) bool {
	return strings.HasPrefix(s, string(scheme)+"://")
}

// Parse parses and validates a scope reference.
func Parse(input string) (Ref, error) {
	sep := strings.Index(input, "://")
	if sep < 0 {
		return Ref{}, parseErr(ErrNotARef,
			"not a scope reference (want <scheme>://[<workspace>/][<dim>:<value>/]*<KEY>[@<version>])")
	}
	scheme := Scheme(input[:sep])
	if _, ok := schemeDimensions[scheme]; !ok {
		return Ref{}, parseErr(ErrUnknownScheme, "unknown scheme (want one of: secret, config, flag)")
	}

	body := input[sep+3:]
	if body == "" {
		return Ref{}, parseErr(ErrBadShape, "reference has no key")
	}

	version := 0
	if at := strings.LastIndex(body, "@"); at >= 0 {
		raw := body[at+1:]
		if !versionRE.MatchString(raw) {
			return Ref{}, parseErr(ErrBadVersion, "@<version> must be a positive integer")
		}
		v, err := strconv.Atoi(raw)
		if err != nil {
			return Ref{}, parseErr(ErrBadVersion, "@<version> must be a positive integer")
		}
		version = v
		body = body[:at]
	}

	segments := strings.Split(body, "/")
	key := segments[len(segments)-1]
	segments = segments[:len(segments)-1]
	if !keyRE.MatchString(key) {
		return Ref{}, parseErr(ErrBadKey, "invalid key segment")
	}

	ref := Ref{Scheme: scheme, Key: key, Version: version}

	var named, bare []string
	for _, s := range segments {
		if strings.Contains(s, ":") {
			named = append(named, s)
		} else {
			bare = append(bare, s)
		}
	}

	// The deprecated positional form: <workspace>/<project>/<env>/<KEY>.
	// Accepted as sugar and normalized on read, so already-published references
	// keep their meaning and their digest.
	if len(bare) == 3 && len(named) == 0 {
		if err := assignSlug(&ref.Workspace, "workspace", bare[0]); err != nil {
			return Ref{}, err
		}
		if err := assignDimension(&ref, scheme, DimProject, bare[1]); err != nil {
			return Ref{}, err
		}
		if err := assignDimension(&ref, scheme, DimEnv, bare[2]); err != nil {
			return Ref{}, err
		}
		return ref, nil
	}

	if len(bare) > 1 {
		return Ref{}, parseErr(ErrBadShape,
			"at most one bare segment (the workspace) may precede named dimensions")
	}
	if len(bare) == 1 {
		// A single leading bare segment is the workspace. It must come FIRST —
		// otherwise position carries meaning again, which is what we left behind.
		if len(segments) == 0 || segments[0] != bare[0] {
			return Ref{}, parseErr(ErrBadShape, "the workspace segment must come first")
		}
		if err := assignSlug(&ref.Workspace, "workspace", bare[0]); err != nil {
			return Ref{}, err
		}
	}

	seen := make(map[Dimension]struct{}, len(named))
	for _, segment := range named {
		idx := strings.Index(segment, ":")
		dim := Dimension(segment[:idx])
		value := segment[idx+1:]
		if !knownDimension(dim) {
			// Fail closed. Ignoring `enviroment:prod` would resolve at a BROADER
			// scope than the author meant.
			return Ref{}, parseErr(ErrUnknownDimension,
				"unknown dimension %q (want one of: project, env, component)", string(dim))
		}
		if _, dup := seen[dim]; dup {
			return Ref{}, parseErr(ErrDuplicateDimension, "dimension %q appears more than once", string(dim))
		}
		seen[dim] = struct{}{}
		if err := assignDimension(&ref, scheme, dim, value); err != nil {
			return Ref{}, err
		}
	}

	return ref, nil
}

func knownDimension(d Dimension) bool {
	for _, known := range Dimensions {
		if known == d {
			return true
		}
	}
	return false
}

func assignSlug(dst *string, name, value string) error {
	if !slugRE.MatchString(value) {
		return parseErr(ErrBadValue, "invalid %s segment", name)
	}
	*dst = value
	return nil
}

func assignDimension(ref *Ref, scheme Scheme, dim Dimension, value string) error {
	allowed := false
	for _, d := range schemeDimensions[scheme] {
		if d == dim {
			allowed = true
			break
		}
	}
	if !allowed {
		return parseErr(ErrDimensionNotAllowed,
			"dimension %q is not addressable on the %s plane", string(dim), string(scheme))
	}
	if !slugRE.MatchString(value) {
		return parseErr(ErrBadValue, "invalid %s segment", string(dim))
	}
	switch dim {
	case DimProject:
		ref.Project = value
	case DimEnv:
		ref.Env = value
	case DimComponent:
		ref.Component = value
	}
	return nil
}

// String renders the canonical form: workspace (if pinned) as a bare leading
// segment, then named dimensions in Dimensions order, then the key.
//
// Canonical ordering is load-bearing — references live in content-addressed
// plan objects, so two spellings of the same reference must produce one string.
func (r Ref) String() string {
	parts := make([]string, 0, 5)
	if r.Workspace != "" {
		parts = append(parts, r.Workspace)
	}
	for _, dim := range Dimensions {
		var value string
		switch dim {
		case DimProject:
			value = r.Project
		case DimEnv:
			value = r.Env
		case DimComponent:
			value = r.Component
		}
		if value != "" {
			parts = append(parts, string(dim)+":"+value)
		}
	}
	parts = append(parts, r.Key)
	out := string(r.Scheme) + "://" + strings.Join(parts, "/")
	if r.Version > 0 {
		out += "@" + strconv.Itoa(r.Version)
	}
	return out
}

// Canonicalize parses then re-renders — the normalization the deprecated
// positional form goes through on read.
func Canonicalize(input string) (string, error) {
	ref, err := Parse(input)
	if err != nil {
		return "", err
	}
	return ref.String(), nil
}

// IsFullyContextual reports whether a reference leaves every dimension to the
// run context.
func (r Ref) IsFullyContextual() bool {
	return r.Workspace == "" && r.Project == "" && r.Env == "" && r.Component == ""
}
