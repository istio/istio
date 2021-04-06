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

package istioagent

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	testutil "istio.io/istio/pilot/test/util"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/security/pkg/credentialfetcher/plugin"
	camock "istio.io/istio/security/pkg/nodeagent/caclient/providers/mock"
	"istio.io/istio/security/pkg/nodeagent/test/mock"
	pkiutil "istio.io/istio/security/pkg/pki/util"
	"istio.io/istio/tests/util/leak"
	"istio.io/pkg/log"
)

func TestAgent(t *testing.T) {
	wd := t.TempDir()
	mktemp := t.TempDir
	// Normally we call leak checker first. Here we call it after TempDir to avoid the (extremely
	// rare) race condition of a certificate being written at the same time the cleanup occurs, which
	// causes the test to fail. By checking for the leak first, we ensure all of our activities have
	// fully cleanup up.
	// https://github.com/golang/go/issues/43547
	leak.Check(t)
	// We run in the temp dir to ensure that when we are writing to the hardcoded ./etc/certs we
	// don't collide with other tests
	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	certDir := filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot")
	fakeSpiffeID := "spiffe://cluster.local/ns/fake-namespace/sa/fake-sa"
	// As a hack, we are using the serving sets as client cert, so the identity here is istiod not a spiffe
	preProvisionID := "istiod.istio-system.svc"

	checkCertsWritten := func(t *testing.T, dir string) {
		retry.UntilSuccessOrFail(t, func() error {
			// Ensure we output the certs
			if names := filenames(t, dir); !reflect.DeepEqual(names, []string{"cert-chain.pem", "key.pem", "root-cert.pem"}) {
				return fmt.Errorf("expected certs to be output, but got %v", names)
			}
			return nil
		}, retry.Delay(time.Millisecond*10), retry.Timeout(time.Second*5))
	}

	t.Run("Kubernetes defaults", func(t *testing.T) {
		// XDS and CA are both using JWT authentication and TLS. Root certificates distributed in
		// configmap to each namespace.
		Setup(t).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
	})
	t.Run("RSA", func(t *testing.T) {
		// All of the other tests use ECC for speed. Here we make sure RSA still works
		Setup(t, func(a AgentTest) AgentTest {
			a.Security.ECCSigAlg = ""
			return a
		}).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
	})
	t.Run("Kubernetes defaults output key and cert", func(t *testing.T) {
		// same as "Kubernetes defaults", but also output the key and cert. This can be used for tools
		// that expect certs as files, like Prometheus.
		dir := mktemp()
		sds := Setup(t, func(a AgentTest) AgentTest {
			a.Security.OutputKeyCertToDir = dir
			a.Security.SecretRotationGracePeriodRatio = 1
			return a
		}).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)

		// Ensure we output the certs
		checkCertsWritten(t, dir)

		// We rotate immediately, so we expect these files to be constantly updated
		go sds[security.WorkloadKeyCertResourceName].DrainResponses()
		expectFileChanged(t, filepath.Join(dir, "cert-chain.pem"), filepath.Join(dir, "key.pem"))
	})
	t.Run("Kubernetes defaults - output key and cert without SDS", func(t *testing.T) {
		dir := mktemp()
		Setup(t, func(a AgentTest) AgentTest {
			a.Security.OutputKeyCertToDir = dir
			a.Security.SecretRotationGracePeriodRatio = 1
			return a
		})
		// We start the agent, but never send a single SDS request
		// This behavior is useful for supporting writing the certs to disk without Envoy
		checkCertsWritten(t, dir)

		// TODO: this does not actually work, rotation is tied to SDS currently
		// expectFileChanged(t, filepath.Join(dir, "cert-chain.pem"))
		// expectFileChanged(t, filepath.Join(dir, "key.pem"))
	})
	t.Run("File mounted certs", func(t *testing.T) {
		// User sets FileMountedCerts. They also need to set ISTIO_META_TLS_CLIENT* to specify the
		// file paths. CA communication is disabled. mTLS is always used for authentication with
		// Istiod, never JWT.
		dir := mktemp()
		copyCerts(t, dir)

		cfg := model.SdsCertificateConfig{
			CertificatePath:   filepath.Join(dir, "cert-chain.pem"),
			PrivateKeyPath:    filepath.Join(dir, "key.pem"),
			CaCertificatePath: filepath.Join(dir, "root-cert.pem"),
		}
		Setup(t, func(a AgentTest) AgentTest {
			// Ensure we use the mTLS certs for XDS
			a.XdsAuthenticator.Set("", preProvisionID)
			// Ensure we don't try to connect to CA
			a.CaAuthenticator.Set("", "")
			a.ProxyConfig.ProxyMetadata = map[string]string{}
			a.ProxyConfig.ProxyMetadata[MetadataClientCertChain] = filepath.Join(dir, "cert-chain.pem")
			a.ProxyConfig.ProxyMetadata[MetadataClientCertKey] = filepath.Join(dir, "key.pem")
			a.ProxyConfig.ProxyMetadata[MetadataClientRootCert] = filepath.Join(dir, "root-cert.pem")
			a.Security.FileMountedCerts = true
			return a
		}).Check(cfg.GetRootResourceName(), cfg.GetResourceName())
	})
	t.Run("Implicit file mounted certs", func(t *testing.T) {
		// User mounts certificates in /etc/certs (hardcoded path). CA communication is disabled.
		// mTLS is always used for authentication with Istiod, never JWT.
		copyCerts(t, "./etc/certs")
		t.Cleanup(func() {
			_ = os.RemoveAll("./etc/certs")
		})
		Setup(t, func(a AgentTest) AgentTest {
			// Ensure we use the token
			a.XdsAuthenticator.Set("fake", "")
			// Ensure we don't try to connect to CA
			a.CaAuthenticator.Set("", "")
			return a
		}).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
	})
	t.Run("VMs", func(t *testing.T) {
		// Bootstrap sets up a short lived JWT token and root certificate. The initial run will fetch
		// a certificate and write it to disk. This will be used (by mTLS authenticator) for future
		// requests to both the CA and XDS.
		dir := mktemp()
		t.Run("initial run", func(t *testing.T) {
			a := Setup(t, func(a AgentTest) AgentTest {
				a.XdsAuthenticator.Set("", fakeSpiffeID)
				a.Security.OutputKeyCertToDir = dir
				a.Security.ProvCert = dir
				return a
			})
			a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)

			// Switch out our auth to only allow mTLS. In practice, the real server would allow JWT, but we
			// don't have a good way to expire JWTs here. Instead, we just deny all JWTs to ensure mTLS is used
			a.CaAuthenticator.Set("", filepath.Join(dir, "cert-chain.pem"))
			a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
		})
		t.Run("reboot", func(t *testing.T) {
			// Switch the JWT to a bogus path, to simulate the VM being rebooted
			a := Setup(t, func(a AgentTest) AgentTest {
				a.XdsAuthenticator.Set("", fakeSpiffeID)
				a.CaAuthenticator.Set("", fakeSpiffeID)
				a.Security.OutputKeyCertToDir = dir
				a.Security.ProvCert = dir
				a.Security.JWTPath = "bogus"
				return a
			})
			// Ensure we can still make requests
			a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
			a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
		})
	})
	t.Run("VMs to etc/certs", func(t *testing.T) {
		// Handle special edge case where we output certs to /etc/certs, which has magic implicit
		// reading from file logic
		dir := "./etc/certs"
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(dir)
		})
		a := Setup(t, func(a AgentTest) AgentTest {
			a.XdsAuthenticator.Set("", fakeSpiffeID)
			a.Security.OutputKeyCertToDir = dir
			a.Security.ProvCert = dir
			a.Security.SecretRotationGracePeriodRatio = 1
			return a
		})
		sds := a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)

		// Ensure we output the certs
		checkCertsWritten(t, dir)

		// The provisioning certificates should not be touched
		go sds[security.WorkloadKeyCertResourceName].DrainResponses()
		expectFileChanged(t, filepath.Join(dir, "cert-chain.pem"), filepath.Join(dir, "key.pem"))
	})
	t.Run("VMs provisioned certificates - short lived", func(t *testing.T) {
		// User has certificates pre-provisioned on the VM by some sort of tooling, pointed to by
		// PROV_CERT. These are used for mTLS auth with XDS and CA. Certificates are short lived,
		// OUTPUT_CERT = PROV_CERT. This is the same as "VMs", just skipping the initial
		// JWT exchange.
		dir := mktemp()
		copyCerts(t, dir)

		sds := Setup(t, func(a AgentTest) AgentTest {
			a.CaAuthenticator.Set("", preProvisionID)
			a.XdsAuthenticator.Set("", fakeSpiffeID)
			a.Security.OutputKeyCertToDir = dir
			a.Security.ProvCert = dir
			a.Security.SecretRotationGracePeriodRatio = 1
			return a
		}).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)

		// The provisioning certificates should not be touched
		go sds[security.WorkloadKeyCertResourceName].DrainResponses()
		expectFileChanged(t, filepath.Join(dir, "cert-chain.pem"), filepath.Join(dir, "key.pem"))
	})
	t.Run("VMs provisioned certificates - long lived", func(t *testing.T) {
		// User has certificates pre-provisioned on the VM by some sort of tooling, pointed to by
		// PROV_CERT. These are used for mTLS auth with XDS and CA. Certificates are long lived, we
		// always use the same certificate for control plane authentication and the short lived
		// certificates returned from the CA for workload authentication
		dir := mktemp()
		copyCerts(t, dir)

		sds := Setup(t, func(a AgentTest) AgentTest {
			a.CaAuthenticator.Set("", preProvisionID)
			a.XdsAuthenticator.Set("", preProvisionID)
			a.Security.ProvCert = dir
			a.Security.SecretRotationGracePeriodRatio = 1
			return a
		}).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)

		// The provisioning certificates should not be touched
		go sds[security.WorkloadKeyCertResourceName].DrainResponses()
		expectFileUnchanged(t, filepath.Join(dir, "cert-chain.pem"), filepath.Join(dir, "key.pem"))
	})
	t.Run("Token exchange", func(t *testing.T) {
		// This is used in environments where the CA expects a different token type than K8s jwt, and
		// exchanges it for another form using TokenExchanger
		Setup(t, func(a AgentTest) AgentTest {
			a.CaAuthenticator.Set("some-token", "")
			a.Security.TokenExchanger = camock.NewMockTokenExchangeServer(map[string]string{"fake": "some-token"})
			return a
		}).Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
	})
	t.Run("Token exchange with credential fetcher", func(t *testing.T) {
		// This is used with platform credentials, where the platform provides some underlying
		// identity (typically for VMs, not Kubernetes), which is exchanged with TokenExchanger
		// before sending to the CA.
		dir := mktemp()
		a := Setup(t, func(a AgentTest) AgentTest {
			a.CaAuthenticator.Set("some-token", "")
			a.XdsAuthenticator.Set("", fakeSpiffeID)
			a.Security.TokenExchanger = camock.NewMockTokenExchangeServer(map[string]string{"platform-cred": "some-token"})
			a.Security.CredFetcher = plugin.CreateMockPlugin("platform-cred")
			a.Security.OutputKeyCertToDir = dir
			a.Security.ProvCert = dir
			a.AgentConfig.XDSRootCerts = filepath.Join(certDir, "root-cert.pem")
			return a
		})

		a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
	})
	t.Run("Token exchange with credential fetcher downtime", func(t *testing.T) {
		// This ensures our pre-warming is resilient to temporary downtime of the CA
		dir := mktemp()
		a := Setup(t, func(a AgentTest) AgentTest {
			// Make CA deny all requests to simulate downtime
			a.CaAuthenticator.Set("", "")
			a.XdsAuthenticator.Set("", fakeSpiffeID)
			a.Security.TokenExchanger = camock.NewMockTokenExchangeServer(map[string]string{"platform-cred": "some-token"})
			a.Security.CredFetcher = plugin.CreateMockPlugin("platform-cred")
			a.Security.OutputKeyCertToDir = dir
			a.Security.ProvCert = dir
			a.AgentConfig.XDSRootCerts = filepath.Join(certDir, "root-cert.pem")
			return a
		})

		go func() {
			// Wait until we get a failure
			if err := retry.UntilSuccess(func() error {
				if a.CaAuthenticator.Failures.Load() < 2 {
					return fmt.Errorf("not enough failures yet")
				}
				return nil
			}); err != nil {
				// never got failures, we cannot fail in goroutine so just log it and the outer
				// function will fail
				log.Error(err)
				return
			}
			// Bring the CA back up
			a.CaAuthenticator.Set("some-token", "")
		}()

		a.Check(security.WorkloadKeyCertResourceName, security.RootCertReqResourceName)
	})
}

