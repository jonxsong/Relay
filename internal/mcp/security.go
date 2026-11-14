package relaymcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "fc00::/7", "fe80::/10", "2001:db8::/32", "ff00::/8",
)

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

func ValidateEndpoint(raw string, allowPrivate bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("invalid MCP endpoint")
	}
	if endpoint.Scheme != "https" && !(allowPrivate && endpoint.Scheme == "http") {
		return nil, errors.New("MCP endpoint must use HTTPS")
	}
	if endpoint.Hostname() == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, errors.New("MCP endpoint must have a host and no credentials, query, or fragment")
	}
	hostname := strings.TrimSuffix(strings.ToLower(endpoint.Hostname()), ".")
	if !allowPrivate && (hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".local")) {
		return nil, errors.New("MCP endpoint resolves to a local name")
	}
	if address, err := netip.ParseAddr(hostname); err == nil && !allowPrivate && isBlockedAddress(address) {
		return nil, errors.New("MCP endpoint uses a non-public address")
	}
	return endpoint, nil
}

func isBlockedAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func safeHTTPClient(connection Connection) (*http.Client, error) {
	_, err := ValidateEndpoint(connection.Endpoint, connection.AllowPrivateNetwork)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.MaxResponseHeaderBytes = 64 << 10
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid MCP network address")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve MCP host: %w", err)
		}
		for _, candidate := range addresses {
			if !candidate.IsValid() || (!connection.AllowPrivateNetwork && isBlockedAddress(candidate)) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		}
		return nil, errors.New("MCP host has no permitted addresses")
	}
	var roundTripper http.RoundTripper = transport
	if len(connection.Headers) > 0 {
		roundTripper = headerTransport{next: roundTripper, headers: connection.Headers}
	}
	return &http.Client{
		Transport: roundTripper,
		Timeout:   connection.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

type headerTransport struct {
	next    http.RoundTripper
	headers http.Header
}

func (t headerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	for name, values := range t.headers {
		for _, value := range values {
			clone.Header.Add(name, value)
		}
	}
	return t.next.RoundTrip(clone)
}

func endpointMatches(connection Connection, requestURL *url.URL) bool {
	endpoint, err := url.Parse(connection.Endpoint)
	return err == nil && strings.EqualFold(endpoint.Scheme, requestURL.Scheme) && strings.EqualFold(endpoint.Host, requestURL.Host)
}
