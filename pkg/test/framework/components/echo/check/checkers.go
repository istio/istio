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

package check

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc/codes"

	"istio.io/istio/pkg/config/protocol"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/util/istiomultierror"
)

// Each applies the given per-response function across all responses.
func Each(v Visitor) echo.Checker {
	return v.Checker()
}

// And is an aggregate Checker that requires all Checkers succeed. Any nil Checkers are ignored.
func And(checkers ...echo.Checker) echo.Checker {
	return func(result echo.CallResult, err error) error {
		for _, c := range filterNil(checkers) {
			if err := c(result, err); err != nil {
				return err
			}
		}
		return nil
	}
}

// Or is an aggregate Checker that requires at least one Checker succeeds.
// Note: the checkers must succeed for all requests; there is no per-request Or().
func Or(checkers ...echo.Checker) echo.Checker {
	return func(result echo.CallResult, err error) error {
		out := istiomultierror.New()
		for idx, c := range checkers {
			err := c(result, err)
			if err == nil {
				return nil
			}
			out = multierror.Append(out, fmt.Errorf("failed Or() index %d: %v", idx, err))
		}
		return out.ErrorOrNil()
	}
}

func filterNil(checkers []echo.Checker) []echo.Checker {
	var out []echo.Checker
	for _, c := range checkers {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// NoError is similar to echo.NoChecker, but provides additional context information.
func NoError() echo.Checker {
	return func(_ echo.CallResult, err error) error {
		if err != nil {
			return fmt.Errorf("expected no error, but encountered %v", err)
		}
		return nil
	}
}

// Error provides a checker that returns an error if the call succeeds.
func Error() echo.Checker {
	return func(_ echo.CallResult, err error) error {
		if err == nil {
			return errors.New("expected error, but none occurred")
		}
		return nil
	}
}

// ErrorContains is similar to Error, but checks that the error message contains the given string.
func ErrorContains(expected string) echo.Checker {
	return func(_ echo.CallResult, err error) error {
		if err == nil {
			return errors.New("expected error, but none occurred")
		}
		if !strings.Contains(err.Error(), expected) {
			return fmt.Errorf("expected error to contain %s: %v", expected, err)
		}
		return nil
	}
}

func ErrorOrStatus(expected int) echo.Checker {
	return Or(Error(), Status(expected))
}

func ErrorOrNotStatus(expected int) echo.Checker {
	return Or(Error(), NotStatus(expected))
}

// OK is shorthand for NoErrorAndStatus(200).
func OK() echo.Checker {
	return NoErrorAndStatus(http.StatusOK)
}

// NotOK is shorthand for ErrorOrNotStatus(http.StatusOK).
func NotOK() echo.Checker {
	return ErrorOrNotStatus(http.StatusOK)
}

// NoErrorAndStatus is checks that no error occurred and that the returned status code matches the expected
// value.
func NoErrorAndStatus(expected int) echo.Checker {
	return And(NoError(), Status(expected))
}

// Status checks that the response status code matches the expected value. If the expected value is zero,
// checks that the response code is unset.
func Status(expected int) echo.Checker {
	return Each(VStatus(expected))
}

// NotStatus checks that the response status code does not match the expected value.
func NotStatus(expected int) echo.Checker {
	return Each(VNotStatus(expected))
}

// VStatus is a Visitor-based version of Status.
func VStatus(expected int) Visitor {
	expectedStr := ""
	if expected > 0 {
		expectedStr = strconv.Itoa(expected)
	}
	return func(r echoClient.Response) error {
		if r.Code != expectedStr {
			return fmt.Errorf("expected response code `%s`, got %q. Response: %s", expectedStr, r.Code, r)
		}
		return nil
	}
}

// VNotStatus is a Visitor-based version of NotStatus.
func VNotStatus(notExpected int) Visitor {
	notExpectedStr := ""
	if notExpected > 0 {
		notExpectedStr = strconv.Itoa(notExpected)
	}
	return func(r echoClient.Response) error {
		if r.Code == notExpectedStr {
			return fmt.Errorf("received unexpected response code `%s`. Response: %s", notExpectedStr, r)
		}
		return nil
	}
}

// GRPCStatus checks that the gRPC response status code matches the expected value.
func GRPCStatus(expected codes.Code) echo.Checker {
	return func(result echo.CallResult, err error) error {
		if expected == codes.OK {
			if err != nil {
				return fmt.Errorf("unexpected error: %w", err)
			}
			return nil
		}
		if err == nil {
			return fmt.Errorf("expected gRPC error with status %s, but got OK", expected.String())
		}
		expectedSubstr := fmt.Sprintf("code = %s", expected.String())
		if strings.Contains(err.Error(), expectedSubstr) {
			return nil
		}
		return fmt.Errorf("expected gRPC response code %q. Instead got: %w", expected.String(), err)
	}
}

// BodyContains checks that the response body contains the given string.
func BodyContains(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if !strings.Contains(r.RawContent, expected) {
			return fmt.Errorf("want %q in body but not found: %s", expected, r.RawContent)
		}
		return nil
	})
}

