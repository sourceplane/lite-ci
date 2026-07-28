package cliauth

import "testing"

// The remote grammar half of ResolveGitHubRepoIdentity: every remote spelling
// git produces for github.com must parse to owner/name; anything else must be
// rejected (the identity is then omitted with a warning, never guessed).
func TestGitHubRemoteParsing(t *testing.T) {
	cases := []struct {
		remote string
		want   string // "owner/name" or "" for rejected
	}{
		{"git@github.com:sourceplane/nimbus.git", "sourceplane/nimbus"},
		{"git@github.com:sourceplane/nimbus", "sourceplane/nimbus"},
		{"https://github.com/sourceplane/nimbus.git", "sourceplane/nimbus"},
		{"https://github.com/sourceplane/nimbus", "sourceplane/nimbus"},
		{"ssh://git@github.com/sourceplane/nimbus.git", "sourceplane/nimbus"},
		{"https://gitlab.com/acme/thing.git", ""},
		{"/local/path/repo.git", ""},
	}
	for _, tc := range cases {
		m := githubRemoteRe.FindStringSubmatch(tc.remote)
		got := ""
		if m != nil {
			got = m[1] + "/" + m[2]
		}
		if got != tc.want {
			t.Errorf("remote %q: parsed %q, want %q", tc.remote, got, tc.want)
		}
	}
}
