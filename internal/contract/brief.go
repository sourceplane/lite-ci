package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// The sealed brief — content addressing as the system of proof. A Brief is
// the frozen statement of intent an agent implements against: the spec it
// belongs to and the task contracts, and nothing that a fold could move
// under it. Identity is content: canonical JSON, sha256.
//
// Carved out of internal/worklens at WT2 with its seal intact. What the
// teardown removed was the plane that minted briefs from a hosted tracker;
// what stayed is the artifact and the proof that it has not changed.

// BriefSpec is the grouping envelope a brief was frozen from.
type BriefSpec struct {
	Key    string `json:"key"`
	Title  string `json:"title,omitempty"`
	DocRef string `json:"docRef,omitempty"`
}

// BriefTask is one task envelope inside a brief: the key the run is
// dispatched against and the contract it is briefed with.
type BriefTask struct {
	Key      string    `json:"key"`
	Title    string    `json:"title,omitempty"`
	Contract *Contract `json:"contract,omitempty"`
}

// Brief is a sealed snapshot as the agent runtime reads it. It decodes
// leniently — unknown fields are ignored, because the id is computed over
// the bytes on disk, never over this struct's re-encoding (a lossy decode
// can therefore never fabricate a matching id).
type Brief struct {
	Spec     BriefSpec   `json:"spec"`
	Tasks    []BriefTask `json:"tasks"`
	CoordSeq int64       `json:"coordSeq"`
	ObsSeq   int64       `json:"obsSeq"`
}

// Task returns the task envelope with the given key.
func (b *Brief) Task(key string) (BriefTask, bool) {
	if b == nil {
		return BriefTask{}, false
	}
	for _, t := range b.Tasks {
		if t.Key == key {
			return t, true
		}
	}
	return BriefTask{}, false
}

// DecodeBrief parses sealed brief bytes and returns the brief together with
// the content id of its canonical form — the id a dispatcher pins.
func DecodeBrief(raw []byte) (*Brief, string, error) {
	var b Brief
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, "", fmt.Errorf("contract: decode sealed brief: %w", err)
	}
	id, err := IDForBytes(raw)
	if err != nil {
		return nil, "", err
	}
	return &b, id, nil
}

// VerifySealedBytes checks that bytes hash to the claimed content id and
// carries no fold output. Content addressing needs no second canonicalizer,
// only the hash.
func VerifySealedBytes(id string, raw []byte) error {
	got, err := IDForBytes(raw)
	if err != nil {
		return err
	}
	if got != id {
		return fmt.Errorf("contract: sealed bytes hash to %s, not the claimed %s", got, id)
	}
	return nil
}

// IDForBytes returns the content id of an already-encoded document, hashing
// its canonical form so formatting can never change identity. It also
// refuses a brief that smuggles derived state into the proof plane.
func IDForBytes(raw []byte) (string, error) {
	var tree interface{}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return "", fmt.Errorf("contract: decode for canonicalization: %w", err)
	}
	canonical, err := appendCanonical(make([]byte, 0, len(raw)), tree)
	if err != nil {
		return "", err
	}
	for _, k := range derivedStateKeys {
		if containsToken(canonical, k) {
			return "", fmt.Errorf("contract: sealed brief carries derived state (%s) — a brief is intent only", k)
		}
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// ContentID returns "sha256:<hex>" over the canonical bytes of v, plus
// those bytes — the same object has the same id on every machine.
func ContentID(v interface{}) (string, []byte, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), b, nil
}

// CanonicalJSON encodes v deterministically: lexicographically sorted keys,
// no insignificant whitespace, UTF-8 — the same logical content yields the
// same bytes on every machine.
func CanonicalJSON(v interface{}) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("contract: canonical encode: %w", err)
	}
	var tree interface{}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("contract: canonical decode: %w", err)
	}
	return appendCanonical(make([]byte, 0, len(raw)), tree)
}

func appendCanonical(out []byte, v interface{}) ([]byte, error) {
	switch t := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = append(out, '{')
		for i, k := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			out = append(out, kb...)
			out = append(out, ':')
			out, err = appendCanonical(out, t[k])
			if err != nil {
				return nil, err
			}
		}
		return append(out, '}'), nil
	case []interface{}:
		out = append(out, '[')
		for i, e := range t {
			if i > 0 {
				out = append(out, ',')
			}
			var err error
			out, err = appendCanonical(out, e)
			if err != nil {
				return nil, err
			}
		}
		return append(out, ']'), nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil, err
		}
		return append(out, b...), nil
	}
}

// derivedStateKeys are the fold-output names a sealed brief may never carry:
// the seal refuses any payload that smuggles moving state into the proof
// plane. The type system prevents it for our own shapes; this guards
// hand-authored and imported briefs.
var derivedStateKeys = []string{"\"rung\"", "\"lifecycle\"", "\"assignees\"", "\"pinned\""}

func containsToken(b []byte, token string) bool {
	if len(token) == 0 || len(b) < len(token) {
		return false
	}
	for i := 0; i+len(token) <= len(b); i++ {
		if string(b[i:i+len(token)]) == token {
			return true
		}
	}
	return false
}
