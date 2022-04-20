//go:build gofuzz
// +build gofuzz

// Copyright Istio Authors
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

package ca

import (
	"context"
	"fmt"

	pb "istio.io/api/security/v1alpha1"

	"istio.io/istio/pkg/security"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	caerror "istio.io/istio/security/pkg/pki/error"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
)

// FuzzCreateCertificate implements a fuzzer
// that tests CreateCertificate().
func CreateCertificateFuzz(data []byte) int {
	f := fuzz.NewConsumer(data)
	f.AllowUnexportedFields()

	// Insert random values in IstioCertificateRequest
	request := &pb.IstioCertificateRequest{}
	err := f.GenerateStruct(request)
	if err != nil {
		return 0
	}

	// Insert random values in the fakeCA
	fakeCA := &mockca.FakeCA{}
	err = f.GenerateStruct(fakeCA)
	if err != nil {
		return 0
	}
	fakeCA.SignErr = caerror.NewError(caerror.CSRError, fmt.Errorf("cannot sign"))

	server := &Server{
		ca:             fakeCA,
		Authenticators: []security.Authenticator{&mockAuthenticator{}},
		monitoring:     newMonitoringMetrics(),
	}

	// Hit the target
	_, _ = server.CreateCertificate(context.Background(), request)
	return 1
}
