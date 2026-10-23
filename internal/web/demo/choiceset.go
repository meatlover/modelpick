package demo

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/meatlover/modelpick/internal/web"
)

// capElement validates one cap element (a number 0–100, "pace+N" with N
// 5–50, or "default"); where names it in the refusal.
func capElement(where string, raw json.RawMessage) (any, error) {
	text := strings.TrimSpace(string(raw))
	var n float64
	if text != "null" && json.Unmarshal(raw, &n) == nil {
		if n < 0 || n > 100 {
			return nil, refuse("%s: %s is outside 0–100%%", where, text)
		}
		return n, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		// Owner ruling 2026-10-23 (round 5): "default" means the position
		// uses the Global default cap; stored as sent.
		if s == "default" {
			return s, nil
		}
		if m := paceRE.FindStringSubmatch(s); m != nil {
			v, _ := strconv.Atoi(m[1])
			// Owner ruling 2026-10-23 (rounds 3 and 6): pace N is 5–50
			// ("set a minimum, +5").
			if v < 5 || v > 50 {
				return nil, refuse("%s: %s is outside pace+5–pace+50", where, s)
			}
			return s, nil
		}
	}
	var shown string
	if text == "null" || json.Unmarshal(raw, &shown) != nil {
		shown = text
	}
	return nil, refuse("%s: %s is not a number, pace+N or default", where, shown)
}

// parseCap validates a cap list; prefix is "Profile p, choice n" and
// labels are the account's window labels for the model (none without a
// stored reading, so the position is numbered instead).
func parseCap(prefix string, labels []string, raw json.RawMessage) ([]any, error) {
	var items []json.RawMessage
	if strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, refuse("%s: cap must be a list or null", prefix)
	}
	out := []any{}
	for k, it := range items {
		v, err := capElement(capWhere(prefix, labels, k), it)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// capWhere names cap position k: by window label when the account has
// them, else by number (owner, 2026-10-23, N13).
func capWhere(prefix string, labels []string, k int) string {
	if k < len(labels) {
		return prefix + ", " + labels[k] + " cap"
	}
	return prefix + ", cap " + strconv.Itoa(k+1)
}

// fixThinking keeps the thinking if the model offers it, else the first.
func (d *Demo) fixThinking(c *web.Choice) {
	levels := d.modelThinking(c.Account, c.Model)
	if c.Thinking != nil && contains(levels, *c.Thinking) {
		return
	}
	c.Thinking = nil
	if len(levels) > 0 {
		c.Thinking = str(levels[0])
	}
}

func (d *Demo) setAccount(c *web.Choice, v string) error {
	if !d.hasAccount(v) {
		return refuse("Unknown account %s", v)
	}
	c.Account = v
	if !d.offers(v, c.Model) {
		c.Model = d.models[v][0].ID
	}
	d.fixThinking(c)
	return nil
}

func (d *Demo) offers(account, model string) bool {
	for _, m := range d.models[account] {
		if m.ID == model {
			return true
		}
	}
	return false
}

func (d *Demo) setField(c *web.Choice, base string, a opArgs) error {
	where := base
	var s string
	var list []string
	switch a.Field {
	case "account", "model":
		if json.Unmarshal(a.Value, &s) != nil {
			return refuse("%s: %s must be text", where, a.Field)
		}
		if a.Field == "account" {
			return d.setAccount(c, s)
		}
		if !d.offers(c.Account, s) {
			return refuse("%s: %s does not offer %s", where, c.Account, s)
		}
		c.Model = s
		d.fixThinking(c)
	case "thinking":
		return d.setThinking(c, where, a.Value)
	case "cap":
		v, err := parseCap(base, d.windows[c.Account][c.Model], a.Value)
		c.Cap = v
		return err
	case "skip_during", "allow_during":
		if json.Unmarshal(a.Value, &list) != nil || list == nil {
			return refuse("%s: %s must be a list", where, a.Field)
		}
		return d.setSchedules(c, where, a.Field, list)
	default:
		return refuse("Unknown field %s", a.Field)
	}
	return nil
}

func (d *Demo) setThinking(c *web.Choice, where string, raw json.RawMessage) error {
	var s *string
	levels := d.modelThinking(c.Account, c.Model)
	if json.Unmarshal(raw, &s) != nil || (s == nil) != (len(levels) == 0) || (s != nil && !contains(levels, *s)) {
		return refuse("%s: thinking %s is not offered by %s", where, strings.TrimSpace(string(raw)), c.Model)
	}
	c.Thinking = s
	return nil
}

func (d *Demo) setSchedules(c *web.Choice, where, field string, list []string) error {
	for _, n := range list {
		if d.schedule(n) == nil {
			return refuse("%s: unknown schedule %s", where, n)
		}
	}
	if field == "skip_during" {
		c.SkipDuring = list
	} else {
		c.AllowDuring = list
	}
	return nil
}

func (d *Demo) opChoiceSet(a opArgs) error {
	p, err := d.choiceAt(a.Profile, a.Index, a.Expect)
	if err != nil {
		return err
	}
	where := "Profile " + p.Name + ", choice " + strconv.Itoa(a.Index+1)
	c := p.Choices[a.Index]
	if err := d.setField(&c, where, a); err != nil {
		return err
	}
	if len(c.SkipDuring) > 0 && len(c.AllowDuring) > 0 {
		return refuse("Use Skip or Allow, not both")
	}
	p.Choices[a.Index] = c
	return nil
}
