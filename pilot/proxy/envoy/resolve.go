package envoy

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/golang/glog"
)

func resolveStatsdAddr(statsdAdrr string) (string, error) {
	if statsdAdrr == "" {
		return "", nil
	}
	colon := strings.Index(statsdAdrr, ":")
	host := statsdAdrr[:colon]
	port := statsdAdrr[colon:]
	glog.Infof("Attempting to lookup statsd address: %s", host)
	defer glog.Infof("Finished lookup of statsd address: %s", host)
	// lookup the statsd udp address with a timeout of 15 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	addrs, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
	if lookupErr != nil {
		return "", fmt.Errorf("lookup failed for statsd udp address: %v", lookupErr)
	}
	resolvedAddr := fmt.Sprintf("%s%s", addrs[0].IP, port)
	glog.Infof("Statsd Addr: %s", resolvedAddr)
	return resolvedAddr, nil
}
