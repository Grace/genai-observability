package mapping

import (
	"encoding/json"
	"strings"
)

// Objections is a list of stated concerns that tolerates the shapes models
// actually return.
//
// Observed in a deployed run: the adversarial reviewer returned objections as an
// array of objects rather than of strings, so decoding failed with
// "cannot unmarshal object into agentJSON.objections.0 of type string". The
// assessment was recorded as UNKNOWN and the reviewer's actual finding - that
// the example descriptions may not reflect production - was discarded.
//
// The reviewer is the component whose entire purpose is to object. Silently
// dropping its objections because of a container type is the worst place in this
// system for a schema mismatch to hide.
type Objections []string

func (o *Objections) UnmarshalJSON(b []byte) error {
	// The common case: a list of strings.
	var strs []string
	if err := json.Unmarshal(b, &strs); err == nil {
		*o = strs
		return nil
	}
	// A single string instead of a list.
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		if one = strings.TrimSpace(one); one != "" {
			*o = Objections{one}
		}
		return nil
	}
	// A list of objects: keep the human-readable text rather than the struct.
	var items []map[string]any
	if err := json.Unmarshal(b, &items); err != nil {
		// Unrecognised shape. An empty objection list is wrong in a way that
		// matters, so report it rather than implying the reviewer was content.
		*o = Objections{"reviewer returned objections in an unrecognised shape"}
		return nil
	}
	out := make(Objections, 0, len(items))
	for _, item := range items {
		if text := objectionText(item); text != "" {
			out = append(out, text)
		}
	}
	*o = out
	return nil
}

// objectionText pulls the most likely prose field out of a structured objection,
// falling back to the whole object so nothing is lost.
func objectionText(item map[string]any) string {
	for _, key := range []string{"objection", "claim", "text", "description", "reason", "detail", "message", "issue"} {
		if v, ok := item[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	if raw, err := json.Marshal(item); err == nil {
		return string(raw)
	}
	return ""
}