// Forbidden checks that the response indicates that the request was rejected by RBAC.
func Forbidden(p protocol.Instance) echo.Checker {
	switch {
	case p.IsGRPC():
		return ErrorContains("rpc error: code = PermissionDenied")
	case p.IsTCP():
		return ErrorContains("EOF")
	default:
		return NoErrorAndStatus(http.StatusForbidden)
	}
}

// TooManyRequests checks that at least one message receives a StatusTooManyRequests status code.
func TooManyRequests() echo.Checker {
	codeStr := strconv.Itoa(http.StatusTooManyRequests)
	return func(result echo.CallResult, _ error) error {
		for _, r := range result.Responses {
			if codeStr == r.Code {
				// Successfully received too many requests.
				return nil
			}
		}
		return errors.New("no request received StatusTooManyRequest error")
	}
}

func Host(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.Host != expected {
			return fmt.Errorf("expected host %s, received %s", expected, r.Host)
		}
		return nil
	})
}

// Hostname checks the hostname the request landed on. This differs from Host which is the request we called.
func Hostname(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.Hostname != expected {
			return fmt.Errorf("expected hostname %s, received %s", expected, r.Hostname)
		}
		return nil
	})
}

func Protocol(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.Protocol != expected {
			return fmt.Errorf("expected protocol %s, received %s", expected, r.Protocol)
		}
		return nil
	})
}

func Alpn(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.Alpn != expected {
			return fmt.Errorf("expected alpn %s, received %s", expected, r.Alpn)
		}
		return nil
	})
}

func SNI(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.SNI != expected {
			return fmt.Errorf("expected SNI %s, received %s", expected, r.SNI)
		}
		return nil
	})
}

func ProxyProtocolVersion(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.ProxyProtocol != expected {
			return fmt.Errorf("expected proxy protocol %s, received %s", expected, r.ProxyProtocol)
		}
		return nil
	})
}

// DestinationIPv4 checks the request was received by the server over IPv4
func DestinationIPv4() echo.Checker {
	return Each(func(r echoClient.Response) error {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return fmt.Errorf("could not parse IP %q: %v", r.IP, err)
		}
		if !ip.Is4() {
			return fmt.Errorf("expected DestinationIPv4, got %s", ip.String())
		}
		return nil
	})
}

// DestinationIPv6 checks the request was received by the server over IPv6
func DestinationIPv6() echo.Checker {
	return Each(func(r echoClient.Response) error {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return fmt.Errorf("could not parse IP %q: %v", r.IP, err)
		}
		if !ip.Is6() {
			return fmt.Errorf("expected DestinationIPv6, got %s", ip.String())
		}
		return nil
	})
}

// SourceIPv4 checks the request was sent by the client over IPv4
func SourceIPv4() echo.Checker {
	return Each(func(r echoClient.Response) error {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return fmt.Errorf("could not parse IP %q: %v", r.IP, err)
		}
		if !ip.Is4() {
			return fmt.Errorf("expected SourceIPv4, got %s", ip.String())
		}
		return nil
	})
}

// SourceIPv6 checks the request was sent by the client over IPv6
func SourceIPv6() echo.Checker {
	return Each(func(r echoClient.Response) error {
		ip, err := netip.ParseAddr(r.IP)
		if err != nil {
			return fmt.Errorf("could not parse IP %q: %v", r.IP, err)
		}
		if !ip.Is6() {
			return fmt.Errorf("expected SourceIPv6, got %s", ip.String())
		}
		return nil
	})
}

func isHTTPProtocol(r echoClient.Response) bool {
	return strings.HasPrefix(r.RequestURL, "http://") ||
		strings.HasPrefix(r.RequestURL, "grpc://") ||
		strings.HasPrefix(r.RequestURL, "ws://")
}

func isMTLS(r echoClient.Response) bool {
	_, f1 := r.RequestHeaders["X-Forwarded-Client-Cert"]
	// nolint: staticcheck
	_, f2 := r.RequestHeaders["x-forwarded-client-cert"] // grpc has different casing
	return f1 || f2
}

func MTLSForHTTP() echo.Checker {
	return Each(func(r echoClient.Response) error {
		if !isHTTPProtocol(r) {
			// Non-HTTP traffic. Fail open, we cannot check mTLS.
			return nil
		}
		if isMTLS(r) {
			return nil
		}
		return fmt.Errorf("expected X-Forwarded-Client-Cert but not found: %v", r)
	})
}

