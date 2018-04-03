// Copyright 2018 Istio Authors
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

package probe

import (
	"fmt"
	"testing"
	"time"

	rpc "github.com/gogo/googleapis/google/rpc"
	"google.golang.org/grpc/balancer"

	"istio.io/istio/pkg/probe"
	caclient "istio.io/istio/security/pkg/caclient/grpc"
	"istio.io/istio/security/pkg/pki/ca"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/platform"
	pb "istio.io/istio/security/proto"
)

type FakeCAGrpcClientImpl struct {
	resp *pb.CsrResponse
	err  error
}

func (c *FakeCAGrpcClientImpl) SetResponse(resp *pb.CsrResponse, err error) {
	c.resp = resp
	c.err = err
}

// SendCSR
func (c *FakeCAGrpcClientImpl) SendCSR(req *pb.CsrRequest, pc platform.Client, caAddress string) (*pb.CsrResponse, error) {
	return c.resp, c.err
}

func TestGcpGetServiceIdentity(t *testing.T) {
	bundle, err := util.NewVerifiedKeyCertBundleFromFile(
		"./testdata/ca.crt", "./testdata/ca.key", "", "./testdata/root.crt")
	if err != nil {
		t.Error(err)
	}
	istioCA, err := ca.NewIstioCA(&ca.IstioCAOptions{
		CertTTL:       time.Minute * time.Duration(2),
		MaxCertTTL:    time.Minute * time.Duration(4),
		KeyCertBundle: bundle,
	})
	if err != nil {
		t.Fatalf("Failed to create a CA instances: %v", err)
	}

	testCases := map[string]struct {
		resp     *pb.CsrResponse
		err      error
		expected string
	}{
		"Check success": {
			resp: &pb.CsrResponse{
				IsApproved: true,
				Status:     &rpc.Status{Code: int32(rpc.OK), Message: "OK"},
				SignedCert: nil,
				CertChain:  nil,
			},
			err:      nil,
			expected: "",
		},
		"SendCSR failed": {
			resp:     nil,
			err:      fmt.Errorf("sendCSR failed"),
			expected: "sendCSR failed",
		},
		"gRPC server is not available": {
			resp:     nil,
			err:      fmt.Errorf("%v", balancer.ErrTransientFailure.Error()),
			expected: "",
		},
	}

	for id, c := range testCases {
		// override mock client
		mockClient := FakeCAGrpcClientImpl{
			resp: c.resp,
			err:  c.err,
		}

		var g interface{} = &mockClient
		client, ok := g.(caclient.CAGrpcClient)
		if !ok {
			t.Fatalf("%v: Failed to create a client", id)
		}

		// test liveness probe check controller
		controller, err := NewLivenessCheckController(
			time.Minute,
			"localhost",
			1234,
			istioCA,
			&probe.Options{
				Path:           "/tmp/test.key",
				UpdateInterval: time.Minute,
			},
			client,
		)
		if err != nil {
			t.Errorf("%v: Expecting an error but an Istio CA is wrongly instantiated", id)
		}

		err = controller.checkGrpcServer()
		if len(c.expected) == 0 {
			if err != nil {
				t.Errorf("%v: checkGrpcServer should return nil: %v", id, err)
			}
		} else {
			if err == nil || c.expected != err.Error() {
				t.Errorf("%v: Unexpected error. expected: %v, got: %v", id, c.expected, err)
			}
		}
	}
}
