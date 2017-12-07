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

package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
)

type routingToEgress struct {
	*infra
}

func (t *routingToEgress) String() string {
	return "routing-rules-to-egress"
}

func (t *routingToEgress) setup() error {
	return nil
}

func (t *routingToEgress) run() error {
	cases := []struct {
		description   string
		configEgress  string
		configRouting string
		check         func() error
	}{
		{
			description:   "inject a http fault in traffic to httpbin.org",
			configEgress:  "egress-rule-httpbin.yaml.tmpl",
			configRouting: "rule-fault-injection-httpbin.yaml.tmpl",
			check: func() error {
				return t.verifyFaultInjectionByResponseCode("a", "http://httpbin.org", 418)
			},
		},
	}

	var errs error
	for _, cs := range cases {
		log("Checking routing rule to egress rule test", cs.description)
		if err := t.applyConfig(cs.configEgress, nil); err != nil {
			return err
		}
		if err := t.applyConfig(cs.configRouting, nil); err != nil {
			return err
		}

		if err := repeat(cs.check, 3, time.Second); err != nil {
			glog.Infof("Failed the test with %v", err)
			errs = multierror.Append(errs, multierror.Prefix(err, cs.description))
		} else {
			glog.Info("Success!")
		}
	}
	return errs
}

func (t *routingToEgress) teardown() {
	glog.Info("Cleaning up route rules to egress rules...")
	if err := t.deleteAllConfigs(); err != nil {
		glog.Warning(err)
	}
}

func (t *routingToEgress) verifyFaultInjectionByResponseCode(src, url string, respCode int) error {
	glog.Infof("Making 1 request (%s) from %s...\n", url, src)

	resp := t.clientRequest(src, url, 1, "")

	statusCode := ""
	if len(resp.code) > 0 {
		statusCode = resp.code[0]
	}

	if strconv.Itoa(respCode) != statusCode {
		return fmt.Errorf("fault injection verification failed: "+
			"status code %s, "+
			"expected status code %d", statusCode, respCode)
	}
	return nil
}
