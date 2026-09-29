package preview

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// Host aliases that point into the cluster's service network at an address no Service
// holds (etappe 109).
//
// A hostAlias pins a name to an IP inside the pod, past DNS. ESS setups use it to reach
// their own public hostname through the ingress without leaving the cluster — with the
// ingress controller's ClusterIP written into the configuration. A ClusterIP belongs to
// one cluster. After a move to a new server the configuration still named the old
// Traefik, 10.43.222.87; nothing listened there, and Element Call failed with
// "Unable to create room on SFU" until the address was found by comparing both
// clusters by hand.

// ServiceIP is one Service's ClusterIP.
type ServiceIP struct {
	Namespace, Name, IP string
}

// StaleAlias is a hostAlias whose IP looks like a ClusterIP but belongs to no Service.
type StaleAlias struct {
	Workload  string   `json:"workload"`
	IP        string   `json:"ip"`
	Hostnames []string `json:"hostnames"`
	// Suggest is the ingress controller's ClusterIP, when there is exactly one — the
	// usual target of such an alias. Empty when that is not clear.
	Suggest string `json:"suggest,omitempty"`
	Message string `json:"message"`
}

// ingressControllers are the Service names such an alias usually means.
var ingressControllers = map[string]bool{"traefik": true, "ingress-nginx-controller": true}

// StaleHostAliases checks every hostAlias of the rendered workloads against the
// Services that exist.
//
// "Looks like a ClusterIP" is decided by the cluster itself: an address in the same /16
// as most ClusterIPs. No service CIDR is assumed — k3s, kubeadm and managed clusters use
// different ones — and an alias to anything outside it (a LAN host, a public address)
// is none of this check's business.
func StaleHostAliases(rendered string, services []ServiceIP) []StaleAlias {
	serviceNet := dominantPrefix16(services)
	if serviceNet == "" {
		return nil
	}
	known := map[string]bool{}
	var ingress []ServiceIP
	for _, s := range services {
		known[s.IP] = true
		if ingressControllers[s.Name] {
			ingress = append(ingress, s)
		}
	}

	var out []StaleAlias
	for _, t := range templates(rendered) {
		if t.spec == nil {
			continue
		}
		for _, a := range t.spec.Spec.HostAliases {
			if known[a.IP] || prefix16(a.IP) != serviceNet {
				continue
			}
			s := StaleAlias{Workload: t.name, IP: a.IP, Hostnames: a.Hostnames}
			s.Message = fmt.Sprintf("%s löst %s über einen festen Eintrag auf %s auf — dort gibt es im Cluster keinen Dienst.",
				t.name, strings.Join(a.Hostnames, ", "), a.IP)
			if len(ingress) == 1 {
				s.Suggest = ingress[0].IP
				s.Message += fmt.Sprintf(" Vermutlich ist %s/%s gemeint (%s) — typisch nach einem Umzug, weil eine ClusterIP nur in ihrem Cluster gilt.",
					ingress[0].Namespace, ingress[0].Name, ingress[0].IP)
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workload < out[j].Workload })
	return out
}

func prefix16(ip string) string {
	p := net.ParseIP(ip).To4()
	if p == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d", p[0], p[1])
}

// dominantPrefix16 is the /16 most ClusterIPs share. Headless services ("None") and
// IPv6 addresses do not count.
func dominantPrefix16(services []ServiceIP) string {
	count := map[string]int{}
	best, n := "", 0
	for _, s := range services {
		p := prefix16(s.IP)
		if p == "" {
			continue
		}
		count[p]++
		if count[p] > n {
			best, n = p, count[p]
		}
	}
	return best
}
