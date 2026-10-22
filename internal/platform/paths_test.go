package platform

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestConfigPathPrecedence(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"explicit", map[string]string{"MODELPICK_CONFIG": "/x/c.json", "XDG_CONFIG_HOME": "/xdg"}, "/x/c.json"},
		{"xdg", map[string]string{"XDG_CONFIG_HOME": "/xdg"}, "/xdg/modelpick/config.json"},
		{"home", map[string]string{}, "/home/u/.config/modelpick/config.json"},
	}
	for _, c := range cases {
		if got := ConfigPath(env(c.env), "/home/u"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCacheDir(t *testing.T) {
	if got := CacheDir(env(map[string]string{"XDG_CACHE_HOME": "/xc"}), "/home/u"); got != "/xc/modelpick" {
		t.Errorf("xdg: got %q", got)
	}
	if got := CacheDir(env(nil), "/home/u"); got != "/home/u/.cache/modelpick" {
		t.Errorf("home: got %q", got)
	}
}
