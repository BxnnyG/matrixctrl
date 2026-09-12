package auth

import "testing"

// Nobody named means nobody loses anything. An upgrade that silently demoted every
// existing operator to read-only would be a worse failure than the one this fixes.
func TestNoNamesMeansEverybodyMayWrite(t *testing.T) {
	for _, list := range []string{"", "   ", ",", " , "} {
		r := ParseRoles(list)
		if r.Restricted() {
			t.Errorf("%q: restricted with nobody named", list)
		}
		if !r.MayWrite("01KSPV9ZMR7NB4B2BBWMPYSD1P") || !r.MayWrite(BootstrapUserID) {
			t.Errorf("%q: somebody was refused although no list exists", list)
		}
	}
}

func TestNamingSomebodyExcludesEverybodyElse(t *testing.T) {
	r := ParseRoles(" 01ADMIN , 01SECOND ")
	if !r.Restricted() {
		t.Fatal("a list with names in it is a restriction")
	}
	for _, id := range []string{"01ADMIN", "01SECOND"} {
		if !r.MayWrite(id) {
			t.Errorf("%s was named and refused", id)
		}
	}
	if r.MayWrite("01MODERATOR") {
		t.Error("a Matrix admin who was not named must not be able to change anything — " +
			"that is the entire point: moderating rooms is not administering the server")
	}
}

// A ULID is twenty-six characters of no pattern, and it is typed into a values file.
// Getting it wrong must not cost the installation.
func TestTheLocalAdminCanNeverBeLockedOut(t *testing.T) {
	r := ParseRoles("01TYPOOOOOOOOOOOOOOOOOOOOO")
	if !r.MayWrite(BootstrapUserID) {
		t.Error("the local admin was excluded by a list it may have mistyped itself")
	}
}
