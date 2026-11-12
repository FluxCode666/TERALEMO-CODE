package repository

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

type imageIPLookup func(context.Context, string, string) ([]net.IP, error)
type imageIPDial func(context.Context, string, string) (net.Conn, error)

// Resolve once and dial that exact public IP. Host and TLS ServerName remain
// attached to the original request hostname, including after redirects.
func publicImageDialContext(lookup imageIPLookup, dial imageIPDial) imageIPDial {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid image download address")
		}
		ips, err := lookup(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("image download DNS resolution failed")
		}
		for _, ip := range ips {
			if outbound.IsPrivateIP(ip) {
				return nil, errors.New("image download address is not public")
			}
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func (s *httpUpstreamService) doPublicImageDownload(req *http.Request, proxyURL string) (*http.Response, error) {
	if strings.TrimSpace(proxyURL) != "" {
		return nil, errors.New("image downloads through proxies are not supported")
	}
	check := func(req *http.Request) error {
		if req.URL == nil || req.URL.User != nil {
			return errors.New("invalid image download URL")
		}
		allowHTTP := s.cfg != nil && s.cfg.Security.URLAllowlist.AllowInsecureHTTP
		_, err := urlvalidator.ValidateURLFormat(req.URL.String(), allowHTTP)
		return err
	}
	if err := check(req); err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext:           publicImageDialContext(net.DefaultResolver.LookupIP, newUpstreamDialer().DialContext),
		TLSHandshakeTimeout:   defaultUpstreamTLSHandshakeTimeout,
		ResponseHeaderTimeout: defaultResponseHeaderTimeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return check(req)
	}}
	return client.Do(req)
}
