package worker

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"), netip.MustParsePrefix("2001:db8::/32"),
}

func publicIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}
func gatewayClient(ctx context.Context, endpoint string) (*http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid Gateway URL")
	}
	ci := os.Getenv("CI") == "true" && os.Getenv("PUSHGO_CI_ALLOW_LOOPBACK") == "true"
	if u.Scheme != "https" && !ci {
		return nil, errors.New("HTTPS Gateway required")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("Gateway DNS unavailable")
	}
	for _, addr := range addresses {
		if !publicIP(addr) && !(ci && addr.IsLoopback()) {
			return nil, errors.New("Gateway must resolve to public IP addresses")
		}
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	pinned := net.JoinHostPort(addresses[0].String(), port)
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 8 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, pinned)
	}}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
func decryptPassword(encoded, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil || len(raw) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted credential")
	}
	b, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	return string(b), err
}
func (w *Worker) send(ctx context.Context, s settings, j job) (bool, []byte, time.Duration, error) {
	client, err := gatewayClient(ctx, s.Config.GatewayURL)
	if err != nil {
		return false, nil, -1, err
	}
	defer client.CloseIdleConnections()
	password, err := decryptPassword(s.Password, w.EncryptionKey)
	if err != nil {
		return false, nil, -1, errors.New("channel credential cannot be decrypted")
	}
	var body map[string]any
	if json.Unmarshal(j.Payload, &body) != nil {
		return false, nil, -1, errors.New("invalid saved payload")
	}
	body["channel_id"] = s.Config.ChannelID
	body["password"] = password
	body["op_id"] = j.Op
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(s.Config.GatewayURL, "/")+"/message", bytes.NewReader(raw))
	if err != nil {
		return false, nil, -1, err
	}
	req.Header.Set("Authorization", "Bearer "+w.GatewayToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return false, nil, 0, errors.New("Gateway response unknown; retry same operation")
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 65536))
	if err != nil {
		return false, nil, 0, errors.New("Gateway response incomplete")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var ack struct {
			Success bool `json:"success"`
			Data    struct {
				OpID      string `json:"op_id"`
				MessageID string `json:"message_id"`
			} `json:"data"`
		}
		if json.Unmarshal(payload, &ack) == nil && ack.Success && ack.Data.OpID == j.Op && ack.Data.MessageID != "" {
			return true, payload, 0, nil
		}
		return false, nil, 0, errors.New("Gateway acknowledgement invalid; retry same operation")
	}
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		delay := time.Duration(1<<min(j.Attempts+1, 8)) * time.Second
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 && seconds <= 300 {
			delay = time.Duration(seconds) * time.Second
		}
		jitter, _ := rand.Int(rand.Reader, big.NewInt(1000))
		if jitter != nil {
			delay += time.Duration(jitter.Int64()) * time.Millisecond
		}
		return false, nil, delay, errors.New("Gateway temporarily unavailable")
	}
	return false, nil, -1, errors.New("Gateway rejected configuration or request; inspect channel binding")
}
