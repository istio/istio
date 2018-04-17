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

package cloudfoundry_test

import (
	"crypto/tls"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"code.cloudfoundry.org/copilot/testhelpers"
	"github.com/onsi/gomega"

	"istio.io/istio/pilot/pkg/serviceregistry/cloudfoundry"
)

type testState struct {
	// generated credentials for server and client
	creds testhelpers.MTLSCredentials

	// path on disk to store the config.yaml
	configFilePath string

	// object under test
	config *cloudfoundry.Config
}

func (s *testState) cleanup() {
	s.creds.CleanupTempFiles()
	_ = os.RemoveAll(s.configFilePath)
}

func newTestState() *testState {
	creds := testhelpers.GenerateMTLS()
	clientTLSFiles := creds.CreateClientTLSFiles()
	return &testState{
		creds:          creds,
		configFilePath: filepath.Join(creds.TempDir, "config.yaml"),
		config: &cloudfoundry.Config{
			Copilot: cloudfoundry.CopilotConfig{
				ServerCACertPath: clientTLSFiles.ServerCA,
				ClientCertPath:   clientTLSFiles.ClientCert,
				ClientKeyPath:    clientTLSFiles.ClientKey,
				Address:          "https://copilot.dns.address:port",
				PollInterval:     90 * time.Second,
			},
			ServicePort: 8080,
		},
	}
}

func TestConfig_SaveAndLoad(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newTestState()
	defer state.cleanup()

	err := state.config.Save(state.configFilePath)
	g.Expect(err).To(gomega.BeNil())

	loadedConfig, err := cloudfoundry.LoadConfig(state.configFilePath)
	g.Expect(err).To(gomega.BeNil())

	g.Expect(loadedConfig).To(gomega.Equal(state.config))
}

func TestConfig_Load_FileNotFound(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newTestState()
	defer state.cleanup()

	_, err := cloudfoundry.LoadConfig(state.configFilePath)

	g.Expect(err).NotTo(gomega.BeNil())
	g.Expect(err.Error()).To(gomega.HavePrefix("reading config file: open"))
}

func TestConfig_Load_BadYAML(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newTestState()
	defer state.cleanup()

	err := ioutil.WriteFile(state.configFilePath, []byte("bad yaml"), 0600)
	g.Expect(err).To(gomega.BeNil())

	_, err = cloudfoundry.LoadConfig(state.configFilePath)
	g.Expect(err.Error()).To(gomega.HavePrefix("parsing config: yaml"))
}

func TestConfig_Load_MissingRequiredFields(t *testing.T) {
	requiredFieldNames := []string{
		"ServerCACertPath",
		"ClientCertPath",
		"ClientKeyPath",
		"Address",
		"PollInterval",
	}
	for _, n := range requiredFieldNames {
		// don't close over the iterator: https://golang.org/doc/faq#closures_and_goroutines
		fieldName := n

		t.Run("required field "+fieldName, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			state := newTestState()
			defer state.cleanup()

			// zero out the named field of the config struct
			fieldValue := reflect.Indirect(reflect.ValueOf(&state.config.Copilot)).FieldByName(fieldName)
			fieldValue.Set(reflect.Zero(fieldValue.Type()))

			// save to the file
			err := state.config.Save(state.configFilePath)
			g.Expect(err).To(gomega.BeNil())

			// attempt to load it
			_, err = cloudfoundry.LoadConfig(state.configFilePath)
			g.Expect(err).NotTo(gomega.BeNil())
			g.Expect(err.Error()).To(gomega.HavePrefix("invalid config: Copilot." + fieldName))
		})
	}
}

