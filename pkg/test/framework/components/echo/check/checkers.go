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
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/protocol"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/util/istiomultierror"
)

// Each applies the given per-response function across all responses.
func Each(c func(r echoClient.Response) error) echo.Checker {
	return func(result echo.CallResult, _ error) error {
		rs := result.Responses
		if rs.IsEmpty() {
			return fmt.Errorf("no responses received")
		}
		outErr := istiomultierror.New()
		for i, r := range rs {
			if err := c(r); err != nil {
				outErr = multierror.Append(outErr, fmt.Errorf("response[%d]: %v", i, err))
			}
		}
		return outErr.ErrorOrNil()
	}
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
func Or(checkers ...echo.Checker) echo.Checker {
	return func(result echo.CallResult, err error) error {
		out := istiomultierror.New()
		for _, c := range checkers {
			err := c(result, err)
			if err == nil {
				return nil
			}
			out = multierror.Append(out, err)
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
	expectedStr := ""
	if expected > 0 {
		expectedStr = strconv.Itoa(expected)
	}
	return func(resp echo.CallResult, err error) error {
		if err != nil {
			return nil
		}
		for _, r := range resp.Responses {
			if r.Code != expectedStr {
				return fmt.Errorf("expected response code `%s`, got %q", expectedStr, r.Code)
			}
		}
		return nil
	}
}

// OK is a shorthand for NoErrorAndStatus(200).
func OK() echo.Checker {
	return NoErrorAndStatus(http.StatusOK)
}

// NoErrorAndStatus is checks that no error occurred and htat the returned status code matches the expected
// value.
func NoErrorAndStatus(expected int) echo.Checker {
	return And(NoError(), Status(expected))
}

// Status checks that the response status code matches the expected value. If the expected value is zero,
// checks that the response code is unset.
func Status(expected int) echo.Checker {
	expectedStr := ""
	if expected > 0 {
		expectedStr = strconv.Itoa(expected)
	}
	return Each(func(r echoClient.Response) error {
		if r.Code != expectedStr {
			return fmt.Errorf("expected response code `%s`, got %q. Response: %s", expectedStr, r.Code, r)
		}
		return nil
	})
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

func MTLSForHTTP() echo.Checker {
	return Each(func(r echoClient.Response) error {
		if !strings.HasPrefix(r.RequestURL, "http://") &&
			!strings.HasPrefix(r.RequestURL, "grpc://") &&
			!strings.HasPrefix(r.RequestURL, "ws://") {
			// Non-HTTP traffic. Fail open, we cannot check mTLS.
			return nil
		}
		_, f1 := r.RequestHeaders["X-Forwarded-Client-Cert"]
		// nolint: staticcheck
		_, f2 := r.RequestHeaders["x-forwarded-client-cert"] // grpc has different casing
		if f1 || f2 {
			return nil
		}
		return fmt.Errorf("expected X-Forwarded-Client-Cert but not found: %v", r)
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

// ReachedTargetClusters is similar to ReachedClusters, except that the set of expected clusters is
// retrieved from the Target of the request.
func ReachedTargetClusters(allClusters cluster.Clusters) echo.Checker {
	return func(result echo.CallResult, err error) error {
		expectedByNetwork := result.Opts.To.Clusters().ByNetwork()
		return checkReachedClusters(result, allClusters, expectedByNetwork)
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
			return fmt.Errorf("did not reach network %q, got %v", network, networkHits)
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

func checkReachedClustersInNetwork(result echo.CallResult, allClusters cluster.Clusters, expectedByNetwork cluster.ClustersByNetwork) error {
	// Determine the source network of the caller.
	var sourceNetwork string
	switch from := result.From.(type) {
	case echo.Instance:
		sourceNetwork = from.Config().Cluster.NetworkName()
	case ingress.Instance:
		sourceNetwork = from.Cluster().NetworkName()
	default:
		// Unable to determine the source network of the caller. Skip this check.
		return nil
	}

	// Lookup only the expected clusters in the same network as the caller.
	expectedClustersInSourceNetwork := expectedByNetwork[sourceNetwork]

	clusterHits := make(map[string]int)
	for _, rr := range result.Responses {
		clusterHits[rr.Cluster]++
	}

	for _, c := range expectedClustersInSourceNetwork {
		if clusterHits[c.Name()] == 0 {
			return fmt.Errorf("did not reach all of %v in source network %v, got %v",
				expectedClustersInSourceNetwork, sourceNetwork, clusterHits)
		}
	}

	// Verify that no unexpected clusters were reached.
	for clusterName := range clusterHits {
		reachedCluster := allClusters.GetByName(clusterName)
		if reachedCluster == nil || reachedCluster.NetworkName() != sourceNetwork {
			// Ignore clusters on a different network from the source.
			continue
		}

		if expectedClustersInSourceNetwork.GetByName(clusterName) == nil {
			return fmt.Errorf("reached cluster %v in source network %v not in %v, got %v",
				clusterName, sourceNetwork, expectedClustersInSourceNetwork, clusterHits)
		}
	}
	return nil
}
