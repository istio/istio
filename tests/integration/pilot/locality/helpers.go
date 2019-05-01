package locality

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/apps"
)

func SendTraffic(t *testing.T, duration time.Duration, from apps.KubeApp, to string, hostnameMatcher *regexp.Regexp) {
	timeout := time.After(duration)
	totalRequests := 0
	hostnameFailures := map[string]int{}
	errorFailures := map[string]int{}
	for {
		select {
		case <-timeout:
			if totalRequests < 1 {
				t.Error("No requests made.")
			}
			if len(hostnameFailures) > 0 {
				t.Errorf("Total requests: %v, requests made to service unexpected services: %v.", totalRequests, hostnameFailures)
			}
			if len(errorFailures) > 0 {
				t.Errorf("Total requests: %v, requests failed: %v.", totalRequests, errorFailures)
			}
			return
		default:
			headers := http.Header{}
			headers.Add("Host", to)
			// This is a hack to remain infrastructure agnostic when running these tests
			// We actually call the host set above not the endpoint we pass
			resp, err := from.Call(from.EndpointForPort(80), apps.AppCallOptions{
				Protocol: apps.AppProtocolHTTP,
				Headers:  headers,
			})
			totalRequests++
			for _, r := range resp {
				if match := hostnameMatcher.FindString(r.Hostname); len(match) == 0 {
					hostnameFailures[r.Hostname]++
				}
			}
			if err != nil {
				errorFailures[fmt.Sprintf("send to %v failed: %v", to, err)]++
			}
		}
	}
}
