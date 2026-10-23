package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// CapKind says how a cap element is written.
type CapKind int

const (
	CapFixed   CapKind = iota // a percentage, 0–100
	CapPace                   // "pace+N": the pace line plus a margin
	CapDefault                // "default": the global default_cap
)

// Pace margins accepted in a config file (percentage points).
const (
	MinPaceMargin = 5
	MaxPaceMargin = 50
)

// CapElem is one position of a choice's cap array. N is the percent for a
// fixed cap, the margin for a pace cap, and 0 for the default cap.
type CapElem struct {
	Kind CapKind
	N    float64
}

const capShape = `must be a number, "pace+N" or "default"`

// ParseCapElem decodes one cap element. The error text is the fragment that
// follows the field name in a ConfigError.
func ParseCapElem(raw json.RawMessage) (CapElem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return CapElem{}, errors.New(capShape)
		}
		return parseCapWord(s)
	}
	if string(raw) == "null" {
		return CapElem{}, errors.New("must be a number")
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return CapElem{}, errors.New(capShape)
	}
	if v < 0 || v > 100 {
		return CapElem{}, errors.New("must be between 0 and 100")
	}
	return CapElem{CapFixed, v}, nil
}

func parseCapWord(s string) (CapElem, error) {
	if s == "default" {
		return CapElem{Kind: CapDefault}, nil
	}
	rest, ok := strings.CutPrefix(s, "pace+")
	if !ok {
		return CapElem{}, errors.New(capShape)
	}
	n, err := strconv.ParseFloat(rest, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return CapElem{}, errors.New(capShape)
	}
	if n != math.Trunc(n) {
		return CapElem{}, fmt.Errorf("%q: N must be a whole number", s)
	}
	if n < MinPaceMargin || n > MaxPaceMargin {
		return CapElem{}, fmt.Errorf("%q: N must be %d–%d", s, MinPaceMargin, MaxPaceMargin)
	}
	return CapElem{CapPace, n}, nil
}

// String renders the element as written in the file: "90", "pace+10", "default".
func (e CapElem) String() string {
	switch e.Kind {
	case CapPace:
		return "pace+" + strconv.FormatFloat(e.N, 'f', -1, 64)
	case CapDefault:
		return "default"
	}
	return strconv.FormatFloat(e.N, 'f', -1, 64)
}

// MarshalJSON writes a fixed cap as a number (as v1 did) and the others as strings.
func (e CapElem) MarshalJSON() ([]byte, error) {
	if e.Kind == CapFixed {
		return json.Marshal(e.N)
	}
	return json.Marshal(e.String())
}
