/*
Copyright 2026 LedgerForge Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package hooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const MaxResponseBytes = 1024 * 1024

// ValidateURL only permits absolute HTTP(S) destinations without credentials.
func ValidateURL(ctx context.Context, raw string, blockPrivate bool) error {
	return validateURL(ctx, raw, blockPrivate, net.DefaultResolver)
}

func validateURL(ctx context.Context, raw string, blockPrivate bool, resolver *net.Resolver) error {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("webhook URL must be an absolute HTTP(S) URL without credentials or fragment")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("invalid webhook port")
		}
	}
	if blockPrivate {
		_, err := publicAddresses(ctx, u.Hostname(), resolver)
		return err
	}
	return nil
}

// IANA non-global special-purpose ranges (registries updated 2025-10-09):
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
// Translation/tunnel ranges are also denied to prevent embedded IPv4 bypasses.
var nonPublicWebhookRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

// More-specific globally reachable allocations within otherwise non-global
// IETF protocol ranges remain valid destinations.
var publicWebhookExceptions = []netip.Prefix{
	netip.MustParsePrefix("192.0.0.9/32"),
	netip.MustParsePrefix("192.0.0.10/32"),
	netip.MustParsePrefix("2001:1::1/128"),
	netip.MustParsePrefix("2001:1::2/128"),
	netip.MustParsePrefix("2001:1::3/128"),
	netip.MustParsePrefix("2001:3::/32"),
	netip.MustParsePrefix("2001:4:112::/48"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2001:30::/28"),
}

var ipv6GlobalUnicast = netip.MustParsePrefix("2000::/3")

func blockedAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.Zone() != "" || !addr.IsGlobalUnicast() {
		return true
	}
	// Only currently allocated IPv6 global-unicast space is eligible. This also
	// rejects local/reserved space, discard-only, SRv6 and NAT64 prefixes.
	if addr.Is6() && !ipv6GlobalUnicast.Contains(addr) {
		return true
	}
	for _, prefix := range publicWebhookExceptions {
		if prefix.Contains(addr) {
			return false
		}
	}
	for _, prefix := range nonPublicWebhookRanges {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func publicAddresses(ctx context.Context, host string, resolver *net.Resolver) ([]netip.Addr, error) {
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("cannot resolve webhook destination")
	}
	for _, addr := range addrs {
		if blockedAddress(addr) {
			return nil, fmt.Errorf("webhook destination is not public")
		}
	}
	return addrs, nil
}

// HTTPClient bounds delivery time and disables redirects. Private-destination
// enforcement resolves and dials the same addresses, preventing DNS rebinding.
func HTTPClient(base *http.Client, timeoutSec int, blockPrivate bool) *http.Client {
	client := http.Client{}
	if base != nil {
		client = *base
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 120 {
		timeoutSec = 120
	}
	timeout := time.Duration(timeoutSec) * time.Second
	if client.Timeout <= 0 || client.Timeout > timeout {
		client.Timeout = timeout
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if blockPrivate {
		client.Transport = publicWebhookTransport
	}

	return &client
}

var publicWebhookTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy could resolve the target itself and bypass destination enforcement.
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 30 * time.Second, ControlContext: publicWebhookControl}
	transport.DialContext = dialer.DialContext

	return transport
}()

// ControlContext receives the resolved IP immediately before connect(2), so
// registration-time DNS checks cannot be bypassed by a later DNS answer.
func publicWebhookControl(_ context.Context, _ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid webhook destination")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || blockedAddress(addr) {
		return fmt.Errorf("webhook destination is not public")
	}
	return nil
}

// SignRequest authenticates the exact raw body and timestamp. Empty secrets
// produce no authentication headers; configured headers cannot impersonate one.
func SignRequest(req *http.Request, body []byte, secret string) {
	req.Header.Del("X-LedgerForge-Signature")
	req.Header.Del("X-LedgerForge-Timestamp")
	if secret == "" {
		return
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	req.Header.Set("X-LedgerForge-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-LedgerForge-Timestamp", timestamp)
}

// SafeURL excludes query strings and credentials from delivery logs.
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid URL"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimSpace(u.String())
}
