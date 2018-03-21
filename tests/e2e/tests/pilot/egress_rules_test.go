// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Routing tests

package pilot

import (
	"fmt"
	"os"
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type egressRules struct {
	*tutil.Environment
}

func (t *egressRules) String() string {
	return "egress-rules"
}

func (t *egressRules) Setup() error {
	return nil
}

// TODO: test negatives
func (t *egressRules) Run() error {
	if os.Getenv("SKIP_EGRESS") != "" {
		return nil
	}
	// egress rules are v1alpha1
	if !t.Config.V1alpha1 {
		return nil
	}
	cases := []struct {
		description string
		config      string
		check       func() error
	}{
		{
			description: "allow external traffic to httbin.org",
			config:      "v1alpha1/egress-rule-httpbin.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("http://httpbin.org/headers", true)
			},
		},
		{
			description: "allow external traffic to *.httbin.org",
			config:      "v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("http://www.httpbin.org/headers", true)
			},
		},
		{
			description: "ensure traffic to httbin.org is prohibited when setting *.httbin.org",
			config:      "v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("http://httpbin.org/headers", false)
			},
		},
		{
			description: "allow external http2 traffic to google.com",
			config:      "v1alpha1/egress-rule-google.yaml.tmpl",
			check: func() error {
				// Note that we're using http (not https). We're relying on Envoy to convert the outbound call to
				// TLS for us. This is currently the suggested way for the application to call an external TLS service.\
				// If the application uses TLS, then no metrics will be collected for the request.
				return t.verifyReachable("http://www.google.com:443", true)
			},
		},
		{
			description: "prohibit https to httbin.org",
			config:      "v1alpha1/egress-rule-httpbin.yaml.tmpl",
			check: func() error {
				// Note that we're using http (not https). We're relying on Envoy to convert the outbound call to
				// TLS for us. This is currently the suggested way for the application to call an external TLS service.\
				// If the application uses TLS, then no metrics will be collected for the request.
				return t.verifyReachable("http://httpbin.org:443/headers", false)
			},
		},
		{
			description: "allow https external traffic to www.wikipedia.org by a tcp egress rule with cidr",
			config:      "v1alpha1/egress-rule-tcp-wikipedia-cidr.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("https://www.wikipedia.org", true)
			},
		},
		{
			description: "prohibit http external traffic to cnn.com by a tcp egress rule",
			config:      "v1alpha1/egress-rule-tcp-wikipedia-cidr.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("https://cnn.com", false)
			},
		},
	}
	var errs error
	for _, cs := range cases {
		tutil.Tlog("Checking egressRules test", cs.description)
		if err := t.ApplyConfig(cs.config, nil); err != nil {
			return err
		}

		if err := tutil.Repeat(cs.check, 3, time.Second); err != nil {
			log.Infof("Failed the test with %v", err)
			errs = multierror.Append(errs, multierror.Prefix(err, cs.description))
		} else {
			log.Info("Success!")
		}

		if err := t.DeleteConfig(cs.config, nil); err != nil {
			return err
		}
	}
	return errs
}

func (t *egressRules) Teardown() {
	log.Info("Cleaning up egress rules...")
	if err := t.DeleteAllConfigs(); err != nil {
		log.Warna(err)
	}
}

// verifyReachable verifies that the url is reachable
func (t *egressRules) verifyReachable(url string, shouldBeReachable bool) error {
	funcs := make(map[string]func() tutil.Status)
	for _, src := range []string{"a", "b"} {
		name := fmt.Sprintf("Request from %s to %s", src, url)
		funcs[name] = (func(src string) func() tutil.Status {
			trace := fmt.Sprint(time.Now().UnixNano())
			return func() tutil.Status {
				resp := t.ClientRequest(src, url, 1, fmt.Sprintf("-key Trace-Id -val %q", trace))
				reachable := resp.IsHTTPOk() && strings.Contains(resp.Body, trace)
				if reachable && !shouldBeReachable {
					return fmt.Errorf("%s is reachable from %s (should be unreachable)", url, src)
				}
				if !reachable && shouldBeReachable {
					return tutil.ErrAgain
				}

				return nil
			}
		})(src)
	}

	return tutil.Parallel(funcs)
}
