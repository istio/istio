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

package cloudfoundry

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	validator "gopkg.in/validator.v2"
	yaml "gopkg.in/yaml.v2"
)

// CopilotConfig describes how the Cloud Foundry platform adapter can connect to the Cloud Foundry Copilot
type CopilotConfig struct {
	ServerCACertPath string        `yaml:"server_ca_cert_path" validate:"nonzero"`
	ClientCertPath   string        `yaml:"client_cert_path" validate:"nonzero"`
	ClientKeyPath    string        `yaml:"client_key_path" validate:"nonzero"`
	Address          string        `yaml:"address" validate:"nonzero"`
	PollInterval     time.Duration `yaml:"poll_interval" validate:"nonzero"`
}

// Config for the Cloud Foundry platform adapter
type Config struct {
	Copilot CopilotConfig `yaml:"copilot"`
}

// LoadConfig reads configuration data from a YAML file
func LoadConfig(path string) (*Config, error) {
	cfgBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %s", err)
	}
	cfg := new(Config)
	err = yaml.Unmarshal(cfgBytes, cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %s", err)
	}
	err = validator.Validate(cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid config: %s", err)
	}
	return cfg, nil
}

// Save writes configuration data to a YAML file
func (c *Config) Save(path string) error {
	configBytes, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, configBytes, 0600)
}

// ClientTLSConfig returns a tls.Config needed to instantiate a copilot.IstioClient
func (c *Config) ClientTLSConfig() (*tls.Config, error) {
	clientCert, err := tls.LoadX509KeyPair(c.Copilot.ClientCertPath, c.Copilot.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("parsing client cert/key: %s", err)
	}

	serverCABytes, err := ioutil.ReadFile(c.Copilot.ServerCACertPath)
	if err != nil {
		return nil, fmt.Errorf("loading server CAs: %s", err)
	}
	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(serverCABytes); !ok {
		return nil, errors.New("parsing server CAs: invalid pem block")
	}

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		CurvePreferences: []tls.CurveID{tls.CurveP384},
		Certificates:     []tls.Certificate{clientCert},
		RootCAs:          serverCAs,
	}, nil
}
