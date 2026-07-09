package ring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCappedWriterRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	w := &cappedWriter{path: path, max: 100}
	if err := w.open(); err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("a", 39) + "\n" // 40 bytes
	for i := 0; i < 5; i++ {               // rotations before write 3 and write 5
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	cur, _ := os.ReadFile(path)
	old, _ := os.ReadFile(path + ".old")
	if len(old) != 80 || len(cur) != 40 {
		t.Fatalf("after rotation: old=%d cur=%d, want 80/40", len(old), len(cur))
	}
}
