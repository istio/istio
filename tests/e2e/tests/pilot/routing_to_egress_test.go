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

// Tests of routing rules to outbound traffic defined by egress rules

package pilot

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	tutil "istio.io/istio/tests/e2e/tests/pilot/util"
)

type routingToEgress struct {
	*tutil.Environment
}

func (t *routingToEgress) String() string {
	return "routing-rules-to-egress"
}

func (t *routingToEgress) Setup() error {
	return nil
}

func (t *routingToEgress) Run() error {
	// egress rules are v1alpha1
	if !t.Config.V1alpha1 {
		return nil
	}
	cases := []struct {
		description   string
		configEgress  []string
		configRouting string
		routingData   map[string]string
		check         func() error
	}{
		// Fault Injection
		{
			description:   "inject a http fault in traffic to httpbin.org",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-fault-injection-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service": "httpbin.org",
			},
			check: func() error {
				return t.verifyFaultInjectionByResponseCode("a", "http://httpbin.org", 418)
			},
		},
		{
			description:   "inject a http fault in traffic to *.httpbin.org",
			configEgress:  []string{"v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-fault-injection-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service": "*.httpbin.org",
			},
			check: func() error {
				return t.verifyFaultInjectionByResponseCode("a", "http://www.httpbin.org", 418)
			},
		},
		{
			description:   "inject a http fault in traffic to google.com",
			configEgress:  []string{"v1alpha1/egress-rule-google.yaml.tmpl"},
			configRouting: "v1alpha1/rule-fault-injection-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service": "*google.com",
			},
			check: func() error {
				return t.verifyFaultInjectionByResponseCode("a", "http://www.google.com:443", 418)
			},
		},
		// Append Headers
		{
			description:   "append http headers in traffic to httpbin.org",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-route-append-headers-httpbin.yaml.tmpl",
			routingData:   nil,
			check: func() error {
				return t.verifyRequestHeaders("a", "http://httpbin.org/headers",
					map[string]string{
						"istio-custom-header1": "user-defined-value1",
						"istio-custom-header2": "user-defined-value2",
					})
			},
		},
		// Redirect
		{
			description:   "redirect traffic from httpbin.org/post to httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-redirect-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "httpbin.org",
				"from":      "/post",
				"to":        "/get",
				"authority": "httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://httpbin.org/post", "httpbin.org", "/get")
			},
		},
		{
			description:   "redirect traffic from httpbin.org/post to *.httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl", "v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-redirect-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "httpbin.org",
				"from":      "/post",
				"to":        "/get",
				"authority": "www.httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://httpbin.org/post", "www.httpbin.org", "/get")
			},
		},
		{
			description:   "redirect traffic from *.httpbin.org/post to httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl", "v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-redirect-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "*.httpbin.org",
				"from":      "/post",
				"to":        "/get",
				"authority": "httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://www.httpbin.org/post", "httpbin.org", "/get")
			},
		},
		{
			description:   "redirect traffic from google.com/post to httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-google.yaml.tmpl", "v1alpha1/egress-rule-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-redirect-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "*google.com",
				"from":      "/post",
				"to":        "/get",
				"authority": "httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://www.google.com:443/post", "httpbin.org", "/get")
			},
		},
		// Rewrite
		{
			description:   "rewrite traffic from httpbin.org/post to httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-rewrite-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "httpbin.org",
				"from":      "/post",
				"to":        "/get",
				"authority": "httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://httpbin.org/post", "httpbin.org", "/get")
			},
		},
		{
			description:   "rewrite traffic from httpbin.org/post to *.httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl", "v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-rewrite-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "httpbin.org",
				"from":      "/post",
				"to":        "/get",
				"authority": "www.httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://httpbin.org/post", "www.httpbin.org", "/get")
			},
		},
		{
			description:   "rewrite traffic from *.httpbin.org/post to httpbin.org/get",
			configEgress:  []string{"v1alpha1/egress-rule-httpbin.yaml.tmpl", "v1alpha1/egress-rule-wildcard-httpbin.yaml.tmpl"},
			configRouting: "v1alpha1/rule-rewrite-to-egress.yaml.tmpl",
			routingData: map[string]string{
				"service":   "*.httpbin.org",
				"from":      "/post",
				"to":        "/get",
				"authority": "httpbin.org",
			},
			check: func() error {
				return t.verifyEgressRedirectRewrite("a", "http://www.httpbin.org/post", "httpbin.org", "/get")
			},
		},
	}

	var errs error
	for _, cs := range cases {
		tutil.Tlog("Checking routing rule to egress rule test", cs.description)
		for _, configEgress := range cs.configEgress {
			if err := t.ApplyConfig(configEgress, nil); err != nil {
				return err
			}
		}
		if err := t.ApplyConfig(cs.configRouting, cs.routingData); err != nil {
			return err
		}

		if err := tutil.Repeat(cs.check, 3, time.Second); err != nil {
			log.Infof("Failed the test with %v", err)
			errs = multierror.Append(errs, multierror.Prefix(err, cs.description))
		} else {
			log.Info("Success!")
		}

		if err := t.DeleteConfig(cs.configRouting, cs.routingData); err != nil {
			return err
		}
		for _, configEgress := range cs.configEgress {
			if err := t.DeleteConfig(configEgress, nil); err != nil {
				return err
			}
		}
	}
	return errs
}

