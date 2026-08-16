package synapse

import "testing"

// Room IDs are not identifiers made of letters. `!` and `:` are part of them, and a
// path built by concatenation works on every example anyone types by hand and fails
// on the first real room.
func TestRoomPathEscapesRealIDs(t *testing.T) {
	cases := []struct{ in, want string }{
		{"!AbCdEfGh:example.org", "%21AbCdEfGh:example.org"},
		// A localpart with characters that must not end the path segment.
		{"!a/b:example.org", "%21a%2Fb:example.org"},
		{"!with%percent:example.org", "%21with%25percent:example.org"},
	}
	for _, tc := range cases {
		if got := roomPath(tc.in); got != tc.want {
			t.Errorf("roomPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The members endpoint returns everything at once, so the page boundaries are ours.
// These are the cases where "ours" goes wrong: a page starting past the end, a page
// running off the end, and a limit nobody set.
func TestMemberPaging(t *testing.T) {
	all := []string{"@a:x", "@b:x", "@c:x", "@d:x", "@e:x"}

	cases := []struct {
		name       string
		from, size int
		want       []string
		wantOffset int
	}{
		{"first page", 0, 2, []string{"@a:x", "@b:x"}, 0},
		{"middle", 2, 2, []string{"@c:x", "@d:x"}, 2},
		{"runs off the end", 4, 10, []string{"@e:x"}, 4},
		{"starts past the end", 99, 10, []string{}, 99},
		{"negative from is the first page", -5, 2, []string{"@a:x", "@b:x"}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sliceMembers(all, tc.from, tc.size)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// A page that starts past the end must be an empty list, never nil: the frontend
// checks `.length`, and nil marshals to JSON null (E36 hit this on the room list).
func TestMemberSliceIsNeverNil(t *testing.T) {
	if got := sliceMembers(nil, 0, 10); got == nil {
		t.Error("sliceMembers returned nil for an empty room")
	}
	if got := sliceMembers([]string{"@a:x"}, 50, 10); got == nil {
		t.Error("sliceMembers returned nil for a page past the end")
	}
}