type AgentTest struct {
	t                *testing.T
	ProxyConfig      meshconfig.ProxyConfig
	Security         security.Options
	AgentConfig      AgentOptions
	XdsAuthenticator *security.FakeAuthenticator
	CaAuthenticator  *security.FakeAuthenticator

	agent *Agent
}

func Setup(t *testing.T, opts ...func(a AgentTest) AgentTest) *AgentTest {
	d := t.TempDir()
	resp := AgentTest{
		t:                t,
		XdsAuthenticator: security.NewFakeAuthenticator("xds").Set("fake", ""),
		CaAuthenticator:  security.NewFakeAuthenticator("ca").Set("fake", ""),
	}
	// Run through opts one time just to get the authenticators.
	for _, opt := range opts {
		resp = opt(resp)
	}
	ca := setupCa(t, resp.CaAuthenticator)
	resp.Security = security.Options{
		WorkloadUDSPath:   filepath.Join(d, "SDS"),
		CAEndpoint:        ca.URL,
		CAProviderName:    "Citadel",
		TrustDomain:       "cluster.local",
		JWTPath:           filepath.Join(env.IstioSrc, "pkg/istio-agent/testdata/token"),
		WorkloadNamespace: "namespace",
		ServiceAccount:    "sa",
		// Signing in 2048 bit RSA is extremely slow when running with -race enabled, sometimes taking 5s+ in
		// our CI, causing flakes. We use ECC as the default to speed this up.
		ECCSigAlg: string(pkiutil.EcdsaSigAlg),
	}
	resp.ProxyConfig = mesh.DefaultProxyConfig()
	resp.ProxyConfig.DiscoveryAddress = setupDiscovery(t, resp.XdsAuthenticator, ca.KeyCertBundle.GetRootCertPem())
	rootCert := filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/root-cert.pem")
	resp.AgentConfig = AgentOptions{
		ProxyXDSViaAgent: true,
		CARootCerts:      rootCert,
		XDSRootCerts:     rootCert,
		XdsUdsPath:       filepath.Join(d, "XDS"),
	}
	// Run through opts again to apply settings
	for _, opt := range opts {
		resp = opt(resp)
	}
	a := NewAgent(&resp.ProxyConfig, &resp.AgentConfig, &resp.Security)
	t.Cleanup(a.Close)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	resp.agent = a
	return &resp
}

