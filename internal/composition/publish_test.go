package composition

import "testing"

func TestParseGitRemote(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		repo  string
		ok    bool
	}{
		{"git@github.com:sourceplane/orun.git", "sourceplane", "orun", true},
		{"git@github.com:acme/aws-vpc.git", "acme", "aws-vpc", true},
		{"https://github.com/acme/aws-vpc.git", "acme", "aws-vpc", true},
		{"https://github.com/acme/aws-vpc", "acme", "aws-vpc", true},
		{"ssh://git@github.com/acme/aws-vpc.git", "acme", "aws-vpc", true},
		{"", "", "", false},
		{"not-a-url", "", "", false},
	}
	for _, tc := range cases {
		owner, repo, ok := parseGitRemote(tc.in)
		if ok != tc.ok || owner != tc.owner || repo != tc.repo {
			t.Errorf("parseGitRemote(%q) = (%q, %q, %v); want (%q, %q, %v)", tc.in, owner, repo, ok, tc.owner, tc.repo, tc.ok)
		}
	}
}

func TestSplitRefParts(t *testing.T) {
	cases := []struct {
		ref      string
		registry string
		repo     string
		tag      string
	}{
		{"ghcr.io/acme/aws-vpc:v1.0.0", "ghcr.io", "acme/aws-vpc", "v1.0.0"},
		{"ghcr.io/acme/aws-vpc", "ghcr.io", "acme/aws-vpc", ""},
		{"localhost:5000/acme/x:dev", "localhost:5000", "acme/x", "dev"},
		{"acme/aws-vpc:v1", "ghcr.io", "acme/aws-vpc", "v1"},
	}
	for _, tc := range cases {
		registry, repo, tag := splitRefParts(tc.ref)
		if registry != tc.registry || repo != tc.repo || tag != tc.tag {
			t.Errorf("splitRefParts(%q) = (%q, %q, %q); want (%q, %q, %q)", tc.ref, registry, repo, tag, tc.registry, tc.repo, tc.tag)
		}
	}
}

func TestIsRegistryOnly(t *testing.T) {
	cases := map[string]bool{
		"ghcr.io":              true,
		"ghcr":                 true,
		"docker.io":            true,
		"localhost":            true,
		"acme/aws-vpc":         false,
		"ghcr.io/acme/aws-vpc": false,
		"":                     false,
	}
	for in, want := range cases {
		if got := isRegistryOnly(in); got != want {
			t.Errorf("isRegistryOnly(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestSanitizeTag(t *testing.T) {
	cases := map[string]string{
		"v1.0.0":         "v1.0.0",
		"":               "latest",
		"feature/branch": "feature-branch",
		"a b c":          "a-b-c",
	}
	for in, want := range cases {
		if got := sanitizeTag(in); got != want {
			t.Errorf("sanitizeTag(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestNormalizeOCIRef(t *testing.T) {
	cases := map[string]string{
		"sourceplane/devops-compositions": "ghcr.io/sourceplane/devops-compositions:latest",
		"ghcr.io/acme/aws-vpc":            "ghcr.io/acme/aws-vpc:latest",
		"ghcr.io/acme/aws-vpc:v1":         "ghcr.io/acme/aws-vpc:v1",
		"oci://ghcr.io/acme/aws-vpc:v1":   "ghcr.io/acme/aws-vpc:v1",
		"localhost:5000/acme/x:dev":       "localhost:5000/acme/x:dev",
		"":                                "",
	}
	for in, want := range cases {
		if got := NormalizeOCIRef(in); got != want {
			t.Errorf("NormalizeOCIRef(%q) = %q; want %q", in, got, want)
		}
	}
}