func PlaintextForHTTP() echo.Checker {
	return Each(func(r echoClient.Response) error {
		if !isHTTPProtocol(r) {
			// Non-HTTP traffic. Fail open, we cannot check mTLS.
			return nil
		}
		if !isMTLS(r) {
			return nil
		}
		return fmt.Errorf("expected plaintext but found X-Forwarded-Client-Cert header: %v", r)
	})
}

func Port(expected int) echo.Checker {
	return Each(func(r echoClient.Response) error {
		expectedStr := strconv.Itoa(expected)
		if r.Port != expectedStr {
			return fmt.Errorf("expected port %s, received %s", expectedStr, r.Port)
		}
		return nil
	})
}

func requestHeader(r echoClient.Response, key, expected string) error {
	actual := r.RequestHeaders.Get(key)
	if actual != expected {
		return fmt.Errorf("request header %s: expected `%s`, received `%s`", key, expected, actual)
	}
	return nil
}

func responseHeader(r echoClient.Response, key, expected string) error {
	actual := r.ResponseHeaders.Get(key)
	if actual != expected {
		return fmt.Errorf("response header %s: expected `%s`, received `%s`", key, expected, actual)
	}
	return nil
}

func RequestHeader(key, expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		return requestHeader(r, key, expected)
	})
}

func ResponseHeader(key, expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		return responseHeader(r, key, expected)
	})
}

func RequestHeaders(expected map[string]string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		outErr := istiomultierror.New()
		for k, v := range expected {
			outErr = multierror.Append(outErr, requestHeader(r, k, v))
		}
		return outErr.ErrorOrNil()
	})
}

func ResponseHeaders(expected map[string]string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		outErr := istiomultierror.New()
		for k, v := range expected {
			outErr = multierror.Append(outErr, responseHeader(r, k, v))
		}
		return outErr.ErrorOrNil()
	})
}

func Cluster(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.Cluster != expected {
			return fmt.Errorf("expected cluster %s, received %s", expected, r.Cluster)
		}
		return nil
	})
}

func URL(expected string) echo.Checker {
	return Each(func(r echoClient.Response) error {
		if r.URL != expected {
			return fmt.Errorf("expected URL %s, received %s", expected, r.URL)
		}
		return nil
	})
}

func IsDNSCaptureEnabled(t framework.TestContext) bool {
	t.Helper()
	mc := istio.GetOrFail(t).MeshConfigOrFail(t)
	if mc.DefaultConfig != nil && mc.DefaultConfig.ProxyMetadata != nil {
		return mc.DefaultConfig.ProxyMetadata["ISTIO_META_DNS_CAPTURE"] == "true"
	}
	return false
}

// ReachedTargetClusters is similar to ReachedClusters, except that the set of expected clusters is
// retrieved from the Target of the request.
func ReachedTargetClusters(t framework.TestContext) echo.Checker {
	dnsCaptureEnabled := IsDNSCaptureEnabled(t)
	return func(result echo.CallResult, err error) error {
		from := result.From
		to := result.Opts.To
		if from == nil || to == nil {
			// We need src and target in order to determine which clusters should be reached.
			return nil
		}

		if result.Opts.Count < to.Clusters().Len() {
			// There weren't enough calls to hit all the target clusters. Don't bother
			// checking which clusters were reached.
			return nil
		}

		allClusters := t.Clusters()
		if isNaked(from) {
			// Naked clients rely on k8s DNS to lookup endpoint IPs. This
			// means that they will only ever reach endpoint in the same cluster.
			return checkReachedSourceClusterOnly(result, allClusters)
		}

		if to.Config().IsAllNaked() {
			// Requests to naked services will not cross network boundaries.
			// Istio filters out cross-network endpoints.
			return checkReachedSourceNetworkOnly(result, allClusters)
		}

		if !dnsCaptureEnabled && to.Config().IsHeadless() {
			// Headless services rely on DNS resolution. If DNS capture is
			// enabled, DNS will return all endpoints in the mesh, which will
			// allow requests to go cross-cluster. Otherwise, k8s DNS will
			// only return the endpoints within the same cluster as the source
			// pod.
			return checkReachedSourceClusterOnly(result, allClusters)
		}

		toClusters := to.Clusters()
		return checkReachedClusters(result, allClusters, toClusters.ByNetwork())
	}
}

// ReachedClusters returns an error if requests did not load balance as expected.
//
// For cases where all clusters are on the same network, verifies that each of the expected clusters was reached.
//
// For multi-network configurations, verifies the current (limited) Istio load balancing behavior when going through
// a gateway. Ensures that all expected networks were reached, and that all clusters on the same network as the
// client were reached.
func ReachedClusters(allClusters cluster.Clusters, expectedClusters cluster.Clusters) echo.Checker {
	expectedByNetwork := expectedClusters.ByNetwork()
	return func(result echo.CallResult, err error) error {
		return checkReachedClusters(result, allClusters, expectedByNetwork)
	}
}