func (a *AgentTest) Check(expectedSDS ...string) map[string]*xds.AdsTest {
	// Ensure we can send XDS requests
	meta := proxyConfigToMetadata(a.t, a.ProxyConfig)

	// We add a retry around XDS since some of the auth methods are eventually consistent, relying on
	// the CSR flow to complete first. This mirrors Envoy, which will also retry indefinitely
	var resp *discovery.DiscoveryResponse
	retry.UntilSuccessOrFail(a.t, func() error {
		return test.Wrap(func(t test.Failer) {
			conn := setupDownstreamConnectionUDS(t, a.AgentConfig.XdsUdsPath)
			xdsc := xds.NewAdsTest(t, conn).WithMetadata(meta)
			resp = xdsc.RequestResponseAck(nil)
		})
	}, retry.Timeout(time.Second*15), retry.Delay(time.Millisecond*200))

	sdsStreams := map[string]*xds.AdsTest{}
	gotKeys := []string{}
	for _, res := range xdstest.ExtractSecretResources(a.t, resp.Resources) {
		sds := xds.NewSdsTest(a.t, setupDownstreamConnectionUDS(a.t, a.Security.WorkloadUDSPath)).
			WithMetadata(meta).
			WithTimeout(time.Second * 20) // CSR can be extremely slow with race detection enabled due to 2048 RSA
		sds.RequestResponseAck(&discovery.DiscoveryRequest{ResourceNames: []string{res}})
		sdsStreams[res] = sds
		gotKeys = append(gotKeys, res)
	}
	if !reflect.DeepEqual(expectedSDS, gotKeys) {
		a.t.Errorf("expected SDS resources %v got %v", expectedSDS, gotKeys)
	}
	return sdsStreams
}

