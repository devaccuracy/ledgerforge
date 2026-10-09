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
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWebhookSpecialPurposeDestinationsBlocked(t *testing.T) {
	blocked := []string{
		"0.1.2.3", "10.1.2.3", "100.64.0.0", "100.100.100.200", "100.127.255.255", "127.0.0.2", "169.254.169.254", "172.16.1.1",
		"192.0.0.1", "192.0.0.8", "192.0.0.170", "192.0.2.1", "192.88.99.1", "192.88.99.2", "192.168.1.1", "198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "::ffff:100.100.100.200", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe", "64:ff9b::808:808", "64:ff9b:1::1", "100::1", "100:0:0:1::1",
		"2001::1", "2001:2::1", "2001:10::1", "2001:db8::1", "2002:a9fe:a9fe::1", "3fff::1", "5f00::1", "fc00::1", "fe80::1", "fec0::1", "ff02::1",
	}
	for _, address := range blocked {
		t.Run(address, func(t *testing.T) {
			require.True(t, blockedAddress(netip.MustParseAddr(address)))
			target := "http://" + net.JoinHostPort(address, "80") + "/hook"
			require.ErrorContains(t, ValidateURL(context.Background(), target, true), "not public")
			require.ErrorContains(t, publicWebhookControl(context.Background(), "tcp", net.JoinHostPort(address, "80"), nil), "not public")
			require.NoError(t, ValidateURL(context.Background(), target, false), "compatibility option remains off by default")
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "100.63.255.255", "100.128.0.0", "192.0.0.9", "192.0.0.10", "192.31.196.1", "192.52.193.1", "192.175.48.1", "2001:4860:4860::8888", "2606:4700:4700::1111", "2001:1::1", "2001:1::2", "2001:1::3", "2001:3::1", "2001:4:112::1", "2001:20::1", "2001:30::1", "2620:4f:8000::1"} {
		require.False(t, blockedAddress(netip.MustParseAddr(address)), address)
		require.NoError(t, publicWebhookControl(context.Background(), "tcp", net.JoinHostPort(address, "443"), nil), address)
	}
}

func TestWebhookDialerBlocksLiteralCloudMetadataBeforeConnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := publicWebhookTransport.DialContext(ctx, "tcp", "100.100.100.200:80")
	if conn != nil {
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
	}
	require.ErrorContains(t, err, "not public", "dial-time enforcement must reject the reviewer's metadata address")
}

// The DNS server switches from a public answer at validation to loopback at
// connection time. A successful registration must not authorize that socket.
func TestWebhookDNSRebindingBlockedAtConnect(t *testing.T) {
	dns, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	var mu sync.Mutex
	private := false
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 512)
		for {
			n, peer, err := dns.ReadFrom(buf)
			if err != nil {
				return
			}
			request := append([]byte(nil), buf[:n]...)
			pos := 12
			for pos < len(request) && request[pos] != 0 {
				pos += 1 + int(request[pos])
			}
			if pos+5 > len(request) {
				continue
			}
			end := pos + 5
			qtype := binary.BigEndian.Uint16(request[pos+1 : pos+3])
			response := append([]byte(nil), request[:end]...)
			binary.BigEndian.PutUint16(response[2:4], 0x8180)
			binary.BigEndian.PutUint16(response[6:8], 0)
			binary.BigEndian.PutUint16(response[8:10], 0)
			binary.BigEndian.PutUint16(response[10:12], 0)
			if qtype == 1 {
				binary.BigEndian.PutUint16(response[6:8], 1)
				mu.Lock()
				isPrivate := private
				mu.Unlock()
				ip := []byte{8, 8, 8, 8}
				if isPrivate {
					ip = []byte{127, 0, 0, 1}
				}
				response = append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0, 0, 4)
				response = append(response, ip...)
			}
			_, _ = dns.WriteTo(response, peer)
		}
	}()
	t.Cleanup(func() {
		err := dns.Close()
		wg.Wait()
		require.NoError(t, err)
	})
	resolver := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", dns.LocalAddr().String())
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, validateURL(ctx, "http://rebind.test/hook", true, resolver))
	mu.Lock()
	private = true
	mu.Unlock()
	dialer := net.Dialer{Resolver: resolver, ControlContext: publicWebhookControl}
	conn, err := dialer.DialContext(ctx, "tcp", "rebind.test:80")
	if conn != nil {
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
	}
	require.ErrorContains(t, err, "not public")
}

func TestWebhookSpecialPurposeRejectedOnHookCreateAndUpdate(t *testing.T) {
	manager := securityManager(t)
	originalURL := "https://example.com/hook"
	hook := &Hook{ID: "id", URL: originalURL, Type: PreTransaction}
	require.NoError(t, manager.RegisterHook(context.Background(), hook))
	manager.config.Notification.Webhook.BlockPrivateDestinations = true
	for _, address := range []string{"100.100.100.200", "198.18.0.1", "2001:db8::1", "64:ff9b::a9fe:a9fe"} {
		target := "http://" + net.JoinHostPort(address, "80") + "/hook"
		replacement := &Hook{URL: target, Type: PreTransaction}
		require.Error(t, manager.RegisterHook(context.Background(), replacement))
		require.Error(t, manager.UpdateHook(context.Background(), "id", replacement))
		stored, err := manager.GetHook(context.Background(), "id")
		require.NoError(t, err)
		require.Equal(t, originalURL, stored.URL)
	}
}
