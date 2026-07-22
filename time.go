package livetennisapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// Time is a timestamp from the API that never fails to decode.
//
// The API documents UTC ISO 8601 with a Z suffix, and date-only fields such as
// [Player.Birthday] as a plain calendar date. Both parse into this type. A
// value the parser does not recognise is kept verbatim in Raw instead of
// failing the surrounding response, because one odd timestamp is not a reason
// to lose an entire page of matches.
//
// The zero value means the field was null, absent, or unparseable, so a
// nullable timestamp needs no pointer:
//
//	if !match.ScheduledTime.IsZero() {
//		fmt.Println(match.ScheduledTime.Local())
//	}
type Time struct {
	time.Time

	// Raw is the exact string the API sent, preserved even when it parsed
	// cleanly. Empty when the field was null or absent.
	Raw string
}

// timeLayouts are tried in order. RFC 3339 is what the API documents; the rest
// cover a date-only field and the separator variants a JSON producer may emit.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// UnmarshalJSON implements [json.Unmarshaler]. It returns an error only for
// JSON that is not a string or null, never for an unrecognised time format.
func (t *Time) UnmarshalJSON(data []byte) error {
	*t = Time{}

	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}

	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		// Not a string. Keep the literal rather than failing the response.
		t.Raw = string(data)
		return nil
	}

	t.Raw = raw
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	return nil
}

// MarshalJSON implements [json.Marshaler], round-tripping the original string
// when there was one so re-encoding a response does not silently rewrite it.
func (t Time) MarshalJSON() ([]byte, error) {
	switch {
	case t.Raw != "":
		return json.Marshal(t.Raw)
	case !t.Time.IsZero():
		return json.Marshal(t.Time.UTC().Format(time.RFC3339Nano))
	default:
		return []byte("null"), nil
	}
}

// IsZero reports whether the field was null, absent, or unparseable.
func (t Time) IsZero() bool { return t.Time.IsZero() }

// String returns the original string the API sent, or the formatted time when
// the value was constructed locally.
func (t Time) String() string {
	if t.Raw != "" {
		return t.Raw
	}
	if t.Time.IsZero() {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}
