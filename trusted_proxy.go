package zentrox

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// SetTrustedProxies configures proxy CIDRs or single IPs.
func (a *App) SetTrustedProxies(values ...string) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure trusted proxies")
	a.trustedProxies = nil
	a.trustAllProxy = false

	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "*" {
			a.trustAllProxy = true
			continue
		}

		if !strings.Contains(raw, "/") {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				panic("SetTrustedProxies: invalid ip " + raw)
			}
			bits := 32
			if ip.Is6() {
				bits = 128
			}
			a.trustedProxies = append(a.trustedProxies, netip.PrefixFrom(ip, bits))
			continue
		}

		p, err := netip.ParsePrefix(raw)
		if err != nil {
			panic("SetTrustedProxies: invalid cidr " + raw)
		}
		a.trustedProxies = append(a.trustedProxies, p.Masked())
	}

	return a
}

func (a *App) isTrustedProxy(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	if a.trustAllProxy {
		return true
	}
	for _, p := range a.trustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func splitHostIP(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}
	}
	return ip
}

func parseHeaderIPs(v string) []netip.Addr {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]netip.Addr, 0, len(parts))
	for _, p := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(p))
		if err == nil {
			out = append(out, ip)
		}
	}
	return out
}

func (a *App) clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	remote := splitHostIP(r.RemoteAddr)
	if !remote.IsValid() {
		return ""
	}

	if !a.isTrustedProxy(remote) {
		return remote.String()
	}

	xff := parseHeaderIPs(r.Header.Get(HeaderXForwardedFor))
	if len(xff) > 0 {
		chain := append(xff, remote)
		for i := len(chain) - 1; i >= 0; i-- {
			if !a.isTrustedProxy(chain[i]) {
				return chain[i].String()
			}
		}
		return chain[0].String()
	}

	if xr := strings.TrimSpace(r.Header.Get(HeaderXRealIP)); xr != "" {
		if ip, err := netip.ParseAddr(xr); err == nil {
			return ip.String()
		}
	}

	return remote.String()
}
