package reach

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Everything in this file leaves the cluster, which is true of nothing else in
// MatrixCtrl. The handler above it never runs on a timer or a page load.

const (
	// addressService reports the caller's own egress address. The announced RTC
	// hostname is deliberately not used: where it resolves to a proxy or a tunnel —
	// as on the cluster this was built for — testing the hostname tests the proxy
	// and answers a question nobody asked.
	addressService = "https://api.ipify.org"
	// portService checks TCP ports on an arbitrary host.
	portService = "https://portchecker.io/api/v1/query"

	// controlHost is a public resolver whose 443 is open from everywhere. If it
	// comes back closed, the checker is not working and no result from this run can
	// be believed.
	controlHost = "1.1.1.1"
	controlPort = 443

	requestTimeout = 12 * time.Second
)

// Services names what this contacts, so the UI can show it before the click rather
// than in a footnote afterwards.
func Services() []string { return []string{"api.ipify.org", "portchecker.io"} }

// Client performs the outside-in check.
type Client struct {
	http *http.Client
	// addressURL and portURL are fields rather than constants so the tests can
	// point them at a local server. Nothing configures them at runtime: an operator
	// choosing an arbitrary checker would be choosing who receives their address,
	// which is a bigger decision than this feature is worth.
	addressURL string
	portURL    string
}

func NewClient() *Client {
	return &Client{
		http:       &http.Client{Timeout: requestTimeout},
		addressURL: addressService,
		portURL:    portService,
	}
}

// TCPPorts is the subset of a port list this can actually test.
type TCPPorts struct {
	Ports      []PortResult
	UDPSkipped int
}

// SplitByProtocol separates what can be checked from what cannot. The UDP count is
// carried rather than discarded, because the most important port on an RTC
// deployment is UDP and a result that silently omits it invites the reader to
// generalise from the rest.
func SplitByProtocol(ports []PortResult) TCPPorts {
	var out TCPPorts
	for _, p := range ports {
		if p.Protocol == "TCP" {
			p.Status = Unknown
			out.Ports = append(out.Ports, p)
			continue
		}
		out.UDPSkipped++
	}
	return out
}

// Check runs one full measurement: discover the address, verify the checker with a
// control, then test the ports.
func (c *Client) Check(ctx context.Context, ports []PortResult) Result {
	split := SplitByProtocol(ports)
	res := Result{Ports: split.Ports, UDPSkipped: split.UDPSkipped}

	addr, err := c.publicAddress(ctx)
	if err != nil {
		res.Error = "Die eigene öffentliche Adresse konnte nicht ermittelt werden (" + err.Error() + ")."
		return res
	}
	res.Address = addr

	// The control first. If the checker is broken there is no point testing
	// anything, and reporting the ports as closed would be worse than useless.
	control, err := c.checkPorts(ctx, controlHost, []int32{controlPort})
	if err != nil {
		res.Error = "Der Prüfdienst antwortet nicht (" + err.Error() + ")."
		return res
	}
	res.ControlOK = control[controlPort]
	if !res.ControlOK {
		return res
	}

	if len(split.Ports) == 0 {
		return res
	}

	wanted := make([]int32, 0, len(split.Ports))
	for _, p := range split.Ports {
		wanted = append(wanted, p.Port)
	}

	open, err := c.checkPorts(ctx, addr, wanted)
	if err != nil {
		res.Error = "Die Portprüfung ist fehlgeschlagen (" + err.Error() + ")."
		return res
	}

	for i, p := range res.Ports {
		state, tested := open[p.Port]
		switch {
		case !tested:
			// A port the service silently dropped is not a closed port.
			res.Ports[i].Status = Unknown
		case state:
			res.Ports[i].Status = Open
		default:
			res.Ports[i].Status = Closed
		}
	}
	return res
}

func (c *Client) publicAddress(ctx context.Context) (string, error) {
	body, err := c.get(ctx, c.addressURL)
	if err != nil {
		return "", err
	}
	addr := string(bytes.TrimSpace(body))
	if net.ParseIP(addr) == nil {
		return "", fmt.Errorf("unerwartete Antwort")
	}
	return addr, nil
}

// checkPorts returns port → open. A port missing from the map was not tested, which
// the caller must keep distinct from tested-and-closed.
func (c *Client) checkPorts(ctx context.Context, host string, ports []int32) (map[int32]bool, error) {
	payload, err := json.Marshal(map[string]any{"host": host, "ports": ports})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.portURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	return parsePortResponse(body)
}

// parsePortResponse reads the checker's answer. Kept separate from the transport so
// the shape can be tested without a network, and so a service that changes its
// response format fails here — visibly — rather than by reporting every port closed.
func parsePortResponse(body []byte) (map[int32]bool, error) {
	var doc struct {
		Error bool `json:"error"`
		Check []struct {
			Port   int32 `json:"port"`
			Status bool  `json:"status"`
		} `json:"check"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("unlesbare Antwort")
	}
	if doc.Error {
		return nil, fmt.Errorf("der Dienst meldet einen Fehler")
	}
	if len(doc.Check) == 0 {
		return nil, fmt.Errorf("keine Ergebnisse in der Antwort")
	}

	out := make(map[int32]bool, len(doc.Check))
	for _, c := range doc.Check {
		out[c.Port] = c.Status
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<10))
}
