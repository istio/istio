//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package client

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/proto"
	"istio.io/istio/pkg/test/framework/resource"
)

var (
	requestIDFieldRegex      = regexp.MustCompile("(?i)" + string(response.RequestIDField) + "=(.*)")
	serviceVersionFieldRegex = regexp.MustCompile(string(response.ServiceVersionField) + "=(.*)")
	servicePortFieldRegex    = regexp.MustCompile(string(response.ServicePortField) + "=(.*)")
	statusCodeFieldRegex     = regexp.MustCompile(string(response.StatusCodeField) + "=(.*)")
	hostFieldRegex           = regexp.MustCompile(string(response.HostField) + "=(.*)")
	hostnameFieldRegex       = regexp.MustCompile(string(response.HostnameField) + "=(.*)")
	responseHeaderFieldRegex = regexp.MustCompile(string(response.ResponseHeader) + "=(.*)")
	URLFieldRegex            = regexp.MustCompile(string(response.URLField) + "=(.*)")
	ClusterFieldRegex        = regexp.MustCompile(string(response.ClusterField) + "=(.*)")
	IPFieldRegex             = regexp.MustCompile(string(response.IPField) + "=(.*)")
)

// ParsedResponse represents a response to a single echo request.
type ParsedResponse struct {
	// Body is the body of the response
	Body string
	// ID is a unique identifier of the resource in the response
	ID string
	// URL is the url the request is sent to
	URL string
	// Version is the version of the resource in the response
	Version string
	// Port is the port of the resource in the response
	Port string
	// Code is the response code
	Code string
	// Host is the host called by the request
	Host string
	// Hostname is the host that responded to the request
	Hostname string
	// The cluster where the server is deployed.
	Cluster string
	// IP is the requester's ip address
	IP string
	// RawResponse gives a map of all values returned in the response (headers, etc)
	RawResponse map[string]string
}

// IsOK indicates whether or not the code indicates a successful request.
func (r *ParsedResponse) IsOK() bool {
	return r.Code == response.StatusCodeOK
}

// Count occurrences of the given text within the body of this response.
func (r *ParsedResponse) Count(text string) int {
	return strings.Count(r.Body, text)
}

func (r *ParsedResponse) String() string {
	out := ""
	out += fmt.Sprintf("Body:     %s\n", r.Body)
	out += fmt.Sprintf("ID:       %s\n", r.ID)
	out += fmt.Sprintf("URL:      %s\n", r.URL)
	out += fmt.Sprintf("Version:  %s\n", r.Version)
	out += fmt.Sprintf("Port:     %s\n", r.Port)
	out += fmt.Sprintf("Code:     %s\n", r.Code)
	out += fmt.Sprintf("Host:     %s\n", r.Host)
	out += fmt.Sprintf("Hostname: %s\n", r.Hostname)
	out += fmt.Sprintf("Cluster:  %s\n", r.Cluster)
	out += fmt.Sprintf("IP:       %s\n", r.IP)

	return out
}

// ParsedResponses is an ordered list of parsed response objects.
type ParsedResponses []*ParsedResponse

// Len returns the length of the parsed responses.
func (r ParsedResponses) Len() int {
	return len(r)
}

func (r ParsedResponses) Check(check func(int, *ParsedResponse) error) (err error) {
	if r.Len() == 0 {
		return fmt.Errorf("no responses received")
	}

	for i, resp := range r {
		if e := check(i, resp); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return
}

func (r ParsedResponses) CheckOrFail(t test.Failer, check func(int, *ParsedResponse) error) ParsedResponses {
	t.Helper()
	if err := r.Check(check); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckOK() error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if !response.IsOK() {
			return fmt.Errorf("response[%d] Status Code: %s", i, response.Code)
		}
		return nil
	})
}

func (r ParsedResponses) CheckOKOrFail(t test.Failer) ParsedResponses {
	t.Helper()
	if err := r.CheckOK(); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckCode(expected string) error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.Code != expected {
			return fmt.Errorf("expected response code %s, got %q", expected, response.Code)
		}
		return nil
	})
}

func (r ParsedResponses) CheckCodeOrFail(t test.Failer, expected string) ParsedResponses {
	t.Helper()
	if err := r.CheckCode(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckHost(expected string) error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.Host != expected {
			return fmt.Errorf("response[%d] Host: expected %s, received %s", i, expected, response.Host)
		}
		return nil
	})
}

func (r ParsedResponses) CheckHostOrFail(t test.Failer, expected string) ParsedResponses {
	t.Helper()
	if err := r.CheckHost(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckPort(expected int) error {
	expectedStr := strconv.Itoa(expected)
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.Port != expectedStr {
			return fmt.Errorf("response[%d] Port: expected %s, received %s", i, expectedStr, response.Port)
		}
		return nil
	})
}

