package offsite

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

// Whole-archive encryption with age.
//
// The server holds only the public key (`age1…`): it can seal an archive and cannot open
// one. The private key (`AGE-SECRET-KEY-1…`) is shown to the operator once, when the pair
// is generated, and is typed back in only for a restore; it is used for that request and
// never stored or logged. Nothing in this file may put key material into an error text.

const (
	publicKeyPrefix  = "age1"
	privateKeyPrefix = "AGE-SECRET-KEY-"
	// ageMagic starts every binary age file. A restore uses it to tell an encrypted
	// upload from a plain archive without trusting the file name.
	ageMagic = "age-encryption.org/v1"
)

// GenerateKeyPair returns a fresh X25519 pair in age's text form.
func GenerateKeyPair() (public, private string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("Schlüsselpaar konnte nicht erzeugt werden: %w", err)
	}
	return id.Recipient().String(), id.String(), nil
}

// ParsePublicKey checks what the operator pasted and returns it normalised.
//
// A private key pasted into the public-key field is the one mistake that matters: it
// would be stored in the settings, in plain text, on the server — exactly what the whole
// design exists to avoid. It is refused by its prefix, before parsing, and the error
// does not repeat the input.
func ParsePublicKey(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToUpper(s), privateKeyPrefix) {
		return "", errors.New("Das ist ein privater Schlüssel (AGE-SECRET-KEY-…). " +
			"Hier gehört der öffentliche Schlüssel hin; er beginnt mit „age1“. " +
			"Den privaten Schlüssel nirgends auf dem Server eintragen.")
	}
	if !strings.HasPrefix(s, publicKeyPrefix) {
		return "", errors.New("Das ist kein öffentlicher age-Schlüssel; er beginnt mit „age1“.")
	}
	if _, err := age.ParseX25519Recipient(s); err != nil {
		return "", errors.New("Der öffentliche Schlüssel ist unvollständig oder beschädigt.")
	}
	return s, nil
}

// Encrypt wraps w so that everything written to the result is readable only with the
// private half of publicKey. The caller must Close it — that writes the final,
// authenticated chunk; a stream that is not closed is detectably truncated on reading.
func Encrypt(w io.Writer, publicKey string) (io.WriteCloser, error) {
	pub, err := ParsePublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	rcpt, err := age.ParseX25519Recipient(pub)
	if err != nil {
		return nil, errors.New("Der öffentliche Schlüssel ist unbrauchbar.")
	}
	return age.Encrypt(w, rcpt)
}

// Decrypt opens an age stream with the operator's private key.
//
// age reads the header here, so a key that does not belong to the archive fails now,
// before a restore has stopped anything — and says so in words, not as a bare
// "no identity matched".
func Decrypt(r io.Reader, privateKey string) (io.Reader, error) {
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return nil, errors.New("Das Archiv ist verschlüsselt; es fehlt der private Schlüssel (AGE-SECRET-KEY-1…).")
	}
	// ParseIdentities also reads a key *file* with its "# created" comment lines, which
	// is what `age-keygen -o` writes and what an operator is likely to paste whole.
	ids, err := age.ParseIdentities(strings.NewReader(privateKey))
	if err != nil || len(ids) == 0 {
		return nil, errors.New("Das ist kein privater age-Schlüssel; er beginnt mit „AGE-SECRET-KEY-1“.")
	}
	out, err := age.Decrypt(r, ids...)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, errors.New("Dieser private Schlüssel gehört nicht zu diesem Archiv.")
		}
		return nil, fmt.Errorf("Das Archiv lässt sich nicht entschlüsseln (Datei beschädigt oder keine age-Datei): %w", err)
	}
	return out, nil
}

// LooksEncrypted reports whether the first bytes of a stream are an age header.
func LooksEncrypted(head []byte) bool {
	return bytes.HasPrefix(head, []byte(ageMagic))
}

// MagicLen is how many bytes LooksEncrypted needs to see.
func MagicLen() int { return len(ageMagic) }