// ReachedSourceCluster is similar to ReachedClusters, except it only checks the reachability of source cluster only
func ReachedSourceCluster(allClusters cluster.Clusters) echo.Checker {
	return func(result echo.CallResult, err error) error {
		return checkReachedSourceClusterOnly(result, allClusters)
	}
}

// checkReachedSourceClusterOnly verifies that the only cluster that was reached is the cluster where
// the source workload resides.
func checkReachedSourceClusterOnly(result echo.CallResult, allClusters cluster.Clusters) error {
	from := result.From
	to := result.Opts.To
	if from == nil || to == nil {
		return nil
	}

	fromCluster := clusterFor(from)
	if fromCluster == nil {
		return nil
	}

	if !to.Clusters().Contains(fromCluster) {
		// The target is not deployed in the same cluster as the source. Skip this check.
		return nil
	}

	return checkReachedClusters(result, allClusters, cluster.ClustersByNetwork{
		// Use the source network of the caller.
		fromCluster.NetworkName(): cluster.Clusters{fromCluster},
	})
}

// checkReachedSourceNetworkOnly verifies that the only network that was reached is the network where
// the source workload resides.
func checkReachedSourceNetworkOnly(result echo.CallResult, allClusters cluster.Clusters) error {
	fromCluster := clusterFor(result.From)
	if fromCluster == nil {
		return nil
	}

	toClusters := result.Opts.To.Clusters()
	expectedByNetwork := toClusters.ForNetworks(fromCluster.NetworkName()).ByNetwork()
	return checkReachedClusters(result, allClusters, expectedByNetwork)
}

func checkReachedClusters(result echo.CallResult, allClusters cluster.Clusters, expectedByNetwork cluster.ClustersByNetwork) error {
	if err := checkReachedNetworks(result, allClusters, expectedByNetwork); err != nil {
		return err
	}
	return checkReachedClustersInNetwork(result, allClusters, expectedByNetwork)
}

func checkReachedNetworks(result echo.CallResult, allClusters cluster.Clusters, expectedByNetwork cluster.ClustersByNetwork) error {
	// Gather the networks that were reached.
	networkHits := make(map[string]int)
	for _, rr := range result.Responses {
		c := allClusters.GetByName(rr.Cluster)
		if c != nil {
			networkHits[c.NetworkName()]++
		}
	}

	// Verify that all expected networks were reached.
	for network := range expectedByNetwork {
		if networkHits[network] == 0 {
			return fmt.Errorf("did not reach network %v, got %v", network, networkHits)
		}
	}

	// Verify that no unexpected networks were reached.
	for network := range networkHits {
		if expectedByNetwork[network] == nil {
			return fmt.Errorf("reached network not in %v, got %v", expectedByNetwork.Networks(), networkHits)
		}
	}
	return nil
}

func isNaked(c echo.Caller) bool {
	if c != nil {
		if inst, ok := c.(echo.Instance); ok {
			return inst.Config().IsNaked()
		}
	}
	return false
}

func clusterFor(c echo.Caller) cluster.Cluster {
	if c != nil {
		// Determine the source network of the caller.
		switch from := c.(type) {
		case echo.Instance:
			return from.Config().Cluster
		case ingress.Instance:
			return from.Cluster()
		}
	}

	// Unable to determine the source network of the caller. Skip this check.
	return nil
}

func checkReachedClustersInNetwork(result echo.CallResult, allClusters cluster.Clusters, expectedByNetwork cluster.ClustersByNetwork) error {
	fromCluster := clusterFor(result.From)
	if fromCluster == nil {
		return nil
	}
	fromNetwork := fromCluster.NetworkName()

	// Lookup only the expected clusters in the same network as the caller.
	expectedClustersInSourceNetwork := expectedByNetwork[fromNetwork]

	clusterHits := make(map[string]int)
	for _, rr := range result.Responses {
		clusterHits[rr.Cluster]++
	}

	for _, c := range expectedClustersInSourceNetwork {
		if clusterHits[c.Name()] == 0 {
			return fmt.Errorf("did not reach all of %v in source network %v, got %v",
				expectedClustersInSourceNetwork, fromNetwork, clusterHits)
		}
	}

	// Verify that no unexpected clusters were reached.
	for clusterName := range clusterHits {
		reachedCluster := allClusters.GetByName(clusterName)
		if reachedCluster == nil || reachedCluster.NetworkName() != fromNetwork {
			// Ignore clusters on a different network from the source.
			continue
		}

		if expectedClustersInSourceNetwork.GetByName(clusterName) == nil {
			return fmt.Errorf("reached cluster %v in source network %v not in %v, got %v",
				clusterName, fromNetwork, expectedClustersInSourceNetwork, clusterHits)
		}
	}
	return nil
}
