package main

// `orun git-credential` — the git credential helper a grounded session
// authenticates with (orun-grounded-sessions GS1).
//
// Git invokes a helper as `<helper> get|store|erase`, writes the request as
// key=value lines on stdin, and reads the answer the same way. Here `get`
// mints a short-lived, repository-scoped token from the session's own door;
// `store` is deliberately inert (nothing durable is ever written); `erase`
// drops the cache.
//
// Not registered on the root command's help: this is plumbing git calls, not a
// verb a person types.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/agent/ground"
	"github.com/spf13/cobra"
)

func registerGitCredentialCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:    "git-credential <get|store|erase>",
		Short:  "Git credential helper for grounded agent sessions (internal)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runGitCredential(c, args[0])
		},
	}
	root.AddCommand(cmd)
}

func runGitCredential(cmd *cobra.Command, op string) error {
	switch op {
	case "store":
		// Never persist what git offers: the only credential this session may
		// hold is one it minted itself, and it already cached that.
		_, _ = io.Copy(io.Discard, cmd.InOrStdin())
		return nil
	case "erase":
		_, _ = io.Copy(io.Discard, cmd.InOrStdin())
		return ground.ClearCachedToken(ground.CachePath(os.Getenv))
	case "get":
		// handled below
	default:
		// Unknown operations are not errors in git's protocol — a helper that
		// does not understand an operation stays silent.
		return nil
	}

	req := parseCredentialRequest(cmd.InOrStdin())
	session, err := ground.SessionFromEnv(os.Getenv)
	if err != nil {
		// Not a grounded session: print nothing so git falls through to its
		// other helpers instead of failing on our account.
		return nil
	}
	// Serve ONLY the repository this session is bound to. The door enforces the
	// same thing server-side; refusing here too means a hostile repo cannot
	// even provoke the request.
	if !servesRequest(req, session.FullName) {
		return nil
	}

	cachePath := ground.CachePath(os.Getenv)
	now := time.Now()
	token, ok := ground.ReadCachedToken(cachePath, now)
	if !ok {
		minted, mErr := ground.MintRepoToken(cmd.Context(), nil, session)
		if mErr != nil {
			// Silence is the honest answer: git reports an ordinary auth
			// failure. The reason goes to stderr, never stdout — stdout is the
			// credential protocol.
			fmt.Fprintf(cmd.ErrOrStderr(), "orun git-credential: %v\n", mErr)
			return nil
		}
		token = minted
		if wErr := ground.WriteCachedToken(cachePath, token); wErr != nil {
			// A working credential in hand beats a cache; the next call mints.
			fmt.Fprintf(cmd.ErrOrStderr(), "orun git-credential: cache write failed: %v\n", wErr)
		}
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "username=x-access-token")
	fmt.Fprintln(out, "password="+token.Token)
	return nil
}

// credentialRequest is the subset of git's credential description this helper
// decides on.
type credentialRequest struct {
	protocol string
	host     string
	path     string
}

func parseCredentialRequest(r io.Reader) credentialRequest {
	var req credentialRequest
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			break // a blank line terminates the request
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "protocol":
			req.protocol = value
		case "host":
			req.host = value
		case "path":
			req.path = value
		}
	}
	return req
}

// servesRequest reports whether the credential request is for the session's
// bound repository over https. A path is not always present (git omits it
// unless credential.useHttpPath is set), so a host match is sufficient — the
// door is the authority on scope, and it only ever mints for the binding.
func servesRequest(req credentialRequest, fullName string) bool {
	if fullName == "" {
		return false
	}
	if req.protocol != "" && req.protocol != "https" {
		return false
	}
	if !strings.EqualFold(req.host, "github.com") {
		return false
	}
	if req.path == "" {
		return true
	}
	want := strings.ToLower(strings.TrimSuffix(fullName, ".git"))
	got := strings.ToLower(strings.TrimSuffix(strings.Trim(req.path, "/"), ".git"))
	return got == want
}
