package web

// The page calls .find/.map on every list, so a nil slice or map, which
// encoding/json writes as null, would blank a tab. Each response is copied
// through these before it is written; only the fields whose null means
// something (Choice.Cap, Thinking and LoadBalance, UsageWindow.Scope,
// UsageRow.ReadingAgeSeconds) keep it. The backend's slices are never
// written into.

func list[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}

func normalState(s State) State {
	accounts := make([]Account, len(s.Accounts))
	for i, a := range s.Accounts {
		if a.Fields == nil {
			a.Fields = map[string]any{}
		}
		accounts[i] = a
	}
	s.Accounts = accounts
	s.Models = normalModels(s.Models)
	s.Windows = normalWindows(s.Windows)
	s.Schedules = list(s.Schedules)
	s.Algorithms = list(s.Algorithms)
	profiles := make([]Profile, len(s.Profiles))
	for i, p := range s.Profiles {
		choices := make([]Choice, len(p.Choices))
		for j, c := range p.Choices {
			c.SkipDuring = list(c.SkipDuring)
			c.AllowDuring = list(c.AllowDuring)
			choices[j] = c // Cap nil stays nil: it means uncapped
		}
		p.Choices = choices
		profiles[i] = p
	}
	s.Profiles = profiles
	return s
}

func normalModels(in map[string][]Model) map[string][]Model {
	out := make(map[string][]Model, len(in))
	for acct, models := range in {
		ms := make([]Model, len(models))
		for i, m := range models {
			m.Thinking = list(m.Thinking)
			ms[i] = m
		}
		out[acct] = ms
	}
	return out
}

func normalWindows(in map[string]map[string][]string) map[string]map[string][]string {
	out := make(map[string]map[string][]string, len(in))
	for acct, byModel := range in {
		m := make(map[string][]string, len(byModel))
		for model, labels := range byModel {
			m[model] = list(labels)
		}
		out[acct] = m
	}
	return out
}

func normalRow(r UsageRow) UsageRow {
	r.Windows = list(r.Windows)
	return r
}

func normalUsage(rows []UsageRow) []UsageRow {
	out := make([]UsageRow, len(rows))
	for i, r := range rows {
		out[i] = normalRow(r)
	}
	return out
}

func normalDoctor(d Doctor) Doctor {
	d.Findings = list(d.Findings)
	return d
}

func normalOpResult(r OpResult) OpResult {
	r.Findings = list(r.Findings)
	return r
}
