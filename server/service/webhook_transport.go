package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
)

// ユーザー指定の webhook から到達させてはいけない、プライベートおよび
// 特殊用途のネットワーク範囲を明示的に拒否する。
var webhookBlockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func publicWebhookIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	// ネイティブな IPv6 グローバルユニキャストだけを許可し、NAT64 などの
	// 変換用アドレス範囲は除外する。
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range webhookBlockedNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func validateWebhookHost(host string) error {
	name := strings.TrimSuffix(strings.ToLower(host), ".")
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return fmt.Errorf("%w: host must be public", ErrWebhookURLInvalid)
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicWebhookIP(ip) {
		return fmt.Errorf("%w: address must be public", ErrWebhookURLInvalid)
	}
	return nil
}

func newWebhookTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// プロキシを使うとプロキシ側で宛先を解決され、ここでの IP 検査を
	// 回避されるため、プロキシは使用しない。
	transport.Proxy = nil
	dialer := &net.Dialer{
		ControlContext: func(_ context.Context, _ string, address string, _ syscall.RawConn) error {
			return validateWebhookDialAddress(address)
		},
	}
	// Dialer が DNS 解決した接続先を、実際に接続する直前に検査する。
	transport.DialContext = dialer.DialContext
	return transport
}

func validateWebhookDialAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !publicWebhookIP(ip) {
		return fmt.Errorf("%w: address is not public", ErrWebhookURLInvalid)
	}
	return nil
}
