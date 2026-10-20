package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type entry struct {
	name, body, link string
	typ              byte
}

func innerTar(t *testing.T, es []entry) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, e := range es {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Linkname: e.link, ModTime: time.Unix(1700000000, 0)}
		if e.typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if e.typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			_, _ = tw.Write([]byte(e.body))
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var sample = []entry{
	{name: "./", typ: tar.TypeDir},
	{name: "./media_store/", typ: tar.TypeDir},
	{name: "./media_store/aa/", typ: tar.TypeDir},
	{name: "./media_store/aa/bb", body: "hello", typ: tar.TypeReg},
	{name: "./media_store/empty", body: "", typ: tar.TypeReg},
	{name: "./media_store/link", link: "/etc/passwd", typ: tar.TypeSymlink},
	{name: "./media_store/hard", link: "./media_store/aa/bb", typ: tar.TypeLink},
	{name: "./media_store/fifo", typ: tar.TypeFifo},
}

// outer wraps the media into a gzip'd outer archive the way CreateFull does.
func outer(t *testing.T, media []byte, legacy bool) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	if err := writeFile(tw, "manifest.json", []byte(`{"format_version":3,"parts":["media/"]}`), 0o644, time.Now()); err != nil {
		t.Fatal(err)
	}
	if legacy {
		if err := writeFile(tw, LegacyMediaMember, media, 0o644, time.Now()); err != nil {
			t.Fatal(err)
		}
	} else if _, err := writeMedia(tw, bytes.NewReader(media), time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(tw, "secrets/after.txt", []byte("x"), 0o600, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}

func stream(t *testing.T, archive []byte) (io.ReadCloser, bool, error) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	return MediaTarStream(tar.NewReader(gz), io.NopCloser(nil))
}

func readTar(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	got := map[string]string{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return got
		}
		if err != nil {
			t.Fatalf("stream is not a tar: %v", err)
		}
		b, _ := io.ReadAll(tr)
		if h.Typeflag == tar.TypeDir {
			got[strings.TrimSuffix(strings.TrimPrefix(h.Name, "./"), "/")] = "<dir>"
		} else {
			got[strings.TrimPrefix(h.Name, "./")] = string(b)
		}
	}
}

// The point of format 3: a media stream passes through without a file in the temp dir.
// The counter-probe makes the check able to fail: the same inspection must see the file
// the old writeStream would have created.
func TestWriteMediaLeavesNothingInTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	// Counter-probe first: this is what the old code did, and the check must see it.
	f, err := os.CreateTemp("", "mxctrl-part-*")
	if err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(tmp); len(left) != 1 {
		t.Fatalf("the probe cannot see a temp file, so the check below proves nothing")
	}
	_ = f.Close()
	_ = os.Remove(f.Name())

	// A reader that is neither seekable nor sized, like the pod's pipe.
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write(innerTar(t, sample)); _ = pw.Close() }()
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	st, err := writeMedia(tw, pr, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("media left %d file(s) in the temp dir", len(left))
	}
	if st.Files != 2 || st.Dirs != 2 || st.Bytes != 5 || st.Skipped != 3 {
		t.Errorf("stats %+v: want 2 files, 2 dirs (root excluded), 5 bytes, 3 skipped (2 links, 1 fifo)", st)
	}
}

func TestWriteMediaRefusesEscapes(t *testing.T) {
	for _, name := range []string{"../x", "./a/../../x", "/etc/passwd", "a/../b"} {
		var out bytes.Buffer
		tw := tar.NewWriter(&out)
		_, err := writeMedia(tw, bytes.NewReader(innerTar(t, []entry{{name: name, body: "x", typ: tar.TypeReg}})), time.Now())
		if err == nil {
			t.Errorf("%q was accepted", name)
		}
		if strings.Contains(out.String(), "etc/passwd") {
			t.Errorf("%q reached the archive", name)
		}
	}
	// Counter-probe: an ordinary name through the same path is accepted.
	var out bytes.Buffer
	if _, err := writeMedia(tar.NewWriter(&out), bytes.NewReader(innerTar(t, []entry{{name: "./a/b..c", body: "x", typ: tar.TypeReg}})), time.Now()); err != nil {
		t.Errorf("a harmless name was refused: %v", err)
	}
}

