// Copyright 2019 Istio Authors
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

package main

import (
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/spiffe"
)

func Test_createCA(t *testing.T) {
	certPem, err := ioutil.ReadFile("../../pkg/pki/testdata/multilevelpki/int2-cert.pem")
	if err != nil {
		t.Errorf(err.Error())
		return
	}
	client := fake.NewSimpleClientset()
	type args struct {
		client corev1.CoreV1Interface
	}
	tests := []struct {
		name            string
		args            args
		opts            cliOptions
		wantCert        []byte
		wantTrustDomain string
	}{
		{
			name: "Given a client with no TrustDomain specified (self-signed)",
			args: args{
				client: client.CoreV1(),
			},
			opts: cliOptions{
				selfSignedCA:        true,
				readSigningCertOnly: false,
				selfSignedCACertTTL: time.Hour * 24 * 365,
				workloadCertTTL:     time.Hour * 24,
				maxWorkloadCertTTL:  time.Hour * 24 * 7,
				dualUse:             false,
			},
			wantTrustDomain: "cluster.local",
		},
		{
			name: "Given a client with a custom TrustDomain specified (self-signed)",
			args: args{
				client: client.CoreV1(),
			},
			opts: cliOptions{
				selfSignedCA:        true,
				readSigningCertOnly: false,
				selfSignedCACertTTL: time.Hour * 24 * 365,
				workloadCertTTL:     time.Hour * 24,
				maxWorkloadCertTTL:  time.Hour * 24 * 7,
				dualUse:             false,
				trustDomain:         "my.domain.com",
			},
			wantTrustDomain: "my.domain.com",
		},
		{
			name: "Given a client with a no TrustDomain specified (not self-signed)",
			args: args{
				client: client.CoreV1(),
			},
			opts: cliOptions{
				selfSignedCA:       false,
				rootCertFile:       "../../pkg/pki/testdata/multilevelpki/root-cert.pem",
				certChainFile:      "../../pkg/pki/testdata/multilevelpki/int2-cert-chain.pem",
				signingCertFile:    "../../pkg/pki/testdata/multilevelpki/int2-cert.pem",
				signingKeyFile:     "../../pkg/pki/testdata/multilevelpki/int2-key.pem",
				workloadCertTTL:    time.Hour * 24,
				maxWorkloadCertTTL: time.Hour * 24 * 7,
			},
			wantTrustDomain: "cluster.local",
			wantCert:        certPem,
		},
		{
			name: "Given a client with a custom TrustDomain specified (not self-signed)",
			args: args{
				client: client.CoreV1(),
			},
			opts: cliOptions{
				selfSignedCA:       false,
				rootCertFile:       "../../pkg/pki/testdata/multilevelpki/root-cert.pem",
				certChainFile:      "../../pkg/pki/testdata/multilevelpki/int2-cert-chain.pem",
				signingCertFile:    "../../pkg/pki/testdata/multilevelpki/int2-cert.pem",
				signingKeyFile:     "../../pkg/pki/testdata/multilevelpki/int2-key.pem",
				workloadCertTTL:    time.Hour * 24,
				maxWorkloadCertTTL: time.Hour * 24 * 7,
				trustDomain:        "my.domain.com",
			},
			wantCert:        certPem,
			wantTrustDomain: "my.domain.com",
		},
	}
	for _, tt := range tests {
		opts = tt.opts
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			got := createCA(tt.args.client, stopCh)
			gotCert, _, _, _ := got.GetCAKeyCertBundle().GetAllPem()
			if !reflect.DeepEqual(gotCert, tt.wantCert) && !tt.opts.selfSignedCA {
				t.Errorf("cert PEM got = %v, want %v", string(gotCert), string(tt.wantCert))
			}
			if !reflect.DeepEqual(spiffe.GetTrustDomain(), tt.wantTrustDomain) {
				t.Errorf("spiffe trustDomain got = %v, want %v", spiffe.GetTrustDomain(), tt.wantTrustDomain)
			}
		})
	}
}
