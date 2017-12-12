package cloudfoundry

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/validator.v2"
	yaml "gopkg.in/yaml.v2"
)

type CopilotConfig struct {
	ServerCACertPath string        `yaml:"server_ca_cert_path" validate:"nonzero"`
	ClientCertPath   string        `yaml:"client_cert_path" validate:"nonzero"`
	ClientKeyPath    string        `yaml:"client_key_path" validate:"nonzero"`
	Address          string        `yaml:"address" validate:"nonzero"`
	PollInterval     time.Duration `yaml:"poll_interval" validate:"nonzero"`
}

type Config struct {
	Copilot CopilotConfig `yaml:"copilot"`
}

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

func (c *Config) Save(path string) error {
	configBytes, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(path, configBytes, 0600)
}

func (c *Config) ClientTLSConfig() (*tls.Config, error) {
	clientCert, err := tls.LoadX509KeyPair(c.Copilot.ClientCertPath, c.Copilot.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("parsing client cert/key: %s", err)
	}

	ServerCABytes, err := ioutil.ReadFile(c.Copilot.ServerCACertPath)
	if err != nil {
		return nil, fmt.Errorf("loading server CAs: %s", err)
	}
	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(ServerCABytes); !ok {
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
