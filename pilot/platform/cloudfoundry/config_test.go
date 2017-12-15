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
	"net"
	"os"
	"reflect"
	"time"

	"code.cloudfoundry.org/copilot/testhelpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/platform/cloudfoundry"
)

var _ = Describe("Config", func() {
	var (
		cfgPath string
		config  *cloudfoundry.Config
	)
	BeforeEach(func() {
		cfgFile, err := ioutil.TempFile("", "testConfig")
		Expect(err).NotTo(HaveOccurred())
		cfgPath = cfgFile.Name()
		Expect(os.Remove(cfgPath)).To(Succeed())

		config = &cloudfoundry.Config{
			Copilot: cloudfoundry.CopilotConfig{
				ServerCACertPath: "server/ca/cert/path",
				ClientCertPath:   "client/cert/path",
				ClientKeyPath:    "client/key/path",
				Address:          "https://copilot.dns.address:port",
				PollInterval:     90 * time.Second,
			},
		}
	})

	AfterEach(func() {
		_ = os.Remove(cfgPath)
	})

	It("saves and loads a config via YAML", func() {
		Expect(config.Save(cfgPath)).To(Succeed())

		loadedConfig, err := cloudfoundry.LoadConfig(cfgPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(loadedConfig).To(Equal(config))
	})

	Context("when the file is not found", func() {
		It("returns a meaningful error", func() {
			_, err := cloudfoundry.LoadConfig(cfgPath)
			Expect(err).To(MatchError(HavePrefix("reading config file: open")))
		})
	})

	Context("when the file is not valid YAML", func() {
		BeforeEach(func() {
			Expect(ioutil.WriteFile(cfgPath, []byte("nope"), 0600)).To(Succeed())
		})

		It("returns a meaningful error", func() {
			_, err := cloudfoundry.LoadConfig(cfgPath)
			Expect(err).To(MatchError(HavePrefix("parsing config: yaml")))
		})
	})

	DescribeTable("required fields",
		func(fieldName string) {
			// zero out the named field of the config struct
			fieldValue := reflect.Indirect(reflect.ValueOf(&config.Copilot)).FieldByName(fieldName)
			fieldValue.Set(reflect.Zero(fieldValue.Type()))

			// save to the file
			Expect(config.Save(cfgPath)).To(Succeed())
			// attempt to load it
			_, err := cloudfoundry.LoadConfig(cfgPath)
			Expect(err).To(MatchError(HavePrefix("invalid config: Copilot." + fieldName)))
		},
		Entry("ServerCACertPath", "ServerCACertPath"),
		Entry("ClientCertPath", "ClientCertPath"),
		Entry("ClientKeyPath", "ClientKeyPath"),
		Entry("Address", "Address"),
		Entry("PollInterval", "PollInterval"),
	)

	Describe("building the client TLS config", func() {
		var (
			serverListener net.Listener
			creds          testhelpers.MTLSCredentials
		)

		BeforeEach(func() {
			creds = testhelpers.GenerateMTLS()
			tlsFiles := creds.CreateClientTLSFiles()

			config.Copilot.ServerCACertPath = tlsFiles.ServerCA
			config.Copilot.ClientCertPath = tlsFiles.ClientCert
			config.Copilot.ClientKeyPath = tlsFiles.ClientKey

		})

		AfterEach(func() {
			_ = os.Remove(config.Copilot.ServerCACertPath)
			_ = os.Remove(config.Copilot.ClientCertPath)
			_ = os.Remove(config.Copilot.ClientKeyPath)
		})

		Context("with a listening TLS server", func() {

			BeforeEach(func() {
				var err error
				serverTLSConfig := creds.ServerTLSConfig()
				serverListener, err = tls.Listen("tcp", "127.0.0.1:", serverTLSConfig)
				Expect(err).ToNot(HaveOccurred())

				go func() {
					for {
						conn, err := serverListener.Accept()
						if err != nil {
							return
						}
						b := make([]byte, 5)
						_, _ = conn.Read(b)
					}
				}()
			})
			AfterEach(func() {
				_ = serverListener.Close()
			})

			It("returns a valid and usable tls.Config", func() {
				tlsConfig, err := config.ClientTLSConfig()
				Expect(err).NotTo(HaveOccurred())
				Expect(tlsConfig).NotTo(BeNil())

				conn, err := net.DialTimeout("tcp", serverListener.Addr().String(), 1*time.Second)
				Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = conn.Close()
				}()
				tlsConfig.ServerName = "127.0.0.1"
				tlsConn := tls.Client(conn, tlsConfig)
				_, err = tlsConn.Write([]byte("foo"))
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("sets secure values for configuration parameters", func() {
			tlsConfig, err := config.ClientTLSConfig()
			Expect(err).NotTo(HaveOccurred())

			Expect(tlsConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			Expect(tlsConfig.PreferServerCipherSuites).To(BeTrue())
			Expect(tlsConfig.CipherSuites).To(ConsistOf([]uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			}))
			Expect(tlsConfig.CurvePreferences).To(ConsistOf([]tls.CurveID{
				tls.CurveP384,
			}))
			Expect(tlsConfig.RootCAs).ToNot(BeNil())
			Expect(tlsConfig.RootCAs.Subjects()).To(ConsistOf(ContainSubstring("serverCA")))
		})

		Context("when the server CA file does not exist", func() {
			BeforeEach(func() {
				Expect(os.Remove(config.Copilot.ServerCACertPath)).To(Succeed())
			})
			It("returns a meaningful error", func() {
				_, err := config.ClientTLSConfig()
				Expect(err).To(MatchError(HavePrefix("loading server CAs: open")))
			})
		})
		Context("when the server CA PEM data is invalid", func() {
			BeforeEach(func() {
				Expect(ioutil.WriteFile(config.Copilot.ServerCACertPath, []byte("invalid pem"), 0600)).To(Succeed())
			})
			It("returns a meaningful error", func() {
				_, err := config.ClientTLSConfig()
				Expect(err).To(MatchError("parsing server CAs: invalid pem block"))
			})
		})

		Context("when the client cert PEM data is invalid", func() {
			BeforeEach(func() {
				Expect(ioutil.WriteFile(config.Copilot.ClientCertPath, []byte("invalid pem"), 0600)).To(Succeed())
			})
			It("returns a meaningful error", func() {
				_, err := config.ClientTLSConfig()
				Expect(err).To(MatchError(HavePrefix("parsing client cert/key: tls: failed to find any PEM data")))
			})
		})

		Context("when the client key PEM data is invalid", func() {
			BeforeEach(func() {
				Expect(ioutil.WriteFile(config.Copilot.ClientKeyPath, []byte("invalid pem"), 0600)).To(Succeed())
			})
			It("returns a meaningful error", func() {
				_, err := config.ClientTLSConfig()
				Expect(err).To(MatchError(HavePrefix("parsing client cert/key: tls: failed to find any PEM data")))
			})
		})
	})
})