func TestConfig_TLSClient_Connects(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newTestState()
	defer state.cleanup()

	// launch a TLS server
	serverListener, err := tls.Listen("tcp", "127.0.0.1:", state.creds.ServerTLSConfig())
	g.Expect(err).To(gomega.BeNil())

	msgs := make(chan string)
	go func() {
		for {
			conn, err2 := serverListener.Accept()
			if err2 != nil {
				return
			}
			b := make([]byte, 6)
			nBytesRead, _ := conn.Read(b)
			msgs <- string(b[:nBytesRead])
		}
	}()
	defer serverListener.Close()

	// generate client TLS config from the object under test
	clientTLSConfig, err := state.config.ClientTLSConfig()
	g.Expect(err).To(gomega.BeNil())

	clientTLSConfig.ServerName = "127.0.0.1" // a little hack to avoid name mismatch error

	// can use this TLS config to connect to the TLS server
	conn, err := tls.Dial("tcp", serverListener.Addr().String(), clientTLSConfig)
	g.Expect(err).To(gomega.BeNil())
	defer conn.Close()

	// can write some bytes to the server
	_, err = conn.Write([]byte("potato"))
	g.Expect(err).To(gomega.BeNil())

	receivedMsg := <-msgs
	g.Expect(receivedMsg).To(gomega.Equal("potato"))
}

func TestConfig_TLSClient_HasSecureParameters(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	state := newTestState()
	defer state.cleanup()

	tlsConfig, err := state.config.ClientTLSConfig()
	g.Expect(err).To(gomega.BeNil())

	g.Expect(tlsConfig.MinVersion).To(gomega.Equal(uint16(tls.VersionTLS12)))
	g.Expect(tlsConfig.PreferServerCipherSuites).To(gomega.BeTrue())
	g.Expect(tlsConfig.CipherSuites).To(gomega.ConsistOf([]uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	}))
	g.Expect(tlsConfig.CurvePreferences).To(gomega.Equal([]tls.CurveID{tls.CurveP384}))

	rawSubjectData := string(tlsConfig.RootCAs.Subjects()[0])
	if !strings.Contains(rawSubjectData, "serverCA") {
		t.Errorf("expected to find substring 'serverCA' in root CAs subject data")
	}
}

func TestConfig_TLSClient_Errors(t *testing.T) {
	// checking that we get useful errors when the config is broken in various ways
	breakageTypes := map[string]func(path string){
		"remove":       func(path string) { _ = os.Remove(path) },
		"writeBadData": func(path string) { _ = ioutil.WriteFile(path, []byte("bad-data"), 0600) },
	}

	testCases := []struct {
		field             string
		breakage          string
		expectedErrPrefix string
	}{
		{
			"ServerCACertPath",
			"remove",
			"loading server CAs: open",
		},
		{
			"ServerCACertPath",
			"writeBadData",
			"loading server CAs: failed to find any PEM data",
		},
		{
			"ClientCertPath",
			"remove",
			"loading client cert/key: open",
		},
		{
			"ClientCertPath",
			"writeBadData",
			"loading client cert/key: tls: failed to find any PEM data",
		},
		{
			"ClientKeyPath",
			"remove",
			"loading client cert/key: open",
		},
		{
			"ClientKeyPath",
			"writeBadData",
			"loading client cert/key: tls: failed to find any PEM data",
		},
	}

	for _, tc := range testCases {
		// don't close over the iterator: https://golang.org/doc/faq#closures_and_goroutines
		testCase := tc

		t.Run(testCase.field+" "+testCase.breakage, func(t *testing.T) {
			g := gomega.NewGomegaWithT(t)

			state := newTestState()
			defer state.cleanup()

			// the named field of the config struct
			fieldValue := reflect.Indirect(reflect.ValueOf(&state.config.Copilot)).FieldByName(testCase.field)
			path := fieldValue.String()

			// mess up the file
			breakageTypes[testCase.breakage](path)

			// attempt to construct the client config
			_, err := state.config.ClientTLSConfig()

			g.Expect(err).To(gomega.MatchError(gomega.HavePrefix(testCase.expectedErrPrefix)))
		})
	}
}
