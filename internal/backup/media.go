package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"sync"
	"time"
)

// The uploaded files inside an archive (format 3).
//
// Format 2 stored them as one member, `media/media.tar`, because they already arrive as
// a tar from the Synapse pod. A tar header needs the size before the body, a stream does
// not know it, so that member was first copied into a temporary file — and on an install
// with hundreds of gigabytes of uploads that file filled the panel pod's ephemeral
// storage until the pod was evicted, taking the browser download or the scheduled
// offsite backup down with it.
//
// Format 3 reads the inner tar entry by entry instead. Every entry carries its own size
// in its own header, so each one can be written straight into the outer archive as a
// member of its own, `media/files/<path>`, and nothing is ever buffered.
//
// Archives in the old layout are still read: a restore finds whichever of the two is
// there.
const (
	// LegacyMediaMember is the single-tar layout of format 1 and 2.
	LegacyMediaMember = "media/media.tar"
	// MediaFilesPrefix is where format 3 puts one member per file or directory.
	MediaFilesPrefix = "media/files/"
)

// MediaStats is what writeMedia did, so skipped entries are counted rather than lost.
type MediaStats struct {
	Files   int64
	Dirs    int64
	Bytes   int64
	Skipped int64 // links and other types a media store does not contain
}

// MediaInfo describes the media part of an archive, for either layout, so a preview and
// the free-space check ahead of a restore are right whichever one they were handed.
type MediaInfo struct {
	// Layout is "files" (format 3), "tar" (the single-member layout) or empty.
	Layout string `json:"layout,omitempty"`
	// Files is -1 when the layout cannot say without unpacking: the single tar member
	// is opaque, and counting inside it would mean reading all of it.
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

// note folds one outer member into the info. Members that are not media are ignored.
func (m *MediaInfo) note(h *tar.Header) {
	switch {
	case h.Name == LegacyMediaMember && h.Typeflag == tar.TypeReg:
		m.Layout, m.Files, m.Bytes = "tar", -1, h.Size
	case strings.HasPrefix(h.Name, MediaFilesPrefix) && m.Layout != "tar":
		m.Layout = "files"
		if h.Typeflag == tar.TypeReg {
			m.Files++
			m.Bytes += h.Size
		}
	}
}

// cleanMediaPath turns an inner tar name into a path relative to the media root, and
// refuses one that would land anywhere else. An empty result means the root itself.
//
// Refused rather than quietly repaired: the stream comes out of a pod, and again out of
// an archive a browser uploaded. A name with `..` in it is not a file to be salvaged.
func cleanMediaPath(name string) (string, error) {
	if strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("unzulässiger Pfad im Medienstrom: %q", name)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return "", fmt.Errorf("unzulässiger Pfad im Medienstrom: %q", name)
		}
	}
	c := path.Clean(name)
	if c == "." {
		return "", nil
	}
	return c, nil
}

// EndOfArchiveNotifier is a media source that waits to be told the archive's end marker
// has been read before it lets its producer finish (k8s.Client.TarFromPod with a
// release channel).
type EndOfArchiveNotifier interface{ EndOfArchive() }

// writeMedia copies an inner tar into the outer archive as one member per entry.
//
// Regular files and directories are kept. Links are skipped, not followed and not
// recreated: a symlink restored from an archive that is then written *through* by a
// later entry is the classic way out of an extraction directory, and a Synapse media
// store contains none. Anything else is skipped too, and counted.
func writeMedia(tw *tar.Writer, src io.Reader, at time.Time) (MediaStats, error) {
	var st MediaStats
	tr := tar.NewReader(src)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			// The end marker is in: a source held open until now may let its producer
			// finish, or the padding drained below would never end.
			if n, ok := src.(EndOfArchiveNotifier); ok {
				n.EndOfArchive()
			}
			break
		}
		if err != nil {
			return st, fmt.Errorf("Medienstrom unlesbar: %w", err)
		}
		rel, err := cleanMediaPath(h.Name)
		if err != nil {
			return st, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if rel == "" {
				continue
			}
			if err := tw.WriteHeader(&tar.Header{
				Name: MediaFilesPrefix + rel + "/", Typeflag: tar.TypeDir,
				Mode: h.Mode & 0o777, Uid: h.Uid, Gid: h.Gid, ModTime: modOr(h, at),
			}); err != nil {
				return st, err
			}
			st.Dirs++
		case tar.TypeReg:
			if rel == "" {
				st.Skipped++
				continue
			}
			if err := tw.WriteHeader(&tar.Header{
				Name: MediaFilesPrefix + rel, Typeflag: tar.TypeReg, Size: h.Size,
				Mode: h.Mode & 0o777, Uid: h.Uid, Gid: h.Gid, ModTime: modOr(h, at),
			}); err != nil {
				return st, err
			}
			n, err := io.Copy(tw, tr)
			if err != nil {
				// The path stays in the server log. The message shown in the panel
				// names where in the stream, not whose file: remote_content is named
				// after other homeservers, and the operator does not want those names
				// on screen (2026-10-20).
				log.Printf("backup: media stream broke off at %s: %v", rel, err)
				return st, fmt.Errorf("abgebrochen bei Datei %d (%s): %w", st.Files+1, mediaArea(rel), err)
			}
			st.Files++
			st.Bytes += n
		default:
			st.Skipped++
		}
	}
	// The inner tar ends in padding the reader does not consume. Left unread, the
	// producer on the other end of the pipe would block on a write nobody takes.
	_, _ = io.Copy(io.Discard, src)
	if st.Skipped > 0 {
		log.Printf("backup: %d Einträge im Medienstrom übersprungen (Links oder unbekannte Typen)", st.Skipped)
	}
	return st, nil
}

