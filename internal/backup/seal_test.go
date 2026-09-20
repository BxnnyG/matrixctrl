package backup

import (
	"bytes"
	"strings"
	"testing"
)

func TestSealedContentComesBack(t *testing.T) {
	key, shown, err := NewRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("SYNAPSE_SIGNING_KEY: ed25519 a_XYZ abcdef\nMAS_ENCRYPTION_SECRET: 00ff\n")

	sealed, err := Seal(key, secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("SIGNING_KEY")) {
		t.Fatal("the plaintext is still readable in the sealed blob")
	}

	// The operator retypes what they wrote down, not the bytes.
	reparsed, err := ParseRecoveryKey(shown)
	if err != nil {
		t.Fatalf("the key as shown could not be read back: %v", err)
	}
	got, err := Open(reparsed, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secret) {
		t.Error("what came out is not what went in")
	}
}

// However they copy it off the screen.
func TestTheKeyIsReadBackHoweverItWasCopied(t *testing.T) {
	key, shown, _ := NewRecoveryKey()
	for _, variant := range []string{
		shown,
		strings.ToLower(shown),
		strings.ReplaceAll(shown, "-", ""),
		strings.ReplaceAll(shown, "-", " "),
		" " + shown + "\n",
	} {
		got, err := ParseRecoveryKey(variant)
		if err != nil {
			t.Errorf("%q: %v", variant, err)
			continue
		}
		if !bytes.Equal(got, key) {
			t.Errorf("%q decoded to a different key", variant)
		}
	}
}

func TestAWrongKeyIsRefusedRatherThanGuessedAt(t *testing.T) {
	key, _, _ := NewRecoveryKey()
	other, _, _ := NewRecoveryKey()
	sealed, _ := Seal(key, []byte("etwas Geheimes"))

	if _, err := Open(other, sealed); err == nil {
		t.Fatal("a wrong key decrypted the blob")
	}
}

// A single flipped byte must be refused, not half-decrypted — the point of GCM.
func TestATamperedArchiveIsRefused(t *testing.T) {
	key, _, _ := NewRecoveryKey()
	sealed, _ := Seal(key, []byte("etwas Geheimes"))
	sealed[len(sealed)-1] ^= 0x01

	if _, err := Open(key, sealed); err == nil {
		t.Fatal("a modified blob was accepted")
	}
}

// Someone will point this at a part that was never sealed. Saying which of the two
// problems it is saves them looking for a key they never had.
func TestUnsealedInputSaysSoInsteadOfBlamingTheKey(t *testing.T) {
	key, _, _ := NewRecoveryKey()
	_, err := Open(key, []byte("plain tar bytes, no magic"))
	if err == nil {
		t.Fatal("unsealed input was accepted")
	}
	if !strings.Contains(err.Error(), "nicht verschlüsselt") {
		t.Errorf("got %q — it should say the part is not encrypted, not that the key is wrong", err)
	}
}

// Four-character groups, because it is copied by eye.
func TestTheKeyIsShownInGroups(t *testing.T) {
	_, shown, _ := NewRecoveryKey()
	for _, group := range strings.Split(shown, "-") {
		if len(group) > 4 {
			t.Fatalf("group %q is longer than four characters: %s", group, shown)
		}
	}
	if !strings.Contains(shown, "-") {
		t.Errorf("no grouping at all: %s", shown)
	}
}
