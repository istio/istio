package proxy

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/golang/glog"
)

// ResolveAddr resolves an authority address to an IP address
func ResolveAddr(addr string) (string, error) {
	if addr == "" {
		return "", nil
	}
	colon := strings.Index(addr, ":")
	host := addr[:colon]
	port := addr[colon:]
	glog.Infof("Attempting to lookup address: %s", host)
	defer glog.Infof("Finished lookup of address: %s", host)
	// lookup the udp address with a timeout of 15 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if lookupErr != nil {
		return "", fmt.Errorf("lookup failed for udp address: %v", lookupErr)
	}
	resolvedAddr := fmt.Sprintf("%s%s", addrs[0].IP, port)
	glog.Infof("Addr resolved to: %s", resolvedAddr)
	return resolvedAddr, nil
}