func (r ParsedResponses) CheckPortOrFail(t test.Failer, expected int) ParsedResponses {
	t.Helper()
	if err := r.CheckPort(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) clusterDistribution() map[string]int {
	hits := map[string]int{}
	for _, rr := range r {
		hits[rr.Cluster]++
	}
	return hits
}

// CheckReachedClusters returns an error if there wasn't at least one response from each of the given clusters.
// This can be used in combination with echo.Instances.Clusters(), for example:
//     echoA[0].CallOrFail(t, ...).CheckReachedClusters(echoB.Clusters())
func (r ParsedResponses) CheckReachedClusters(clusters resource.Clusters) error {
	hits := r.clusterDistribution()
	exp := map[string]struct{}{}
	for _, expCluster := range clusters {
		exp[expCluster.Name()] = struct{}{}
		if hits[expCluster.Name()] == 0 {
			return fmt.Errorf("did not reach all of %v, got %v", clusters, hits)
		}
	}
	for hitCluster := range hits {
		if _, ok := exp[hitCluster]; !ok {
			return fmt.Errorf("reached cluster not in %v, got %v", clusters, hits)
		}
	}
	return nil
}

// CheckEqualClusterTraffic checks that traffic was equally distributed across the given clusters, allowing some percent error.
// For example, with 100 requests and 20 percent error, each cluster must given received 20±4 responses. Only the passed
// in clusters will be validated.
func (r ParsedResponses) CheckEqualClusterTraffic(clusters resource.Clusters, precisionPct int) error {
	clusterHits := r.clusterDistribution()
	expected := len(r) / len(clusters)
	precision := int(float32(expected) * (float32(precisionPct) / 100))
	for _, hits := range clusterHits {
		if !almostEquals(hits, expected, precision) {
			return fmt.Errorf("requests were not equally distributed across clusters: %v", clusterHits)
		}
	}
	return nil
}

func almostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}

func (r ParsedResponses) CheckCluster(expected string) error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.Cluster != expected {
			return fmt.Errorf("response[%d] Cluster: expected %s, received %s", i, expected, response.Cluster)
		}
		return nil
	})
}

func (r ParsedResponses) CheckClusterOrFail(t test.Failer, expected string) ParsedResponses {
	t.Helper()
	if err := r.CheckCluster(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckIP(expected string) error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.IP != expected {
			return fmt.Errorf("response[%d] IP: expected %s, received %s", i, expected, response.IP)
		}
		return nil
	})
}

func (r ParsedResponses) CheckIPOrFail(t test.Failer, expected string) ParsedResponses {
	t.Helper()
	if err := r.CheckIP(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

// Count occurrences of the given text within the bodies of all responses.
func (r ParsedResponses) Count(text string) int {
	count := 0
	for _, c := range r {
		count += c.Count(text)
	}
	return count
}

// Match returns a subset of ParsedResponses that match the given predicate.
func (r ParsedResponses) Match(f func(r *ParsedResponse) bool) ParsedResponses {
	var matched []*ParsedResponse
	for _, rr := range r {
		if f(rr) {
			matched = append(matched, rr)
		}
	}
	return matched
}

func (r ParsedResponses) String() string {
	out := ""
	for i, resp := range r {
		out += fmt.Sprintf("Response[%d]:\n%s", i, resp.String())
	}
	return out
}

func ParseForwardedResponse(resp *proto.ForwardEchoResponse) ParsedResponses {
	responses := make([]*ParsedResponse, len(resp.Output))
	for i, output := range resp.Output {
		responses[i] = parseResponse(output)
	}
	return responses
}

func parseResponse(output string) *ParsedResponse {
	out := ParsedResponse{
		Body: output,
	}

	match := requestIDFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.ID = match[1]
	}

	match = serviceVersionFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Version = match[1]
	}

	match = servicePortFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Port = match[1]
	}

	match = statusCodeFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Code = match[1]
	}

	match = hostFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Host = match[1]
	}

	match = hostnameFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Hostname = match[1]
	}

	match = URLFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.URL = match[1]
	}

	match = ClusterFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Cluster = match[1]
	}

	match = IPFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.IP = match[1]
	}

	out.RawResponse = map[string]string{}

	matches := responseHeaderFieldRegex.FindAllStringSubmatch(output, -1)
	for _, kv := range matches {
		sl := strings.Split(kv[1], ":")
		if len(sl) != 2 {
			continue
		}
		out.RawResponse[sl[0]] = sl[1]
	}

	for _, l := range strings.Split(output, "\n") {
		prefixSplit := strings.Split(l, "body] ")
		if len(prefixSplit) != 2 {
			continue
		}
		kv := strings.SplitN(prefixSplit[1], "=", 2)
		if len(kv) != 2 {
			continue
		}
		out.RawResponse[kv[0]] = kv[1]
	}

	return &out
}