func copyCerts(t *testing.T, dir string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := file.Copy(filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/cert-chain.pem"), dir, "cert-chain.pem"); err != nil {
		t.Fatal(err)
	}
	if err := file.Copy(filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/key.pem"), dir, "key.pem"); err != nil {
		t.Fatal(err)
	}
	if err := file.Copy(filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/root-cert.pem"), dir, "root-cert.pem"); err != nil {
		t.Fatal(err)
	}
}

func expectFileChanged(t *testing.T, files ...string) {
	t.Helper()
	initials := [][]byte{}
	for _, f := range files {
		initials = append(initials, testutil.ReadFile(f, t))
	}
	retry.UntilSuccessOrFail(t, func() error {
		for i, f := range files {
			now, err := ioutil.ReadFile(f)
			if err != nil {
				return err
			}
			if reflect.DeepEqual(initials[i], now) {
				return fmt.Errorf("file is unchanged")
			}
		}
		return nil
	}, retry.Delay(time.Millisecond*10), retry.Timeout(time.Second*15))
}

func expectFileUnchanged(t *testing.T, files ...string) {
	t.Helper()
	initials := [][]byte{}
	for _, f := range files {
		initials = append(initials, testutil.ReadFile(f, t))
	}
	for attempt := 0; attempt < 10; attempt++ {
		time.Sleep(time.Millisecond * 10)
		for i, f := range files {
			now := testutil.ReadFile(f, t)
			if !reflect.DeepEqual(initials[i], now) {
				t.Fatalf("file is changed!")
			}
		}
	}
}

