// Copyright Istio Authors.
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

package dynamic

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/url"

	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"

	policypb "istio.io/api/policy/v1beta1"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	// file path to the public CA certs
	publicCACertsFile = "/etc/ssl/certs/ca-certificates.crt"
)

type authHelper struct {
	authCfg          *policypb.Authentication
	skipVerification bool
}

type headerToken struct {
	header string
	prefix string
	token  []byte
}

func (c *headerToken) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	if c.prefix == "" {
		return map[string]string{
			c.header: string(c.token),
		}, nil
	}
	return map[string]string{
		c.header: c.prefix + " " + string(c.token),
	}, nil
}

func (c *headerToken) RequireTransportSecurity() bool {
	return true
}

func loadCerts(caFile string) (*x509.CertPool, error) {
	// load ca cert.
	caCertPool := x509.NewCertPool()
	// load public root cert.
	publicCerts, err := ioutil.ReadFile(publicCACertsFile)
	if err != nil {
		log.Warnf("cannot load public root cert: %v", err)
	} else {
		caCertPool.AppendCertsFromPEM(publicCerts)
	}

	// load ca provided by file, such as istio CA certs.
	if caFile != "" {
		caCerts, err := ioutil.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		caCertPool.AppendCertsFromPEM(caCerts)
	}
	return caCertPool, nil
}

func getWhitelistSAN(cert []byte) []*url.URL {
	var whitelistSAN []*url.URL
	c, err := x509.ParseCertificate(cert)
	if err != nil {
		return whitelistSAN
	}
	return append(whitelistSAN, c.URIs...)
}

var bypassVerificationVar = env.RegisterBoolVar("BYPASS_OOP_MTLS_SAN_VERIFICATION", false, "Whether or not to validate SANs for out-of-process adapters auth.")

func buildMTLSDialOption(mtlsCfg *policypb.Mutual) ([]grpc.DialOption, error) {
	// load peer cert/key.
	pk := mtlsCfg.GetPrivateKey()
	cc := mtlsCfg.GetClientCertificate()
	peerCert, err := tls.LoadX509KeyPair(cc, pk)
	if err != nil {
		return nil, errors.Errorf("load peer cert/key error: %v", err)
	}

	// for simplicity, when using mtls, restrict out of process adapter cert subject to be mixer's spiffe identity
	// TODO(bianpengyuan) make this configurable if needed
	var whitelistSAN []*url.URL
	if peerCert.Certificate != nil && len(peerCert.Certificate) > 0 {
		whitelistSAN = getWhitelistSAN(peerCert.Certificate[0])
	}

	// Load certificates, include public certs and certs from config (such as istio CA certs).
	caCertPool, err := loadCerts(mtlsCfg.GetCaCertificates())
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS dial option: CA certificate cannot be loaded: %v", err)
	}

	customVerify := func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		certs := make([]*x509.Certificate, len(rawCerts))
		for i, asn1Data := range rawCerts {
			cert, err := x509.ParseCertificate(asn1Data)
			if err != nil {
				return errors.New("failed to parse certificate from server: " + err.Error())
			}
			certs[i] = cert
		}
		opts := x509.VerifyOptions{
			Roots:         caCertPool,
			Intermediates: x509.NewCertPool(),
		}

		for i, cert := range certs {
			if i == 0 {
				// do not add leaf cert to intermediate certs
				continue
			}
			opts.Intermediates.AddCert(cert)
		}
		if _, err = certs[0].Verify(opts); err != nil {
			return err
		}
		if bypassVerificationVar.Get() {
			log.Infof("BYPASS_OOP_MTLS_SAN_VERIFICATION=true - bypassing mtls custom SAN verification")
			return nil
		}
		for _, uri := range certs[0].URIs {
			for _, whitelisted := range whitelistSAN {
				if uri.String() == whitelisted.String() {
					return nil
				}
			}
		}
		return fmt.Errorf("failed to authenticate, cert SAN %v is not whitelisted", certs[0].URIs)
	}

	tc := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{peerCert},
		// For mtls, skip default cert verification and use the custom one, which basically replicates the
		// default verification logic. The only difference is that it checks subject alt name to be
		// whitelisted spiffe URI instead of checking DNS/server name.
		InsecureSkipVerify:    true,
		VerifyPeerCertificate: customVerify,
		ServerName:            mtlsCfg.ServerName,
	})
	return []grpc.DialOption{grpc.WithTransportCredentials(tc)}, nil
}

