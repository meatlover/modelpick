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

// A short row's last cell wider than every other cell in its column must not
// widen that column.
func TestWriteColumnsLastCellSetsNoWidth(t *testing.T) {
	rows := [][]string{{"a", "b", "c"}, {"a", "wide-last"}}
	var buf bytes.Buffer
	writeColumns(&buf, rows, nil)
	if want := "a  b  c\na  wide-last\n"; buf.String() != want {
		t.Errorf("got %q\nwant %q", buf.String(), want)
	}
}
