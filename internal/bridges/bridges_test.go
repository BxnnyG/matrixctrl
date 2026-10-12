package bridges

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// A mautrix-style registration, tokens included as every real one has them.
const reg = `id: whatsapp
url: http://mautrix-whatsapp.bridges.svc:29318
as_token: as-secret-0123456789
hs_token: hs-secret-0123456789
sender_localpart: whatsappbot
rate_limited: false
namespaces:
  users:
  - regex: '^@whatsapp_.*:example\.com$'
    exclusive: true
  aliases: []
`

// The tokens let whoever holds them act as every bridged user. They are checked for
// presence and must not appear in anything this package hands out.
func TestARegistrationIsReadWithoutItsTokens(t *testing.T) {
	r, err := Parse([]byte(reg))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "whatsapp" || r.SenderLocalpart != "whatsappbot" || len(r.UserNamespaces) != 1 {
		t.Fatalf("parsed %+v", r)
	}
	out, _ := json.Marshal(r)
	if strings.Contains(string(out), "secret") {
		t.Fatalf("a token left the package: %s", out)
	}
}

func TestARegistrationWithoutTokensIsRefused(t *testing.T) {
	_, err := Parse([]byte("id: x\nurl: http://x\nsender_localpart: bot\n"))
	if err == nil || !strings.Contains(err.Error(), "as_token") {
		t.Fatalf("err = %v", err)
	}
}

func TestReach(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if msg := Reach(context.Background(), "http://"+ln.Addr().String(), nil); msg != "" {
		t.Errorf("a listening address read as unreachable: %s", msg)
	}
	ln.Close()
	if msg := Reach(context.Background(), "http://"+ln.Addr().String(), nil); msg == "" {
		t.Error("a closed address read as reachable")
	}
	if msg := Reach(context.Background(), "", nil); !strings.Contains(msg, "keine Adresse") {
		t.Errorf("empty url: %q", msg)
	}
}
