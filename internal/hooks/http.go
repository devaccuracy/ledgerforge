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
	"time"
)

const MaxResponseBytes = 1024 * 1024

// ValidateURL only permits absolute HTTP(S) destinations without credentials.
func ValidateURL(ctx context.Context, raw string, blockPrivate bool) error {
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
		_, err := publicAddresses(ctx, u.Hostname())
		return err
	}
	return nil
}

func blockedAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast()
}

func publicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
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
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid webhook destination")
		}
		addrs, err := publicAddresses(ctx, host)
		if err != nil {
			return nil, err
		}
		dialer := net.Dialer{Timeout: 30 * time.Second}
		for _, addr := range addrs {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("cannot connect to webhook destination")
	}
	return transport
}()

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
