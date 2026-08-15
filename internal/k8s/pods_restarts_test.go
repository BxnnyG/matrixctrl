package k8s

import "testing"

// The rules here are all "when do we stay silent", because a wrong attribution is
// worse than none: it invents a culprit, and the operator acts on it.
func TestDominantContributor(t *testing.T) {
	cases := []struct {
		name  string
		by    map[string]int32
		total int32
		want  string
	}{
		{
			// The case this exists for. ess-postgres-0 on 2026-08-15: the number
			// read 42 and the database had restarted zero times.
			name:  "sidecar carries all of it",
			by:    map[string]int32{"postgres": 0, "postgres-ess-updater": 0, "postgres-exporter": 42},
			total: 42,
			want:  "postgres-exporter",
		},
		{
			name:  "single container names nothing the row does not already say",
			by:    map[string]int32{"synapse": 7},
			total: 7,
			want:  "",
		},
		{
			name:  "nothing to attribute",
			by:    map[string]int32{"a": 0, "b": 0},
			total: 0,
			want:  "",
		},
		{
			// Genuinely "the pod". Picking one of three equals would be a guess
			// dressed as a finding.
			name:  "evenly spread stays silent",
			by:    map[string]int32{"a": 14, "b": 14, "c": 14},
			total: 42,
			want:  "",
		},
		{
			// 22 of 42 is a majority but not a dominant one, and the other 20 matter
			// just as much. Naming only the first would hide half the problem.
			name:  "bare majority is not dominance",
			by:    map[string]int32{"a": 22, "b": 20},
			total: 42,
			want:  "",
		},
		{
			name:  "exactly two thirds qualifies",
			by:    map[string]int32{"a": 20, "b": 10},
			total: 30,
			want:  "a",
		},
		{
			name:  "just under two thirds does not",
			by:    map[string]int32{"a": 19, "b": 11},
			total: 30,
			want:  "",
		},
		{
			// Map iteration is randomised. Without a deterministic tie-break the
			// answer would flicker between two identical reads, which reads to an
			// operator as something changing.
			name:  "ties resolve by name, not by map order",
			by:    map[string]int32{"zeta": 30, "alpha": 30},
			total: 30,
			want:  "alpha",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 20; i++ {
				if got := DominantContributor(tc.by, tc.total); got != tc.want {
					t.Fatalf("DominantContributor() = %q, want %q", got, tc.want)
				}
			}
		})
	}
}
