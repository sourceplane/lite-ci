package flow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// streamMu serializes streamed step-output lines across concurrently running
// steps so lines from parallel steps never interleave mid-line.
var streamMu sync.Mutex

// lineStream tees step output to the engine log line-by-line, prefixed with
// the step name, while the buffered capture keeps the full text for
// outputs/until evaluation. Without this, live progress was invisible — a
// preflight that legitimately polls for ten minutes read as a hang, and a
// failed step never showed WHY on the terminal.
type lineStream struct {
	dst     io.Writer
	prefix  string
	partial bytes.Buffer
}

func (l *lineStream) Write(p []byte) (int, error) {
	if l.dst == nil || l.dst == io.Discard {
		return len(p), nil
	}
	l.partial.Write(p)
	for {
		b := l.partial.Bytes()
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break
		}
		line := string(b[:i])
		l.partial.Next(i + 1)
		l.emit(line)
	}
	return len(p), nil
}

func (l *lineStream) emit(line string) {
	streamMu.Lock()
	fmt.Fprintf(l.dst, "    %s │ %s\n", l.prefix, line)
	streamMu.Unlock()
}

// flush emits any trailing partial line once the process has exited.
func (l *lineStream) flush() {
	if l.dst == nil || l.dst == io.Discard || l.partial.Len() == 0 {
		return
	}
	l.emit(l.partial.String())
	l.partial.Reset()
}

// runExec is the run: verb (design §6): argv only — never a shell — with an
// env allowlist over a fixed hygienic base, an explicit cwd defaulting to the
// step's run dir, and the exit/stdout/stderr triple as the step result. A `;`
// or `$( )` in an argument is a literal; anyone needing a shell writes
// ["bash", "-lc", ...] explicitly and owns that choice visibly in review.
// Output is captured for the step result AND streamed line-by-line to log.
func runExec(ctx context.Context, s *Step, vars map[string]any, runDir string, st *StepState, log io.Writer) error {
	argv := make([]string, len(s.Run))
	for i, a := range s.Run {
		v, err := Interpolate(a, vars)
		if err != nil {
			return fmt.Errorf("run[%d]: %w", i, err)
		}
		argv[i] = fmt.Sprintf("%v", v)
	}
	if argv[0] == "" {
		return fmt.Errorf("run: empty argv[0]")
	}

	cwd := runDir
	if s.Cwd != "" {
		v, err := Interpolate(s.Cwd, vars)
		if err != nil {
			return fmt.Errorf("cwd: %w", err)
		}
		cwd = fmt.Sprintf("%v", v)
		if !filepath.IsAbs(cwd) {
			abs, err := filepath.Abs(cwd)
			if err != nil {
				return err
			}
			cwd = abs
		}
	}

	// Hygienic base (WA0 decision): PATH/HOME/TMPDIR pass through — without
	// them no real tool runs — plus exactly the declared keys. Nothing else
	// is inherited; credentials travel via connection:, never ambient env.
	env := []string{}
	for _, k := range []string{"PATH", "HOME", "TMPDIR"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	// envInherit: named host variables passed through by explicit declaration
	// — the allowlist philosophy intact (visible in review, nothing ambient),
	// for channels that are process-inherited by nature (SSH_AUTH_SOCK et al).
	for _, k := range s.EnvInherit {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	for k, raw := range s.Env {
		v, err := Interpolate(raw, vars)
		if err != nil {
			return fmt.Errorf("env.%s: %w", k, err)
		}
		env = append(env, fmt.Sprintf("%s=%v", k, v))
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // declared argv, no shell (design §6)
	cmd.Dir = cwd
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	outStream := &lineStream{dst: log, prefix: s.Name}
	errStream := &lineStream{dst: log, prefix: s.Name}
	cmd.Stdout = io.MultiWriter(&limitWriter{w: &stdout}, outStream)
	cmd.Stderr = io.MultiWriter(&limitWriter{w: &stderr}, errStream)

	err := cmd.Run()
	outStream.flush()
	errStream.flush()
	st.Stdout, st.Stderr = stdout.String(), stderr.String()
	st.ExitCode = cmd.ProcessState.ExitCode()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("run: timed out (killed): %s", argv[0])
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return &ExitErr{Argv0: argv[0], Code: st.ExitCode, StderrTail: tailOf(st.Stderr)}
		}
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

// ExitErr is a run: step's non-zero exit — distinct from "could not execute"
// so poll: can treat it as an evaluable attempt (gh pr checks exits non-zero
// while checks are pending) rather than a terminal fault.
type ExitErr struct {
	Argv0      string
	Code       int
	StderrTail string
}

func (e *ExitErr) Error() string {
	return fmt.Sprintf("run: %s exited %d: %s", e.Argv0, e.Code, e.StderrTail)
}

// limitWriter caps captured output at 4 MiB per stream — a step result is a
// sealed run fact, not a log sink.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	const max = 4 << 20
	if l.n >= max {
		return len(p), nil
	}
	if l.n+len(p) > max {
		p = p[:max-l.n]
	}
	l.n += len(p)
	_, err := l.w.Write(p)
	return len(p), err
}

func tailOf(s string) string {
	const n = 400
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
