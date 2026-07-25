package scoperef

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The vector file is a byte-identical copy of
// orun-cloud/packages/contracts/src/scope-ref-vectors.json. A vector added on
// either side fails the other's CI until it is mirrored — that is the mechanism
// that replaced two hand-synced regexes across two languages.
type vectorFile struct {
	Accept []struct {
		Why       string `json:"why"`
		In        string `json:"in"`
		Ref       Ref    `json:"ref"`
		Canonical string `json:"canonical"`
	} `json:"accept"`
	Reject []struct {
		Why   string `json:"why"`
		In    string `json:"in"`
		Class string `json:"class"`
	} `json:"reject"`
}

func loadVectors(t *testing.T) vectorFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "refs.vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vf.Accept) == 0 || len(vf.Reject) == 0 {
		t.Fatal("vector file is empty — the shared fixture did not load")
	}
	return vf
}

func TestVectorsAccept(t *testing.T) {
	for _, v := range loadVectors(t).Accept {
		t.Run(v.In, func(t *testing.T) {
			got, err := Parse(v.In)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", v.Why, err)
			}
			if got != v.Ref {
				t.Errorf("%s:\n parsed %+v\n want   %+v", v.Why, got, v.Ref)
			}
			if s := got.String(); s != v.Canonical {
				t.Errorf("%s: canonical = %q, want %q", v.Why, s, v.Canonical)
			}
			// Canonicalization must be idempotent — references are
			// content-addressed and must not drift on a re-render.
			again, err := Canonicalize(v.Canonical)
			if err != nil {
				t.Fatalf("re-canonicalize %q: %v", v.Canonical, err)
			}
			if again != v.Canonical {
				t.Errorf("canonicalize is not idempotent: %q -> %q", v.Canonical, again)
			}
		})
	}
}

func TestVectorsReject(t *testing.T) {
	for _, v := range loadVectors(t).Reject {
		t.Run(v.In, func(t *testing.T) {
			_, err := Parse(v.In)
			if err == nil {
				t.Fatalf("%s: expected a parse failure, got none", v.Why)
			}
			if class := string(ClassOf(err)); class != v.Class {
				t.Errorf("%s: class = %q, want %q", v.Why, class, v.Class)
			}
		})
	}
}

// A failing value in a secret-shaped slot may itself BE a pasted secret, and
// errors reach logs and CI output.
func TestParseErrorsNeverEchoTheInput(t *testing.T) {
	const secretish = "sk_live_51H8xQ2eZvKYlo2C0aBcDeF"
	for _, in := range []string{
		secretish,
		"secret://env:prod/" + secretish + ":trailing",
		"vault://" + secretish,
	} {
		if _, err := Parse(in); err == nil {
			t.Fatalf("expected %q to fail", in)
		} else if strings.Contains(err.Error(), "sk_live") {
			t.Errorf("error echoed the input: %q", err.Error())
		}
	}
}

func TestIsRef(t *testing.T) {
	for _, s := range []string{"secret://K", "config://K", "flag://K"} {
		if !IsRef(s) {
			t.Errorf("IsRef(%q) = false, want true", s)
		}
	}
	// The leak guard: a literal is not a reference, which is what makes a
	// literal in a secretEnv slot a compile error.
	for _, s := range []string{"hunter2", "postgres://u:p@h/db", ""} {
		if IsRef(s) {
			t.Errorf("IsRef(%q) = true, want false", s)
		}
	}
	if !IsSchemeRef("config://K", SchemeConfig) || IsSchemeRef("config://K", SchemeSecret) {
		t.Error("IsSchemeRef did not discriminate by scheme")
	}
}

func TestLegacyPositionalFormMatchesNamedForm(t *testing.T) {
	legacy, err := Parse("secret://acme/api/prod/DATABASE_URL")
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	named, err := Parse("secret://acme/project:api/env:prod/DATABASE_URL")
	if err != nil {
		t.Fatalf("named: %v", err)
	}
	if legacy != named {
		t.Errorf("legacy %+v != named %+v", legacy, named)
	}
}

func TestIsFullyContextual(t *testing.T) {
	ref, err := Parse("secret://DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if !ref.IsFullyContextual() {
		t.Error("want fully contextual")
	}
	pinned, err := Parse("secret://env:prod/DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if pinned.IsFullyContextual() {
		t.Error("a pinned dimension must not read as contextual")
	}
}

// The vector file is mirrored byte-identically from orun-cloud's
// packages/contracts/src/scope-ref-vectors.json. Neither repo can see the other
// in CI, so each pins the file's DIGEST: a vector added on one side changes the
// digest, this test fails, and the mirror is forced.
//
// To update: change the vectors, copy the file across, and update BOTH digests
// (this constant and the one in packages/contracts/src/scope-ref-vectors.sync.test.ts).
const vectorsSHA256 = "e590cd423cf07199e0a7ea440b038c66f4b3d60cb95861011b92b830c2ed596c"

func TestVectorFileIsMirroredFromContracts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "refs.vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != vectorsSHA256 {
		t.Errorf("vector digest = %s, want %s\n"+
			"The shared fixture drifted from orun-cloud/packages/contracts/src/scope-ref-vectors.json.\n"+
			"Copy the file across and update both pinned digests.", got, vectorsSHA256)
	}
}
