package handlers

import (
	"testing"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/dnscheck"
	"github.com/bxnnyg/matrixctrl/internal/tlscheck"
)

// The three real cases of 2026-09-26…28, as verdicts.
func TestVerdictNamesTheRealFaults(t *testing.T) {
	valid := tlscheck.Cert{Reachable: true, NameMatches: true, DaysLeft: 60, Issuer: "Google Trust Services"}
	selfSigned := tlscheck.Cert{Reachable: true, SelfSigned: true, NameMatches: true, DaysLeft: 365, Issuer: "TRAEFIK DEFAULT CERT"}

	cases := []struct {
		name  string
		rep   hostReport
		level string
		says  string
	}{
		{"grau, Traefik-Standardzertifikat",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusOK}, Public: selfSigned},
			"err", "selbstsigniertes Zertifikat"},
		{"orange, Ursprung selbstsigniert (heute kein Fehler)",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusElsewhere}, Public: valid, Proxied: true, Origin: &selfSigned},
			"info", "In Ordnung für Besucher"},
		{"orange, Ursprung selbstsigniert, Zertifikat nie ausgestellt",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusElsewhere}, Public: valid, Proxied: true, Origin: &selfSigned,
				OriginSecret: "ess-synapse-certmanager-tls", OriginSecretMissing: true},
			"info", "Secret ess-synapse-certmanager-tls"},
		{"zeigt auf den alten Server",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusElsewhere, Resolved: []string{"203.0.113.9"}}, Public: valid},
			"warn", "nicht dieser Server"},
		{"kein DNS-Eintrag",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusMissing}, Public: valid},
			"err", "existiert im DNS nicht"},
		{"alles gut, direkt",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusOK}, Public: valid, Origin: &valid},
			"ok", "In Ordnung"},
		{"alles gut, über Cloudflare",
			hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusElsewhere}, Public: valid, Proxied: true, Origin: &valid},
			"ok", "über Cloudflare"},
	}
	for _, c := range cases {
		got, level := verdict(c.rep, true)
		if level != c.level || !contains(got, c.says) {
			t.Errorf("%s: level %q (want %q), text %q (want to contain %q)", c.name, level, c.level, got, c.says)
		}
	}
}

// Without known node addresses, "points elsewhere" must not be claimed: behind NAT the
// server cannot see its own public address, and every correct record would look wrong.
func TestWithoutKnownAddressesNothingIsCalledWrong(t *testing.T) {
	valid := tlscheck.Cert{Reachable: true, NameMatches: true, DaysLeft: 60, Issuer: "CA"}
	rep := hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusElsewhere, Resolved: []string{"203.0.113.9"}}, Public: valid}
	if _, level := verdict(rep, false); level != "ok" {
		t.Errorf("level %q — an unknown own address is not a fault of the record", level)
	}
}

func TestAnExpiringCertificateIsAWarning(t *testing.T) {
	soon := tlscheck.Cert{Reachable: true, NameMatches: true, DaysLeft: 3, Issuer: "CA", NotAfter: time.Now().Add(72 * time.Hour)}
	if _, level := verdict(hostReport{DNS: dnscheck.Result{Status: dnscheck.StatusOK}, Public: soon}, true); level != "warn" {
		t.Errorf("level %q", level)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfStr(s, sub) >= 0)
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
