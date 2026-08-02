package main

import (
	"bytes"
	"io"
	"sort"

	"github.com/yemon/calypso/internal/project"
)

// minScrubLen is the shortest value the scrubber will conceal. Tiny values
// ("1", "on", "true") are not meaningful secrets, and replacing them would
// garble ordinary output that happens to contain the same characters.
const minScrubLen = 4

type scrubPattern struct {
	value       []byte
	replacement []byte
}

// scrubWriter streams a child process's output to dst, replacing every
// occurrence of a known secret value with the name-free <concealed> marker.
// Up to maxLen-1
// trailing bytes are held back between writes so a value split across two
// Write calls is still caught; Close flushes that tail. Close does not
// close dst.
type scrubWriter struct {
	dst    io.Writer
	pats   []scrubPattern
	maxLen int
	buf    []byte
}

func newScrubWriter(dst io.Writer, vars []project.Var) *scrubWriter {
	w := &scrubWriter{dst: dst, maxLen: 1}
	for _, v := range vars {
		val := v.Value.Reveal()
		if len(val) < minScrubLen {
			continue
		}
		w.pats = append(w.pats, scrubPattern{
			value:       []byte(val),
			replacement: []byte("<concealed>"),
		})
		if len(val) > w.maxLen {
			w.maxLen = len(val)
		}
	}
	// Longest first, so when two values start at the same offset (one a
	// prefix of the other) the longer one wins and nothing leaks.
	sort.Slice(w.pats, func(i, j int) bool { return len(w.pats[i].value) > len(w.pats[j].value) })
	return w
}

func (w *scrubWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if err := w.flush(false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close flushes the held-back tail after the final Write.
func (w *scrubWriter) Close() error { return w.flush(true) }

func (w *scrubWriter) flush(final bool) error {
	for {
		idx, pi := w.earliestMatch()
		if idx < 0 {
			break
		}
		if _, err := w.dst.Write(w.buf[:idx]); err != nil {
			return err
		}
		if _, err := w.dst.Write(w.pats[pi].replacement); err != nil {
			return err
		}
		w.buf = append(w.buf[:0], w.buf[idx+len(w.pats[pi].value):]...)
	}
	// Any complete match left in buf would have been consumed above, and an
	// incomplete one can only start within the last maxLen-1 bytes — so
	// everything before that is safe to emit now.
	keep := 0
	if !final {
		keep = w.maxLen - 1
	}
	if len(w.buf) > keep {
		n := len(w.buf) - keep
		if _, err := w.dst.Write(w.buf[:n]); err != nil {
			return err
		}
		w.buf = append(w.buf[:0], w.buf[n:]...)
	}
	return nil
}

// earliestMatch returns the offset and pattern index of the leftmost secret
// occurrence in buf, preferring the longest pattern on offset ties (the
// slice is sorted longest-first). Returns (-1, -1) when nothing matches.
func (w *scrubWriter) earliestMatch() (int, int) {
	best, bi := -1, -1
	for i := range w.pats {
		if idx := bytes.Index(w.buf, w.pats[i].value); idx >= 0 && (best < 0 || idx < best) {
			best, bi = idx, i
		}
	}
	return best, bi
}