func modOr(h *tar.Header, at time.Time) time.Time {
	if h.ModTime.IsZero() {
		return at
	}
	return h.ModTime
}

// MediaTarStream hands a restore the media of an archive as the single tar stream it
// gives the pod, whichever layout the archive has.
//
// next yields the archive's members in order. The old layout is returned as it is. The
// new one is rebuilt on the fly through a pipe: each `media/files/` member is written
// into a tar.Writer as it is read, so the files are never collected anywhere, the same
// property that made the archive streamable on the way in. ok is false if the archive
// has no media.
func MediaTarStream(tr *tar.Reader, closer io.Closer) (rc io.ReadCloser, ok bool, err error) {
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("beschädigtes Archiv: %w", err)
		}
		if h.Name == LegacyMediaMember && h.Typeflag == tar.TypeReg {
			return mediaReadCloser{Reader: tr, closer: closer}, true, nil
		}
		if strings.HasPrefix(h.Name, MediaFilesPrefix) {
			return rebuildMediaTar(tr, h, closer), true, nil
		}
	}
}

func rebuildMediaTar(tr *tar.Reader, first *tar.Header, closer io.Closer) io.ReadCloser {
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pw.CloseWithError(copyMediaMembers(tar.NewWriter(pw), tr, first))
	}()
	return &pipeMedia{pr: pr, done: done, closer: closer}
}

func copyMediaMembers(tw *tar.Writer, tr *tar.Reader, h *tar.Header) error {
	for {
		if strings.HasPrefix(h.Name, MediaFilesPrefix) {
			if err := copyMediaMember(tw, tr, h); err != nil {
				return err
			}
		}
		var err error
		h, err = tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("beschädigtes Archiv: %w", err)
		}
		// The media members are contiguous; what follows them is not ours to read.
		if !strings.HasPrefix(h.Name, "media/") {
			break
		}
	}
	return tw.Close()
}

func copyMediaMember(tw *tar.Writer, tr *tar.Reader, h *tar.Header) error {
	rel, err := cleanMediaPath(strings.TrimPrefix(h.Name, MediaFilesPrefix))
	if err != nil {
		return err
	}
	if rel == "" || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) {
		return nil
	}
	out := &tar.Header{Name: "./" + rel, Typeflag: h.Typeflag, Mode: h.Mode,
		Uid: h.Uid, Gid: h.Gid, ModTime: h.ModTime}
	if h.Typeflag == tar.TypeDir {
		out.Name += "/"
	} else {
		out.Size = h.Size
	}
	if err := tw.WriteHeader(out); err != nil {
		return err
	}
	if h.Typeflag == tar.TypeReg {
		_, err = io.Copy(tw, tr)
	}
	return err
}

type mediaReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r mediaReadCloser) Close() error { return r.closer.Close() }

// pipeMedia closes the read side first, which makes the producing goroutine's next write
// fail and end, and only then the archive file it was reading from.
type pipeMedia struct {
	pr     *io.PipeReader
	done   chan struct{}
	closer io.Closer
	once   sync.Once
}

func (p *pipeMedia) Read(b []byte) (int, error) { return p.pr.Read(b) }

func (p *pipeMedia) Close() error {
	var err error
	p.once.Do(func() {
		_ = p.pr.Close()
		<-p.done
		err = p.closer.Close()
	})
	return err
}

// mediaArea names the part of Synapse's media store a path lies in, without the path.
func mediaArea(rel string) string {
	switch {
	case strings.Contains(rel, "remote_content/"), strings.Contains(rel, "remote_thumbnail/"):
		return "Medien-Cache anderer Server"
	case strings.Contains(rel, "local_thumbnails/"):
		return "Vorschaubilder"
	case strings.Contains(rel, "local_content/"):
		return "hochgeladene Dateien"
	default:
		return "Medien"
	}
}
