package offsite

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Object names are the contract that makes pruning safe.
//
//	<prefix>/matrixctrl-<yyyy-mm-dd-hhmm>-<anlass>.tar.gz.age
//
// Retention deletes things, and a bucket is shared ground: the operator may keep other
// files in it, even under the same prefix. So the rule is not "everything that looks old"
// but "only what this exact pattern describes, one level below our own prefix". Anything
// else is invisible to Prune and cannot be reached by the download or restore routes.

const (
	namePrefix   = "matrixctrl-"
	nameSuffix   = ".tar.gz.age"
	timeLayout   = "2006-01-02-1504"
	occScheduled = "zeitplan"
	occManual    = "manuell"
	occUpgrade   = "vor-upgrade-"
)

// Occasions a backup is taken for.
const (
	OccasionScheduled = occScheduled
	OccasionManual    = occManual
)

var (
	nameRe     = regexp.MustCompile(`^matrixctrl-(\d{4}-\d{2}-\d{2}-\d{4})-(.+)\.tar\.gz\.age$`)
	occasionRe = regexp.MustCompile(`^(zeitplan|manuell|vor-upgrade-[A-Za-z0-9][A-Za-z0-9._-]{0,63})$`)
	unsafeRe   = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// OccasionUpgrade is the occasion for the backup taken before moving to a version. The
// version goes into an object name, so anything outside a conservative alphabet becomes
// a dash ("26.5.1+build" must not smuggle a slash into a key).
func OccasionUpgrade(version string) string {
	v := strings.Trim(unsafeRe.ReplaceAllString(version, "-"), "-.")
	if v == "" {
		v = "unbekannt"
	}
	if len(v) > 64 {
		v = v[:64]
	}
	return occUpgrade + v
}

// ObjectName builds the file name for a backup taken at t.
func ObjectName(t time.Time, occasion string) (string, error) {
	if !occasionRe.MatchString(occasion) {
		return "", fmt.Errorf("unbekannter Anlass %q", occasion)
	}
	return namePrefix + t.UTC().Format(timeLayout) + "-" + occasion + nameSuffix, nil
}

// Entry is one backup in the bucket, as far as its name says.
type Entry struct {
	Key      string    `json:"key"`
	Name     string    `json:"name"`
	Time     time.Time `json:"time"`
	Occasion string    `json:"occasion"`
	Size     int64     `json:"size"`
}

// IsUpgrade is true for the backups taken before a version change.
func (e Entry) IsUpgrade() bool { return strings.HasPrefix(e.Occasion, occUpgrade) }

// ParseName reads a file name written by ObjectName. ok is false for everything else.
func ParseName(name string) (t time.Time, occasion string, ok bool) {
	m := nameRe.FindStringSubmatch(name)
	if m == nil || !occasionRe.MatchString(m[2]) {
		return time.Time{}, "", false
	}
	t, err := time.ParseInLocation(timeLayout, m[1], time.UTC)
	if err != nil {
		return time.Time{}, "", false
	}
	return t, m[2], true
}

// NormalizePrefix trims slashes and rejects what would climb out of the prefix.
func NormalizePrefix(p string) (string, error) {
	p = strings.Trim(strings.TrimSpace(p), "/")
	if p == "" {
		return "", nil
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("Präfix: „%s“ ist kein gültiger Pfad (leere oder relative Teile sind nicht erlaubt)", p)
		}
	}
	if strings.ContainsAny(p, "\\\x00") {
		return "", fmt.Errorf("Präfix: unzulässige Zeichen")
	}
	return p, nil
}

func joinKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// entryFor accepts an object only if it is ours: directly below the prefix, with a name
// ObjectName could have produced.
func entryFor(prefix string, o Object) (Entry, bool) {
	name := o.Key
	if prefix != "" {
		if !strings.HasPrefix(name, prefix+"/") {
			return Entry{}, false
		}
		name = strings.TrimPrefix(name, prefix+"/")
	}
	if strings.Contains(name, "/") {
		return Entry{}, false
	}
	t, occ, ok := ParseName(name)
	if !ok {
		return Entry{}, false
	}
	return Entry{Key: o.Key, Name: name, Time: t, Occasion: occ, Size: o.Size}, true
}
