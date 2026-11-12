package service

import (
	"context"
	"net"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/outbound"
)

var monitorDialer = &net.Dialer{Timeout: monitorDialTimeout, KeepAlive: monitorDialKeepAlive}

func isBlockedHostname(hostname string) bool {
	host := strings.ToLower(strings.TrimSuffix(hostname, "."))
	switch host {
	case "", "localhost", "localhost.localdomain", "metadata", "metadata.google.internal", "metadata.goog", "instance-data", "instance-data.ec2.internal":
		return true
	}
	return strings.HasSuffix(host, ".localhost")
}

func isPrivateIP(ip net.IP) bool { return outbound.IsPrivateIP(ip) }

func isPrivateOrLoopbackHost(ctx context.Context, hostname string) (bool, error) {
	if isBlockedHostname(hostname) {
		return true, nil
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return isPrivateIP(ip), nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return false, err
	}
	if len(addrs) == 0 {
		return true, nil
	}
	for _, addr := range addrs {
		if isPrivateIP(addr.IP) {
			return true, nil
		}
	}
	return false, nil
}

func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return outbound.Default().DialContext(ctx, monitorDialer, network, address)
}
