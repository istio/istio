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

package policybackend

import (
	"time"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
)

// AdapterMode enumerates the mode of policy backend usage
type AdapterMode int

const (
	// OutOfProcess mode uses policy backend as an out of process adapter
	OutOfProcess AdapterMode = iota

	// InProcess mode uses policy backend as an infra backend for built-in bypass adapter
	InProcess
)

// Instance represents a deployed fake policy backend for Mixer.
type Instance interface {
	resource.Resource

	// DenyCheck indicates that the policy backend should deny all incoming check requests when deny is
	// set to true.
	DenyCheck(t test.Failer, deny bool)

	// AllowCheck indicates the policy backend should allow all incoming check requests,
	// it also indicates the valid duration and valid count in the check result.
	AllowCheck(t test.Failer, d time.Duration, c int32)

	// ExpectReport checks that the backend has received the given report requests. The requests are consumed
	// after the call completes.
	ExpectReport(t test.Failer, expected ...proto.Message)

	// ExpectReportJSON checks that the backend has received the given report request.  The requests are
	// consumed after the call completes.
	ExpectReportJSON(t test.Failer, expected ...string)

	// GetReports reeturns the currently accumulated set of reports.
	GetReports(t test.Failer) []proto.Message

	// CreateConfigSnippet for the Mixer adapter to talk to this policy backend.
	// If adapter mode is in process, the supplied name will be the name of the handler.
	CreateConfigSnippet(name string, namespace string, am AdapterMode) string
}

type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of policybackend.Instance.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail calls New and fails test if it returns an error.
func NewOrFail(t test.Failer, s resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(s, c)
	if err != nil {
		t.Fatalf("policybackend.NewOrFail: %v", err)
	}

	return i
}
