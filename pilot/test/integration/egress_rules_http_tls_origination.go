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

package main

import (
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"
)

type egressRulesHTTPTLSOrigination struct {
	*egressRules
}

func (t *egressRulesHTTPTLSOrigination) String() string {
	return "egress-rules-http-tls-origination"
}

// TODO: test negatives
func (t *egressRulesHTTPTLSOrigination) run() error {
	cases := []struct {
		description string
		config      string
		check       func() error
	}{
		{
			description: "allow https external traffic to *google.com",
			config:      "egress-rule-google.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("http://cloud.google.com:443", true)
			},
		},
		{
			description: "prohibit http external traffic to *google.com",
			config:      "egress-rule-google.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("http://cloud.google.com", false)
			},
		},
		{
			description: "prohibit https external traffic to cnn.com by egress a rule for google",
			config:      "egress-rule-google.yaml.tmpl",
			check: func() error {
				return t.verifyReachable("https://cnn.com", false)
			},
		},
	}
	var errs error
	for _, cs := range cases {
		log("Checking egressRulesHTTPTLSOrigination test", cs.description)
		if err := t.applyConfig(cs.config, nil); err != nil {
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
