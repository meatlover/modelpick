package opencodego

import (
	"bufio"
	"os"
	"strings"
)

// KeyAt returns the line-th key (1-based) of a key file, counting only lines
// that are non-empty after trimming and do not start with "#". found is false
// when the file has fewer keys; err is non-nil only when the file cannot be
// read. The key is returned to the caller and must never be logged.
func KeyAt(path string, line int) (key string, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		n++
		if n == line {
			return s, true, nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}
	return "", false, nil
}