func (t *routingToEgress) Teardown() {
	log.Info("Cleaning up route rules to egress rules...")
	if err := t.DeleteAllConfigs(); err != nil {
		log.Warna(err)
	}
}

func (t *routingToEgress) verifyFaultInjectionByResponseCode(src, url string, respCode int) error {
	log.Infof("Making 1 request (%s) from %s...\n", url, src)

	resp := t.ClientRequest(src, url, 1, "")

	statusCode := ""
	if len(resp.Code) > 0 {
		statusCode = resp.Code[0]
	}

	if strconv.Itoa(respCode) != statusCode {
		return fmt.Errorf("fault injection verification failed: status code %s, expected status code %d",
			statusCode, respCode)
	}
	return nil
}

func (t *routingToEgress) verifyRequestHeaders(src, httpbinURL string, expectedHeaders map[string]string) error {
	log.Infof("Making 1 request (%s) from %s...\n", httpbinURL, src)

	resp := t.ClientRequest(src, httpbinURL, 1, "")

	containsAllExpectedHeaders := true

	headerFormat := "\"%s\": \"%s\""
	for name, value := range expectedHeaders {
		headerContent := fmt.Sprintf(headerFormat, name, value)
		if !strings.Contains(strings.ToLower(resp.Body), strings.ToLower(headerContent)) {
			containsAllExpectedHeaders = false
		}
	}

	if !containsAllExpectedHeaders {
		return fmt.Errorf("headers verification failed: headers: %s, expected headers: %s",
			resp.Body, expectedHeaders)
	}
	return nil
}

//verifyEgressRedirectRewrite verifies if the http redirect/rewrite was setup properly
func (t *routingToEgress) verifyEgressRedirectRewrite(src, dstURL, targetHost, targetPath string) error {
	log.Infof("Making 1 request (%s) from %s...\n", dstURL, src)

	resp := t.ClientRequest(src, dstURL, 1, "")
	if !resp.IsHTTPOk() {
		return fmt.Errorf("redirect verification failed: response status code: %v, expected 200",
			resp.Code)
	}

	var actualRedirection string
	if matches := regexp.MustCompile(`(?i)"url": "(.*)"`).FindStringSubmatch(resp.Body); len(matches) >= 2 {
		actualRedirection = matches[1]
	}

	u, err := url.Parse(actualRedirection)
	if err != nil {
		return fmt.Errorf("redirect verification failed: url.Parse failed: %v", err)
	}

	if u.Host != targetHost {
		return fmt.Errorf("redirect verification failed: location header contains Host=%v, expected Host=%v",
			u.Host, targetHost)
	}

	if u.Path != targetPath {
		return fmt.Errorf("redirect verification failed: location header contains Path=%v, expected Path=%v",
			u.Path, targetPath)
	}

	return nil
}
