package handlers

import (
	"io"
	"strings"
	"testing"
)

// The media exec must not start before the archive reaches the media, and its release
// must be closable more than once (end marker, then cleanup) without a panic.
func TestMediaSourceStartsOnReadAndReleasesOnce(t *testing.T) {
	started := 0
	m := &mediaSource{r: strings.NewReader("media"), start: func() { started++ }, release: make(chan struct{})}
	if started != 0 {
		t.Fatal("producer started before anyone read")
	}
	if b, err := io.ReadAll(m); err != nil || string(b) != "media" {
		t.Fatalf("read %q, %v", b, err)
	}
	if started != 1 {
		t.Fatalf("producer started %d times, want exactly once", started)
	}
	select {
	case <-m.release:
		t.Fatal("released before the end of the archive was signalled")
	default:
	}
	m.EndOfArchive()
	m.EndOfArchive()
	<-m.release
}
