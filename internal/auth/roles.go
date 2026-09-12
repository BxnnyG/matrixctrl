package auth

import "strings"

// BootstrapUserID is the local admin's identity in a session token. Exported because
// the write gate has to know that this one account can never be locked out of its own
// panel by a list it may have typed wrong.
const BootstrapUserID = bootstrapUserID

// Roles answers one question: may this session change anything?
//
// Until now there was exactly one role. Everyone who could sign in could do everything
// — deploy, upgrade, rewrite the configuration, restore an archive over the database,
// deactivate accounts. That was defensible while signing in meant knowing the local
// admin password. It stopped being defensible in etappe 86, when MatrixCtrl learned to
// create MAS admin accounts: with `requireAdmin`, every Matrix admin can sign in here,
// so adding someone to moderate rooms silently handed them the homeserver.
//
// The shape follows the one that already exists for access: `oidc.allowedUsers` is a
// list of MAS user IDs in the chart values, and so is this.
type Roles struct {
	// admins is empty when nobody has been named, which means everybody is one.
	//
	// That default is deliberate: an upgrade must not change who can do what. A
	// deployment that has never heard of this setting keeps behaving exactly as before,
	// and the restriction begins the moment an operator names the first admin.
	admins map[string]bool
}

// ParseRoles reads a comma-separated list of user IDs.
func ParseRoles(list string) *Roles {
	r := &Roles{admins: map[string]bool{}}
	for _, part := range strings.Split(list, ",") {
		if id := strings.TrimSpace(part); id != "" {
			r.admins[id] = true
		}
	}
	return r
}

// Restricted reports whether anyone has been named at all.
func (r *Roles) Restricted() bool { return r != nil && len(r.admins) > 0 }

// MayWrite reports whether this user may change anything.
func (r *Roles) MayWrite(userID string) bool {
	if !r.Restricted() {
		return true
	}
	// The local admin is never excluded. Getting the list wrong is easy — a ULID is
	// twenty-six characters of no pattern — and the cost of that mistake must not be
	// an installation nobody can administer. This is the same reasoning that put the
	// bootstrap password in the release Secret (§4.74).
	if userID == BootstrapUserID {
		return true
	}
	return r.admins[userID]
}
