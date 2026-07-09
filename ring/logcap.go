package ring

import (
	"log"
	"os"
	"sync"
)

// SetupLog routes the standard logger to path with a size cap: when the file exceeds
// maxBytes it is rotated to path+".old" (one generation kept). With every push logged
// raw (~0.5 KB/event), the default 512 KB holds roughly the last 1000 events per file —
// bounded on the speaker's tmpfs instead of growing until RAM runs out.
func SetupLog(path string, maxBytes int64) error {
	w := &cappedWriter{path: path, max: maxBytes}
	if err := w.open(); err != nil {
		return err
	}
	log.SetOutput(w)
	return nil
}

type cappedWriter struct {
	mu   sync.Mutex
	f    *os.File
	n    int64
	path string
	max  int64
}

func (w *cappedWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.n = f, st.Size()
	return nil
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.n+int64(len(p)) > w.max {
		w.f.Close()
		os.Rename(w.path, w.path+".old") // best effort; O_CREATE below recovers either way
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.n += int64(n)
	return n, err
}
