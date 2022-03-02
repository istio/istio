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

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/util/istiomultierror"
)

// Each applies the given per-response function across all responses.
func Each(c func(r echo.Response) error) Checker {
	return func(rs echo.Responses, _ error) error {
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
func And(checkers ...Checker) Checker {
	return func(rs echo.Responses, err error) error {
		for _, c := range filterNil(checkers) {
			if err := c(rs, err); err != nil {
				return err
			}
		}
		return nil
	}
}

// Or is an aggregate Checker that requires at least one Checker succeeds.
func Or(checkers ...Checker) Checker {
	return func(rs echo.Responses, err error) error {
		out := istiomultierror.New()
		for _, c := range checkers {
			err := c(rs, err)
			if err == nil {
				return nil
			}
			out = multierror.Append(out, err)
		}
		return out.ErrorOrNil()
	}
}

func filterNil(checkers []Checker) []Checker {
	var out []Checker
	for _, c := range checkers {
		if c != nil {
			out = append(out, c)
		}
	}
	return out
}

// None provides a Checker that returns the original raw call error, unaltered.
func None() Checker {
	return func(_ echo.Responses, err error) error {
		return err
	}
}

// NoError is similar to None, but provides additional context information.
func NoError() Checker {
	return func(_ echo.Responses, err error) error {
		if err != nil {
			return fmt.Errorf("expected no error, but encountered %v", err)
		}
		return nil
	}
}

// Error provides a checker that returns an error if the call succeeds.
func Error() Checker {
	return func(_ echo.Responses, err error) error {
		if err == nil {
			return errors.New("expected error, but none occurred")
		}
		return nil
	}
}

// ErrorContains is similar to Error, but checks that the error message contains the given string.
func ErrorContains(expected string) Checker {
	return func(_ echo.Responses, err error) error {
		if err == nil {
			return errors.New("expected error, but none occurred")
		}
		if !strings.Contains(err.Error(), expected) {
			return fmt.Errorf("expected error to contain %s: %v", expected, err)
		}
		return nil
	}
}

// OK is a shorthand for NoErrorAndStatus(200).
func OK() Checker {
	return NoErrorAndStatus(http.StatusOK)
}

// NoErrorAndStatus is checks that no error occurred and htat the returned status code matches the expected
// value.
func NoErrorAndStatus(expected int) Checker {
	return And(NoError(), Status(expected))
}

// Status checks that the response status code matches the expected value. If the expected value is zero,
// checks that the response code is unset.
func Status(expected int) Checker {
	expectedStr := ""
	if expected > 0 {
		expectedStr = strconv.Itoa(expected)
	}
	return Each(func(r echo.Response) error {
		if r.Code != expectedStr {
			return fmt.Errorf("expected response code `%s`, got %q", expectedStr, r.Code)
		}
		return nil
	})
}

// TooManyRequests checks that at least one message receives a StatusTooManyRequests status code.
func TooManyRequests() Checker {
	codeStr := strconv.Itoa(http.StatusTooManyRequests)
	return func(rs echo.Responses, _ error) error {
		for _, r := range rs {
			if codeStr == r.Code {
				// Successfully received too many requests.
				return nil
			}
		}
		return errors.New("no request received StatusTooManyRequest error")
	}
}

func Host(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.Host != expected {
			return fmt.Errorf("expected host %s, received %s", expected, r.Host)
		}
		return nil
	})
}

func Protocol(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.Protocol != expected {
			return fmt.Errorf("expected protocol %s, received %s", expected, r.Protocol)
		}
		return nil
	})
}

func Alpn(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.Alpn != expected {
			return fmt.Errorf("expected alpn %s, received %s", expected, r.Alpn)
		}
		return nil
	})
}

func MTLSForHTTP() Checker {
	return Each(func(r echo.Response) error {
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

func Port(expected int) Checker {
	return Each(func(r echo.Response) error {
		expectedStr := strconv.Itoa(expected)
		if r.Port != expectedStr {
			return fmt.Errorf("expected port %s, received %s", expectedStr, r.Port)
		}
		return nil
	})
}

func requestHeader(r echo.Response, key, expected string) error {
	actual := r.RequestHeaders.Get(key)
	if actual != expected {
		return fmt.Errorf("request header %s: expected `%s`, received `%s`", key, expected, actual)
	}
	return nil
}

func responseHeader(r echo.Response, key, expected string) error {
	actual := r.ResponseHeaders.Get(key)
	if actual != expected {
		return fmt.Errorf("response header %s: expected `%s`, received `%s`", key, expected, actual)
	}
	return nil
}

func RequestHeader(key, expected string) Checker {
	return Each(func(r echo.Response) error {
		return requestHeader(r, key, expected)
	})
}

func ResponseHeader(key, expected string) Checker {
	return Each(func(r echo.Response) error {
		return responseHeader(r, key, expected)
	})
}

func RequestHeaders(expected map[string]string) Checker {
	return Each(func(r echo.Response) error {
		outErr := istiomultierror.New()
		for k, v := range expected {
			outErr = multierror.Append(outErr, requestHeader(r, k, v))
		}
		return outErr.ErrorOrNil()
	})
}

func ResponseHeaders(expected map[string]string) Checker {
	return Each(func(r echo.Response) error {
		outErr := istiomultierror.New()
		for k, v := range expected {
			outErr = multierror.Append(outErr, responseHeader(r, k, v))
		}
		return outErr.ErrorOrNil()
	})
}

func Cluster(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.Cluster != expected {
			return fmt.Errorf("expected cluster %s, received %s", expected, r.Cluster)
		}
		return nil
	})
}

func URL(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.URL != expected {
			return fmt.Errorf("expected URL %s, received %s", expected, r.URL)
		}
		return nil
	})
}

// ReachedClusters returns an error if there wasn't at least one response from each of the given clusters.
// This can be used in combination with echo.Responses.Clusters(), for example:
//     echoA[0].CallOrFail(t, ...).CheckReachedClusters(echoB.Clusters())
func ReachedClusters(clusters cluster.Clusters) Checker {
	return func(r echo.Responses, err error) error {
		hits := clusterDistribution(r)
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
}

func clusterDistribution(r echo.Responses) map[string]int {
	hits := map[string]int{}
	for _, rr := range r {
		hits[rr.Cluster]++
	}
	return hits
}
