package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// Action is a tier-1 built-in (design §4). Tier-2 OCI action packages (WA6)
// resolve into this same shape — the registry namespace is cut for them now.
// Every action validates its `with:` before invoke; there is no unvalidated
// dispatch branch (the gap WA2/WA3 exists to close stays closed here).
type Action struct {
	Name     string
	Validate func(with map[string]any) error
	Invoke   func(ctx context.Context, with map[string]any, conn map[string]string) (map[string]any, error)
}

var registry = map[string]*Action{
	"http.request": {
		Name:     "http.request",
		Validate: validateHTTPRequest,
		Invoke:   invokeHTTPRequest,
	},
	"script": {
		Name:     "script",
		Validate: validateScript,
		Invoke:   invokeScript,
	},
	"sleep": {
		Name:     "sleep",
		Validate: validateSleep,
		Invoke:   invokeSleep,
	},
}

// ResolveAction returns the named built-in or an error listing what exists.
func ResolveAction(name string) (*Action, error) {
	if a, ok := registry[name]; ok {
		return a, nil
	}
	known := make([]string, 0, len(registry))
	for k := range registry {
		known = append(known, k)
	}
	sort.Strings(known)
	return nil, fmt.Errorf("unknown action %q (built-ins: %s)", name, strings.Join(known, ", "))
}

// ── http.request ───────────────────────────────────────────────

func validateHTTPRequest(with map[string]any) error {
	url, _ := with["url"].(string)
	if url == "" {
		return fmt.Errorf("http.request: with.url is required")
	}
	if m, ok := with["method"]; ok {
		if _, isStr := m.(string); !isStr {
			return fmt.Errorf("http.request: with.method must be a string")
		}
	}
	return nil
}

func invokeHTTPRequest(ctx context.Context, with map[string]any, conn map[string]string) (map[string]any, error) {
	method, _ := with["method"].(string)
	if method == "" {
		method = http.MethodGet
	}
	url, _ := with["url"].(string)
	var body io.Reader
	if b, ok := with["body"].(string); ok && b != "" {
		body = strings.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), url, body)
	if err != nil {
		return nil, err
	}
	if hs, ok := with["headers"].(map[string]any); ok {
		for k, v := range hs {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}
	// http.bearer connection type: the token is injected here and only here —
	// mapped-only, never via ambient env (design §9).
	if tok := conn["bearerToken"]; tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"status": resp.StatusCode, "body": string(data)}
	var parsed any
	if json.Unmarshal(bytes.TrimSpace(data), &parsed) == nil {
		out["json"] = parsed
	}
	return out, nil
}

// ── script (goja) ──────────────────────────────────────────────
// The one place arbitrary computation is allowed (design §3). The interpreter
// is instantiated per invocation with no host bindings: pure compute over
// `input`, no I/O, no process, no clock beyond what CEL already allows.

func validateScript(with map[string]any) error {
	src, _ := with["source"].(string)
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("script: with.source is required")
	}
	return nil
}

func invokeScript(ctx context.Context, with map[string]any, _ map[string]string) (map[string]any, error) {
	vm := goja.New()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("step timeout")
		case <-done:
		}
	}()
	if err := vm.Set("input", with["input"]); err != nil {
		return nil, err
	}
	src, _ := with["source"].(string)
	v, err := vm.RunString(src)
	if err != nil {
		return nil, fmt.Errorf("script: %w", err)
	}
	return map[string]any{"value": v.Export()}, nil
}

// ── sleep ──────────────────────────────────────────────────────

func validateSleep(with map[string]any) error {
	d, _ := with["duration"].(string)
	if d == "" {
		return fmt.Errorf("sleep: with.duration is required")
	}
	if _, err := time.ParseDuration(d); err != nil {
		return fmt.Errorf("sleep: with.duration: %w", err)
	}
	return nil
}

func invokeSleep(ctx context.Context, with map[string]any, _ map[string]string) (map[string]any, error) {
	d, _ := time.ParseDuration(with["duration"].(string))
	select {
	case <-time.After(d):
		return map[string]any{"slept": d.String()}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
