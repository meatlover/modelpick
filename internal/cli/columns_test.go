package cli

import (
	"bytes"
	"testing"
)

func TestWriteColumns(t *testing.T) {
	rows := [][]string{{"A", "B", "C"}, {"long-one", "x"}, {"b", "yy", "z"}}
	var buf bytes.Buffer
	writeColumns(&buf, rows, map[int]bool{1: true})
	want := "A          B  C\nlong-one  x\nb         yy  z\n"
	if buf.String() != want {
		t.Errorf("got %q\nwant %q", buf.String(), want)
	}
}
