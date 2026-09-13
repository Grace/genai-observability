package mapping

import (
	"encoding/json"
	"testing"
)

// The shape the deployed reviewer actually returned, which failed the decode and
// caused its finding to be discarded.
func TestObjectionsAcceptsObjects(t *testing.T) {
	var a struct {
		Relationship Relationship `json:"relationship"`
		Confidence   Confidence   `json:"confidence"`
		Objections   Objections   `json:"objections"`
	}
	body := `{
	  "relationship":"UNKNOWN",
	  "confidence":0.7,
	  "objections":[
	    {"objection":"example descriptions may not reflect production","severity":"medium"},
	    {"claim":"no source documentation was provided"}
	  ]
	}`
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("structured objections must not fail the decode: %v", err)
	}
	if a.Relationship != Unknown {
		t.Errorf("relationship = %q, want UNKNOWN", a.Relationship)
	}
	if len(a.Objections) != 2 {
		t.Fatalf("objections = %v, want 2", a.Objections)
	}
	if a.Objections[0] != "example descriptions may not reflect production" {
		t.Errorf("objection[0] = %q", a.Objections[0])
	}
	if a.Objections[1] != "no source documentation was provided" {
		t.Errorf("objection[1] = %q", a.Objections[1])
	}
}

func TestObjectionsShapes(t *testing.T) {
	cases := map[string]int{
		`["a","b"]`:             2,
		`"just one"`:            1,
		`[]`:                    0,
		`[{"text":"x"}]`:        1,
		`[{"unknown_key":"y"}]`: 1, // falls back to the whole object
		`{"not":"a list"}`:      1, // unrecognised: reported, not silently empty
	}
	for in, want := range cases {
		var o Objections
		if err := o.UnmarshalJSON([]byte(in)); err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", in, err)
			continue
		}
		if len(o) != want {
			t.Errorf("UnmarshalJSON(%s) produced %d objections, want %d: %v", in, len(o), want, o)
		}
	}
}

// An unrecognised shape must never look like "the reviewer had no objections".
func TestUnrecognisedShapeIsNotSilence(t *testing.T) {
	var o Objections
	if err := o.UnmarshalJSON([]byte(`12345`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(o) == 0 {
		t.Error("an unparseable objections field must not read as no objections")
	}
}
