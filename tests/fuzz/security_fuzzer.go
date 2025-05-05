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

package fuzz

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"

	fuzz "github.com/AdaLogics/go-fuzz-headers"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	pb "istio.io/api/security/v1alpha1"
	"istio.io/istio/pkg/security"
	mockca "istio.io/istio/security/pkg/pki/ca/mock"
	"istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/security/pkg/server/ca"
	"istio.io/istio/security/pkg/server/ca/authenticate"
)

func FuzzGenCSR(data []byte) int {
	f := fuzz.NewConsumer(data)
	certOptions := util.CertOptions{}
	err := f.GenerateStruct(&certOptions)
	if err != nil {
		return 0
	}
	_, _, _ = util.GenCSR(certOptions)
	return 1
}

func fuzzedCertChain(f *fuzz.ConsumeFuzzer) ([][]*x509.Certificate, error) {
	certChain := [][]*x509.Certificate{}
	withPkixExtension, err := f.GetBool()
	if err != nil {
		return certChain, err
	}
	if withPkixExtension {
		ids := []util.Identity{}
		err := f.GenerateStruct(&ids)
		if err != nil {
			return certChain, err
		}
		sanExt, err := util.BuildSANExtension(ids)
		if err != nil {
			return certChain, err
		}
		certChain = [][]*x509.Certificate{
			{
				{
					Extensions: []pkix.Extension{*sanExt},
				},
			},
		}
	}
	return certChain, nil
}

func FuzzCreateCertE2EUsingClientCertAuthenticator(data []byte) int {
	f := fuzz.NewConsumer(data)
	certChainBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	// Check that certChainBytes can be parsed successfully
	_, err = util.ParsePemEncodedCertificate(certChainBytes)
	if err != nil {
		return 0
	}
	rootCertBytes, err := f.GetBytes()
	if err != nil {
		return 0
	}
	// Check that rootCertBytes can be parsed successfully
	_, err = util.ParsePemEncodedCertificate(rootCertBytes)
	if err != nil {
		return 0
	}
	signedCert, err := f.GetBytes()
	if err != nil {
		return 0
	}

	auth := &authenticate.ClientCertAuthenticator{}
	kcb := util.NewKeyCertBundleFromPem(nil, nil, certChainBytes, rootCertBytes, nil)

	mockCa := &mockca.FakeCA{
		SignedCert:    signedCert,
		KeyCertBundle: kcb,
	}
	server, err := ca.New(mockCa, 1, []security.Authenticator{auth}, nil)
	if err != nil {
		return 0
	}
	csrString, err := f.GetString()
	if err != nil {
		return 0
	}
	request := &pb.IstioCertificateRequest{Csr: csrString}
	ctx := context.Background()

	certChain, err := fuzzedCertChain(f)
	if err != nil {
		return 0
	}
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{VerifiedChains: certChain},
	}

	mockIPAddr := &net.IPAddr{IP: net.IPv4(192, 168, 1, 1)}
	p := &peer.Peer{Addr: mockIPAddr, AuthInfo: tlsInfo}

	ctx = peer.NewContext(ctx, p)

	_, _ = server.CreateCertificate(ctx, request)
	return 1
}
