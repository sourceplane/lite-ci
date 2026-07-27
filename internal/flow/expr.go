package flow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

// CEL is the expression contract (design §3): side-effect-free, terminating,
// compile-checked at validate time. goja exists only inside the `script`
// action — never as the ambient language of conditions and interpolation.

// stepDashRe lexically rewrites `steps.open-pr.` → `steps['open-pr'].` so the
// authoring ergonomics of dashed step names survive CEL's identifier grammar.
var stepDashRe = regexp.MustCompile(`\bsteps\.([a-z][a-z0-9]*(?:-[a-z0-9]+)+)\b`)

func rewriteExpr(src string) string {
	return stepDashRe.ReplaceAllString(src, `steps['$1']`)
}

var interpRe = regexp.MustCompile(`\{\{(.*?)\}\}`)

func celEnv() (*cel.Env, error) {
	return cel.NewEnv(
		ext.Strings(), // trim/split/replace etc. — string plumbing is table stakes
		cel.Variable("inputs", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("steps", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("connections", cel.MapType(cel.StringType, cel.DynType)),
		// Step-result scope (outputs:, poll.until): the just-finished attempt.
		cel.Variable("output", cel.DynType),
		cel.Variable("exitCode", cel.IntType),
		cel.Variable("stdout", cel.StringType),
		cel.Variable("stderr", cel.StringType),
		cel.Variable("attempt", cel.IntType),
	)
}

func compileExpr(env *cel.Env, src, where string) error {
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("%s: empty expression", where)
	}
	if _, iss := env.Compile(rewriteExpr(src)); iss != nil && iss.Err() != nil {
		return fmt.Errorf("%s: %v", where, iss.Err())
	}
	return nil
}

// EvalExpr evaluates a CEL expression against the given activation.
func EvalExpr(src string, vars map[string]any) (any, error) {
	env, err := celEnv()
	if err != nil {
		return nil, err
	}
	ast, iss := env.Compile(rewriteExpr(src))
	if iss != nil && iss.Err() != nil {
		return nil, iss.Err()
	}
	prg, err := env.Program(ast)
	if err != nil {
		return nil, err
	}
	base := map[string]any{
		"inputs": map[string]any{}, "steps": map[string]any{},
		"connections": map[string]any{}, "output": map[string]any{},
		"exitCode": 0, "stdout": "", "stderr": "", "attempt": 0,
	}
	for k, v := range vars {
		base[k] = v
	}
	out, _, err := prg.Eval(base)
	if err != nil {
		return nil, err
	}
	return out.Value(), nil
}

// Interpolate renders {{ expr }} segments inside a string. A string that is
// exactly one segment keeps the expression's native type.
func Interpolate(s string, vars map[string]any) (any, error) {
	matches := interpRe.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(s) {
		return EvalExpr(s[matches[0][2]:matches[0][3]], vars)
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		b.WriteString(s[last:m[0]])
		v, err := EvalExpr(s[m[2]:m[3]], vars)
		if err != nil {
			return nil, err
		}
		b.WriteString(fmt.Sprintf("%v", v))
		last = m[1]
	}
	b.WriteString(s[last:])
	return b.String(), nil
}

// reference scanning: every steps.X / steps['X'] / inputs.N / connections.N
// mention in an expression must resolve to a declared name. A lexical scan is
// the WA1 implementation of the spec's compile-check; the grammar of the
// scanned tokens is narrow enough that false positives are contrived.
var (
	refStepDot     = regexp.MustCompile(`\bsteps\.([a-zA-Z0-9_-]+)`)
	refStepBracket = regexp.MustCompile(`\bsteps\['([^']+)'\]`)
	refInput       = regexp.MustCompile(`\binputs\.([a-zA-Z0-9_]+)`)
	refConnection  = regexp.MustCompile(`\bconnections\.([a-zA-Z0-9_]+)`)
)

func (w *Workflow) checkRefs(src, where string, visibleSteps map[string]bool) error {
	for _, m := range refStepDot.FindAllStringSubmatch(src, -1) {
		if !visibleSteps[m[1]] {
			return fmt.Errorf("%s: references steps.%s, which is not a declared upstream step", where, m[1])
		}
	}
	for _, m := range refStepBracket.FindAllStringSubmatch(src, -1) {
		if !visibleSteps[m[1]] {
			return fmt.Errorf("%s: references steps['%s'], which is not a declared upstream step", where, m[1])
		}
	}
	for _, m := range refInput.FindAllStringSubmatch(src, -1) {
		if _, ok := w.Inputs[m[1]]; !ok {
			return fmt.Errorf("%s: references undeclared inputs.%s", where, m[1])
		}
	}
	for _, m := range refConnection.FindAllStringSubmatch(src, -1) {
		if _, ok := w.Connections[m[1]]; !ok {
			return fmt.Errorf("%s: references undeclared connections.%s", where, m[1])
		}
	}
	return nil
}

func (w *Workflow) validateExpressions() error {
	env, err := celEnv()
	if err != nil {
		return err
	}

	// Transitive upstream visibility: steps.X is referenceable only where X is
	// an ancestor via needs — reading a parallel branch's outputs is a race by
	// construction and is rejected at compile time.
	upstream := map[string]map[string]bool{}
	var expand func(name string) map[string]bool
	byName := map[string]*Step{}
	for i := range w.Steps {
		byName[w.Steps[i].Name] = &w.Steps[i]
	}
	expand = func(name string) map[string]bool {
		if v, ok := upstream[name]; ok {
			return v
		}
		vis := map[string]bool{}
		for _, n := range byName[name].Needs {
			vis[n] = true
			for a := range expand(n) {
				vis[a] = true
			}
		}
		upstream[name] = vis
		return vis
	}

	checkString := func(s, where string, vis map[string]bool) error {
		for _, m := range interpRe.FindAllStringSubmatch(s, -1) {
			if err := compileExpr(env, m[1], where); err != nil {
				return err
			}
			if err := w.checkRefs(m[1], where, vis); err != nil {
				return err
			}
		}
		return nil
	}
	var checkAny func(v any, where string, vis map[string]bool) error
	checkAny = func(v any, where string, vis map[string]bool) error {
		switch t := v.(type) {
		case string:
			return checkString(t, where, vis)
		case map[string]any:
			for k, vv := range t {
				if err := checkAny(vv, where+"."+k, vis); err != nil {
					return err
				}
			}
		case []any:
			for i, vv := range t {
				if err := checkAny(vv, fmt.Sprintf("%s[%d]", where, i), vis); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for i := range w.Steps {
		s := &w.Steps[i]
		vis := expand(s.Name)
		where := "step " + s.Name

		if s.If != "" {
			if err := compileExpr(env, s.If, where+": if"); err != nil {
				return err
			}
			if err := w.checkRefs(s.If, where+": if", vis); err != nil {
				return err
			}
		}
		if s.Poll != nil {
			if err := compileExpr(env, s.Poll.Until, where+": poll.until"); err != nil {
				return err
			}
			if err := w.checkRefs(s.Poll.Until, where+": poll.until", vis); err != nil {
				return err
			}
		}
		for name, expr := range s.Outputs {
			if err := compileExpr(env, expr, where+": outputs."+name); err != nil {
				return err
			}
			if err := w.checkRefs(expr, where+": outputs."+name, vis); err != nil {
				return err
			}
		}
		for j, a := range s.Run {
			if err := checkString(a, fmt.Sprintf("%s: run[%d]", where, j), vis); err != nil {
				return err
			}
		}
		for k, v := range s.Env {
			if err := checkString(v, where+": env."+k, vis); err != nil {
				return err
			}
		}
		if err := checkString(s.Cwd, where+": cwd", vis); err != nil {
			return err
		}
		if err := checkAny(map[string]any(s.With), where+": with", vis); err != nil {
			return err
		}
	}

	// Workflow-level outputs see every step.
	all := map[string]bool{}
	for _, s := range w.Steps {
		all[s.Name] = true
	}
	for name, expr := range w.Outputs {
		if err := compileExpr(env, expr, "outputs."+name); err != nil {
			return err
		}
		if err := w.checkRefs(expr, "outputs."+name, all); err != nil {
			return err
		}
	}
	return nil
}