func TestMediaRoundTripNewFormat(t *testing.T) {
	arch := outer(t, innerTar(t, sample), false)
	rc, ok, err := stream(t, arch)
	if err != nil || !ok {
		t.Fatalf("stream: ok=%v err=%v", ok, err)
	}
	got := readTar(t, rc)
	want := map[string]string{
		"media_store": "<dir>", "media_store/aa": "<dir>",
		"media_store/aa/bb": "hello", "media_store/empty": "",
	}
	if len(got) != len(want) {
		t.Errorf("got %v want %v", got, want)
	}
	for k, v := range want {
		if g, present := got[k]; !present || g != v {
			t.Errorf("%s: got %q (present=%v), want %q", k, g, present, v)
		}
	}
	if err := rc.Close(); err != nil {
		t.Errorf("close: %v", err)
	}

	a, err := Read(bytes.NewReader(arch))
	if err != nil {
		t.Fatal(err)
	}
	if a.Media.Layout != "files" || a.Media.Files != 2 || a.Media.Bytes != 5 {
		t.Errorf("preview media %+v", a.Media)
	}
}

func TestMediaStreamRefusesTamperedArchive(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	_ = writeFile(tw, MediaFilesPrefix+"ok", []byte("fine"), 0o644, time.Now())
	_ = writeFile(tw, MediaFilesPrefix+"../evil", []byte("bad"), 0o644, time.Now())
	_ = tw.Close()
	_ = gz.Close()
	rc, ok, err := stream(t, b.Bytes())
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if _, err := io.Copy(io.Discard, rc); err == nil {
		t.Error("a member with .. reached the pod stream")
	}
	_ = rc.Close()
}

func TestMediaOldFormatStillRead(t *testing.T) {
	inner := innerTar(t, sample)
	arch := outer(t, inner, true)
	rc, ok, err := stream(t, arch)
	if err != nil || !ok {
		t.Fatalf("stream: ok=%v err=%v", ok, err)
	}
	b, _ := io.ReadAll(rc)
	if !bytes.Equal(b, inner) {
		t.Errorf("the old member must reach the pod unchanged")
	}
	a, err := Read(bytes.NewReader(arch))
	if err != nil {
		t.Fatal(err)
	}
	if a.Media.Layout != "tar" || a.Media.Bytes != int64(len(inner)) || a.Media.Files != -1 {
		t.Errorf("preview media %+v", a.Media)
	}
	// Counter-probe: an archive without media reports none.
	var none bytes.Buffer
	gz := gzip.NewWriter(&none)
	tw := tar.NewWriter(gz)
	_ = writeFile(tw, "manifest.json", []byte(`{"format_version":3,"parts":[]}`), 0o644, time.Now())
	_ = tw.Close()
	_ = gz.Close()
	if _, ok, _ := stream(t, none.Bytes()); ok {
		t.Error("an archive without media claimed to have some")
	}
}

// notifyingSource records the end-of-archive signal and how much of the padding after
// the end marker was read.
type notifyingSource struct {
	r        io.Reader
	notified bool
	afterEnd int
}

func (s *notifyingSource) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if s.notified {
		s.afterEnd += n
	}
	return n, err
}

func (s *notifyingSource) EndOfArchive() { s.notified = true }

// A held producer is released at the end marker, and what follows the marker is read to
// the end — left unread, it kept the exec blocked forever.
func TestWriteMediaReleasesAndDrains(t *testing.T) {
	var inner bytes.Buffer
	tw := tar.NewWriter(&inner)
	_ = tw.WriteHeader(&tar.Header{Name: "./a", Typeflag: tar.TypeReg, Size: 3, Mode: 0o644})
	_, _ = tw.Write([]byte("abc"))
	_ = tw.Close()
	inner.Write(make([]byte, 8192)) // tar's record padding after the end marker

	src := &notifyingSource{r: &inner}
	var outer bytes.Buffer
	if _, err := writeMedia(tar.NewWriter(&outer), src, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !src.notified {
		t.Error("the producer was never released")
	}
	if inner.Len() != 0 {
		t.Errorf("%d bytes after the end marker left unread", inner.Len())
	}
}