func filenames(t *testing.T, dir string) []string {
	t.Helper()
	res := []string{}
	contents, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range contents {
		res = append(res, f.Name())
	}
	sort.Strings(res)
	return res
}

func proxyConfigToMetadata(t *testing.T, proxyConfig meshconfig.ProxyConfig) model.NodeMetadata {
	t.Helper()
	m := map[string]interface{}{}
	for k, v := range proxyConfig.ProxyMetadata {
		if strings.HasPrefix(k, bootstrap.IstioMetaPrefix) {
			m[strings.TrimPrefix(k, bootstrap.IstioMetaPrefix)] = v
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	meta := model.NodeMetadata{}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	pc := model.NodeMetaProxyConfig(proxyConfig)
	meta.Namespace = "fake-namespace"
	meta.ServiceAccount = "fake-sa"
	meta.ProxyConfig = &pc
	return meta
}

func setupCa(t *testing.T, auth *security.FakeAuthenticator) *mock.CAServer {
	t.Helper()
	opt := tlsOptions(t)
	s, err := mock.NewCAServerWithKeyCert(0,
		testutil.ReadFile(filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/ca-key.pem"), t),
		testutil.ReadFile(filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/ca-cert.pem"), t),
		opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.GRPCServer.Stop)

	s.Authenticators = []security.Authenticator{auth}

	return s
}

func tlsOptions(t *testing.T, extraRoots ...[]byte) grpc.ServerOption {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/cert-chain.pem"),
		filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	peerCertVerifier := spiffe.NewPeerCertVerifier()
	if err := peerCertVerifier.AddMappingFromPEM("cluster.local",
		testutil.ReadFile(filepath.Join(env.IstioSrc, "./tests/testdata/certs/pilot/root-cert.pem"), t)); err != nil {
		t.Fatal(err)
	}
	for _, r := range extraRoots {
		if err := peerCertVerifier.AddMappingFromPEM("cluster.local", r); err != nil {
			t.Fatal(err)
		}
	}
	return grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    peerCertVerifier.GetGeneralCertPool(),
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			err := peerCertVerifier.VerifyPeerCert(rawCerts, verifiedChains)
			if err != nil {
				log.Infof("Could not verify certificate: %v", err)
			}
			return err
		},
	}))
}

func setupDiscovery(t *testing.T, auth *security.FakeAuthenticator, certPem []byte) string {
	t.Helper()

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	opt := tlsOptions(t, certPem)
	// Set up a simple service to make sure we have mTLS requested
	ds := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: ServiceEntry
metadata:
  name: app
  namespace: default
spec:
  hosts:
  - app.com
  ports:
  - number: 80
    name: http
    protocol: HTTP
---
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: plaintext
  namespace: default
spec:
  host: app.com
  trafficPolicy:
    tls:
      mode: ISTIO_MUTUAL
`})
	ds.Discovery.Authenticators = []security.Authenticator{auth}
	grpcServer := grpc.NewServer(opt)
	ds.Discovery.Register(grpcServer)
	go func() {
		_ = grpcServer.Serve(l)
	}()
	t.Cleanup(grpcServer.Stop)
	return net.JoinHostPort("localhost", fmt.Sprint(l.Addr().(*net.TCPAddr).Port))
}
