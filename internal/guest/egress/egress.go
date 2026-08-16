// Package egress allows the guest agent to reach only configured LLM APIs.
package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var allowedHosts = map[string]struct{}{
	"api.x.ai":          {},
	"api.openai.com":    {},
	"api.anthropic.com": {},
}

var dnsServers = []string{"1.1.1.1:53", "8.8.8.8:53"}

func Allowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	_, ok := allowedHosts[host]
	return ok
}

func ConfigureGuestResolver() error {
	_ = os.MkdirAll("/etc", 0o755)
	body := "nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions ndots:1\n"
	if err := os.WriteFile("/etc/resolv.conf", []byte(body), 0o644); err != nil {
		return err
	}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			var last error
			for _, srv := range dnsServers {
				c, err := d.DialContext(ctx, "tcp4", srv)
				if err == nil {
					return c, nil
				}
				last = err
			}
			return nil, last
		},
	}
	return nil
}

func Transport() *http.Transport {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if !Allowed(host) {
				return nil, fmt.Errorf("egress denied: %s is not an allowed LLM endpoint", host)
			}
			if port != "443" {
				return nil, fmt.Errorf("egress denied: only HTTPS :443 is allowed")
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
			if err != nil {
				return nil, fmt.Errorf("resolve %s: %w", host, err)
			}
			var last error
			for _, ip := range ips {
				c, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), port))
				if err == nil {
					return c, nil
				}
				last = err
			}
			if last == nil {
				last = fmt.Errorf("no IPv4 addresses for %s", host)
			}
			return nil, last
		},
	}
}

func Client() *http.Client {
	return &http.Client{
		Timeout:   5 * time.Minute,
		Transport: Transport(),
	}
}
