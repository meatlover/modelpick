package platform

import (
	"reflect"
	"testing"
)

func TestEnvWithout(t *testing.T) {
	in := []string{"FOO=1", "FOOBAR=1", "BAR=2", "PATH=/bin", "NOEQUALS"}
	got := EnvWithout(in, "FOO", "BAR")
	want := []string{"FOOBAR=1", "PATH=/bin", "NOEQUALS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
