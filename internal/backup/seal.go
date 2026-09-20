package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

// Sealing the part of an archive that is worth stealing.
//
// An archive that carries the homeserver's keys is a generalpass: signing key, macaroon,
// MAS encryption secret. Without them no migration keeps its sessions (§4.100 lists what
// each one costs), so leaving them out is not an option either — the operator asked for
// them to be included by default and encrypted.
//
// Where the key lives is the whole design:
//
//   - in the archive → not encryption, decoration.
//   - on the server  → whoever holds the server holds the archive, and a *new* server
//     does not have it, so the one job the archive exists for fails.
//   - with the operator → the archive is safe in transit and in cloud storage, and a
//     migration works. Shown once, stored nowhere.
//
// Only this part is sealed. Losing the key must not cost the accounts, rooms, messages
// and media — it costs the sessions, and that is a survivable amount of bad.

const (
	// keyBytes is 32: AES-256. The key is generated, never typed, so there is no
	// reason to be economical with it.
	keyBytes = 32
	// sealedMagic lets a reader tell a sealed blob from a truncated one before it
	// starts guessing about nonces.
	sealedMagic = "MXCTRLSEAL1"
)

// recoveryKeyAlphabet is Crockford base32 minus nothing: unambiguous when read aloud
// or copied off a screen, which is the only way this key ever travels.
var recoveryKeyAlphabet = base32.NewEncoding("0123456789ABCDEFGHJKMNPQRSTVWXYZ").WithPadding(base32.NoPadding)

// NewRecoveryKey returns a fresh key and the form the operator sees.
func NewRecoveryKey() (key []byte, shown string, err error) {
	key = make([]byte, keyBytes)
	if _, err = rand.Read(key); err != nil {
		return nil, "", fmt.Errorf("could not generate a recovery key: %w", err)
	}
	return key, FormatRecoveryKey(key), nil
}

// FormatRecoveryKey groups the key so a human can copy it without losing their place.
func FormatRecoveryKey(key []byte) string {
	raw := recoveryKeyAlphabet.EncodeToString(key)
	var b strings.Builder
	for i, r := range raw {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ParseRecoveryKey accepts what FormatRecoveryKey produced, however the operator
// re-typed it: dashes or not, spaces or not, upper or lower case.
func ParseRecoveryKey(shown string) ([]byte, error) {
	cleaned := strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "").Replace(shown))
	if cleaned == "" {
		return nil, fmt.Errorf("kein Wiederherstellungsschlüssel angegeben")
	}
	key, err := recoveryKeyAlphabet.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("das ist kein Wiederherstellungsschlüssel — erwartet werden %d Zeichen in Vierergruppen", len(FormatRecoveryKey(make([]byte, keyBytes))))
	}
	if len(key) != keyBytes {
		return nil, fmt.Errorf("der Wiederherstellungsschlüssel ist unvollständig")
	}
	return key, nil
}

// Seal encrypts plaintext with AES-256-GCM.
//
// The nonce is random per call and travels in front of the ciphertext; GCM authenticates
// both, so a corrupted or truncated archive is refused rather than half-decrypted.
func Seal(key, plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(sealedMagic)+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, sealedMagic...)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, []byte(sealedMagic)), nil
}

// Open reverses Seal, and says which of the two things went wrong: the wrong key, or
// a file that is not one of ours.
func Open(key, sealed []byte) ([]byte, error) {
	if len(sealed) < len(sealedMagic) || string(sealed[:len(sealedMagic)]) != sealedMagic {
		return nil, fmt.Errorf("dieser Teil des Archivs ist nicht verschlüsselt worden")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	body := sealed[len(sealedMagic):]
	if len(body) < gcm.NonceSize() {
		return nil, fmt.Errorf("der verschlüsselte Teil ist unvollständig")
	}
	nonce, ct := body[:gcm.NonceSize()], body[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, ct, []byte(sealedMagic))
	if err != nil {
		// GCM cannot tell a wrong key from a changed byte, and neither can this — so
		// it says both rather than picking the friendlier one and being wrong.
		return nil, fmt.Errorf("der Wiederherstellungsschlüssel passt nicht, oder das Archiv wurde verändert")
	}
	return out, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("recovery key must be %d bytes, got %d", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
