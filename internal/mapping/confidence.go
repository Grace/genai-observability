package mapping

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Confidence is a 0..1 score that also accepts the qualitative labels models
// return in practice ("HIGH", "medium", "0.8").
//
// Observed in a deployed run: the adversarial reviewer returned
// "confidence": "HIGH" where a number was requested. Decoding the whole
// assessment failed, the agent was recorded as UNKNOWN with "unparseable model
// response", and the adversarial check silently stopped working. The aggregation
// policy handled that correctly by demanding human review, which is exactly why
// it went unnoticed: the system stayed safe while one of its agents was dead.
//
// A qualitative label maps below the auto-approval bar on purpose. "HIGH" is a
// mood, not a calibrated probability, and must not be able to approve a mapping
// on its own.
type Confidence float64

const (
	ConfidenceHigh   Confidence = 0.75
	ConfidenceMedium Confidence = 0.50
	ConfidenceLow    Confidence = 0.25
)

func (c *Confidence) UnmarshalJSON(b []byte) error {
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*c = Confidence(f)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// Neither number nor string: no stated confidence, rather than
		// discarding an otherwise usable assessment.
		*c = 0
		return nil
	}
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "HIGH", "VERY HIGH", "CERTAIN":
		*c = ConfidenceHigh
	case "MEDIUM", "MED", "MODERATE":
		*c = ConfidenceMedium
	case "LOW", "VERY LOW", "UNCERTAIN":
		*c = ConfidenceLow
	default:
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			*c = Confidence(f)
			return nil
		}
		*c = 0
	}
	return nil
}

// Float returns the score for arithmetic in the aggregation policy.
func (c Confidence) Float() float64 { return float64(c) }