func buildOAuthToken(oauthCfg *policypb.OAuth) (oauth2.TokenSource, error) {
	sp := oauthCfg.ClientSecret
	if sp == "" {
		return nil, errors.New("oauth secret cannot be empty")
	}
	secret, err := ioutil.ReadFile(sp)
	if err != nil {
		return nil, err
	}
	c := &clientcredentials.Config{
		ClientID:     oauthCfg.ClientId,
		ClientSecret: string(secret),
		TokenURL:     oauthCfg.TokenUrl,
		Scopes:       oauthCfg.Scopes,
	}
	return c.TokenSource(context.Background()), nil
}

func buildAuthToken(token []byte, tlsCfg *policypb.Tls) (*headerToken, error) {
	if tlsCfg.TokenType == nil {
		return nil, errors.New("token type should be specified")
	}
	at := &headerToken{
		token: token,
	}
	switch tlsCfg.TokenType.(type) {
	case *policypb.Tls_AuthHeader_:
		ah := tlsCfg.GetAuthHeader()
		at.header = "authorization"
		if ah == policypb.PLAIN {
			at.prefix = ""
		} else if ah == policypb.BEARER {
			at.prefix = "Bearer"
		}
	case *policypb.Tls_CustomHeader:
		at.prefix = ""
		at.header = tlsCfg.GetCustomHeader()
	default:
		return nil, errors.New("authToken type cannot be empty")
	}
	return at, nil
}

func buildPerRPCCredentials(tlsCfg *policypb.Tls) (credentials.PerRPCCredentials, error) {
	switch tlsCfg.TokenSource.(type) {
	case *policypb.Tls_Oauth:
		ts, err := buildOAuthToken(tlsCfg.GetOauth())
		if err != nil {
			return nil, err
		}
		// nolint: govet
		return oauth.TokenSource{ts}, nil
	case *policypb.Tls_TokenPath:
		// read token from given path
		tp := tlsCfg.GetTokenPath()
		if tp == "" {
			return nil, errors.New("token path should not be empty")
		}
		token, err := ioutil.ReadFile(tp)
		if err != nil {
			return nil, err
		}

		// initialize per rpc credential based on token type
		ht, err := buildAuthToken(token, tlsCfg)
		return ht, err
	default:
		return nil, errors.New("unexpected tls token source type")
	}
}

func buildTLSDialOption(tlsCfg *policypb.Tls, skipVerify bool) ([]grpc.DialOption, error) {
	var opts []grpc.DialOption
	// Load certificates, include system certs and additional certs (such as istio CA certs) from file.
	caCertPool, err := loadCerts(tlsCfg.GetCaCertificates())
	if err != nil {
		return nil, fmt.Errorf("failed build tls dial option: ca cert cannot be load %v", err)
	}

	tc := credentials.NewTLS(&tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: skipVerify,
		ServerName:         tlsCfg.ServerName,
	})
	opts = append(opts, grpc.WithTransportCredentials(tc))

	// Load per RPC credential.
	perRPCCreds, err := buildPerRPCCredentials(tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("failed build tls dial option: cannot get grpc per rpc credentials %v", err)
	}
	opts = append(opts, grpc.WithPerRPCCredentials(perRPCCreds))
	return opts, nil
}

func (a *authHelper) getAuthOpt() (opts []grpc.DialOption, err error) {
	// TODO(bianpengyuan) pass in grpc connection context so that oauth client would share the same context.
	if a.authCfg == nil {
		return []grpc.DialOption{grpc.WithInsecure()}, nil
	}
	switch t := a.authCfg.AuthType.(type) {
	case *policypb.Authentication_Tls:
		return buildTLSDialOption(a.authCfg.GetTls(), a.skipVerification)
	case *policypb.Authentication_Mutual:
		return buildMTLSDialOption(a.authCfg.GetMutual())
	default:
		return nil, fmt.Errorf("authentication type is unexpected: %T", t)
	}
}

func getAuthHelper(ac *policypb.Authentication, sv bool) *authHelper {
	return &authHelper{
		authCfg:          ac,
		skipVerification: sv,
	}
}
