package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestConfigOutput(t *testing.T) {
	data, err := os.ReadFile("../../examples/config.json")
	if err != nil {
		t.Fatal(err)
	}
	claude, codex, goFake := newFake("claude"), newFake("codex"), newFake("opencode-go")
	d := testDeps(t, string(data), claude, codex, goFake)

	stdout, stderr, code := run(d, "config")
	if code != 0 {
		t.Fatalf("exit %d; stderr %q", code, stderr)
	}
	var got struct {
		Settings struct {
			Minutes    float64 `json:"min_call_interval_minutes"`
			DefaultCap float64 `json:"default_cap"`
		} `json:"settings"`
		Accounts  map[string]map[string]any `json:"accounts"`
		Schedules map[string]struct {
			Days []string `json:"days"`
			UTC  []string `json:"utc"`
		} `json:"schedules"`
		Profiles map[string][]struct {
			Account  string    `json:"account"`
			Cap      []float64 `json:"cap"`
			Uncapped bool      `json:"uncapped"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if got.Settings.DefaultCap != 95 || got.Settings.Minutes != 15 {
		t.Errorf("settings = %+v", got.Settings)
	}
	if p := got.Accounts["claude-rec"]["config_dir"]; p != "/home/u/.claude-rec" {
		t.Errorf("claude-rec config_dir = %v, want absolute", p)
	}
	if s := got.Schedules["deepseek-peak"]; len(s.Days) != 5 || s.UTC[1] != "06:00-10:00" {
		t.Errorf("schedule as written lost: %+v", s)
	}
	cd := got.Profiles["coder-difficult"]
	if len(cd) != 4 || !cd[3].Uncapped || cd[0].Uncapped || len(cd[1].Cap) != 3 || cd[1].Cap[2] != 95 {
		t.Errorf("coder-difficult = %+v", cd)
	}
	if strings.Contains(stdout, `"uncapped": false`) {
		t.Errorf("uncapped must appear only when true:\n%s", stdout)
	}
	for _, f := range []*fakeAdapter{claude, codex, goFake} {
		if f.totalUsage() != 0 || len(f.credCalls) != 0 {
			t.Errorf("%s adapter was touched: usage %v creds %v", f.vendor, f.usage, f.credCalls)
		}
	}
}

func TestConfigRejectsArgsAndBadConfig(t *testing.T) {
	d := testDeps(t, testConfig)
	if _, _, code := run(d, "config", "extra"); code != 1 {
		t.Errorf("extra arg exit %d, want 1", code)
	}
	bad := testDeps(t, `{"version": 2}`)
	if _, stderr, code := run(bad, "config"); code != 1 || !strings.Contains(stderr, "version") {
		t.Errorf("bad config: code %d stderr %q", code, stderr)
	}
}
