package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/bxnnyg/matrixctrl/internal/k8s"
)

type fakeBridgeCluster struct {
	secrets    map[string]map[string][]byte
	configMaps map[string]map[string]string
	pods       []k8s.LabelledPod
}

func (f fakeBridgeCluster) LabelledPods(context.Context, string, string) ([]k8s.LabelledPod, error) {
	return f.pods, nil
}
func (f fakeBridgeCluster) GetConfigMap(_ context.Context, _, name string) (map[string]string, error) {
	if d, ok := f.configMaps[name]; ok {
		return d, nil
	}
	return nil, errors.New("configmaps \"" + name + "\" not found")
}
func (f fakeBridgeCluster) GetSecret(_ context.Context, _, name string) (map[string][]byte, error) {
	if d, ok := f.secrets[name]; ok {
		return d, nil
	}
	return nil, errors.New("secrets \"" + name + "\" not found")
}

func TestBridgesListsRegistrationsWithoutTokens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	reg := "id: telegram\nurl: http://" + ln.Addr().String() + "\nas_token: AS-TOKEN-SECRET\nhs_token: HS-TOKEN-SECRET\nsender_localpart: telegrambot\n"
	h := NewBridgesHandler(fakeBridgeCluster{
		secrets:    map[string]map[string][]byte{"telegram-reg": {"registration.yaml": []byte(reg)}},
		configMaps: map[string]map[string]string{"signal-reg": {"r.yaml": "id: signal\nurl: http://127.0.0.1:1\nas_token: a\nhs_token: b\nsender_localpart: signalbot\n"}},
	}, nil, "ess")
	values := map[string]interface{}{"synapse": map[string]interface{}{"appservices": []interface{}{
		map[string]interface{}{"secret": "telegram-reg", "secretKey": "registration.yaml"},
		map[string]interface{}{"configMap": "signal-reg", "configMapKey": "r.yaml"},
		map[string]interface{}{"secret": "gone", "secretKey": "x"},
	}}}
	got := h.appservices(context.Background(), values)
	if len(got) != 3 {
		t.Fatalf("got %d entries", len(got))
	}
	if got[0].Registration == nil || got[0].Registration.ID != "telegram" || got[0].Reach != "" {
		t.Errorf("telegram: %+v", got[0])
	}
	if got[1].Registration == nil || got[1].Reach == "" {
		t.Errorf("signal should be listed and unreachable: %+v", got[1])
	}
	if got[2].Error == "" || !strings.Contains(got[2].Source, "gone") {
		t.Errorf("a missing Secret must be said, with its name: %+v", got[2])
	}
	out, _ := json.Marshal(got)
	if strings.Contains(string(out), "TOKEN-SECRET") {
		t.Fatalf("a token reached the response: %s", out)
	}
}

// Webhooks arrive under the homeserver's address unless Hookshot has its own host —
// the derivation the chart's own config uses.
func TestHookshotSaysWhereWebhooksArrive(t *testing.T) {
	h := NewBridgesHandler(fakeBridgeCluster{pods: []k8s.LabelledPod{{Name: "ess-hookshot-0", Ready: true, Image: "ghcr.io/matrix-org/matrix-hookshot:7.5.0"}}}, nil, "ess")
	values := map[string]interface{}{
		"serverName": "example.com",
		"synapse":    map[string]interface{}{"ingress": map[string]interface{}{"host": "matrix.example.com"}},
		"hookshot": map[string]interface{}{"enabled": true, "additional": map[string]interface{}{
			"matrixctrl-tasks": map[string]interface{}{"config": "generic:\n  enabled: true\n"}}},
	}
	v := h.hookshot(context.Background(), values)
	if !v.Enabled || v.Bot != "@hookshot:example.com" || v.Version != "7.5.0" || !v.Webhooks || v.Feeds {
		t.Fatalf("view: %+v", v)
	}
	if v.WebhookPrefix != "https://matrix.example.com/_matrix/hookshot/webhook" {
		t.Errorf("prefix %q", v.WebhookPrefix)
	}
}
