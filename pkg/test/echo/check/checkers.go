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
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/util/istiomultierror"
)

// WithInfo adds additional context information to any error returned by the provided Checker.
func WithInfo(info string, c Checker) Checker {
	return FilterError(func(err error) error {
		return fmt.Errorf("%s: %v", info, err)
	}, c)
}

// FilterError applies the given filter function to any errors returned by the Checker.
func FilterError(filter func(error) error, c Checker) Checker {
	return func(rs echo.Responses, err error) error {
		if err := c(rs, err); err != nil {
			return filter(err)
		}
		return nil
	}
}

// Each applies the given per-response function across all responses.
func Each(c func(r echo.Response) error) Checker {
	return func(rs echo.Responses, _ error) error {
		if rs.Len() == 0 {
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

// And is an aggregate Checker that requires all Checkers succeed.
func And(checkers ...Checker) Checker {
	return func(r echo.Responses, err error) error {
		for _, c := range checkers {
			if err := c(r, err); err != nil {
				return err
			}
		}
		return nil
	}
}

// None provides a Checker that returns the original raw call error, unaltered.
func None() Checker {
	return func(_ echo.Responses, err error) error {
		return err
	}
}

// NoError is similar to None, but provides additional context information.
func NoError() Checker {
	return WithInfo("expected no error, but encountered", None())
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

func OK() Checker {
	return Code(echo.StatusCodeOK)
}

func Code(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.Code != expected {
			return fmt.Errorf("expected response code %s, got %q", expected, r.Code)
		}
		return nil
	})
}

func Host(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.Host != expected {
			return fmt.Errorf("expected host %s, received %s", expected, r.Host)
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
		_, f1 := r.RawResponse["X-Forwarded-Client-Cert"]
		_, f2 := r.RawResponse["x-forwarded-client-cert"] // grpc has different casing
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

func Key(key, expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.RawResponse[key] != expected {
			return fmt.Errorf("%s: HTTP code %s, expected %s, received %s", key, r.Code, expected, r.RawResponse[key])
		}
		return nil
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

func IP(expected string) Checker {
	return Each(func(r echo.Response) error {
		if r.IP != expected {
			return fmt.Errorf("expected IP %s, received %s", expected, r.IP)
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

// EqualClusterTraffic checks that traffic was equally distributed across the given clusters, allowing some percent error.
// For example, with 100 requests and 20 percent error, each cluster must given received 20±4 responses. Only the passed
// in clusters will be validated.
func EqualClusterTraffic(clusters cluster.Clusters, precisionPct int) Checker {
	return func(r echo.Responses, err error) error {
		clusterHits := clusterDistribution(r)
		expected := len(r) / len(clusters)
		precision := int(float32(expected) * (float32(precisionPct) / 100))
		for _, hits := range clusterHits {
			if !almostEquals(hits, expected, precision) {
				return fmt.Errorf("requests were not equally distributed across clusters: %v", clusterHits)
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

func almostEquals(a, b, precision int) bool {
	upper := a + precision
	lower := a - precision
	if b < lower || b > upper {
		return false
	}
	return true
}
