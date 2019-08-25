package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"flag"
	"net/url"
	"os"

	squarejwt "gopkg.in/square/go-jose.v2/jwt"

	"github.com/hashicorp/errwrap"
)

var (
	// PluginMetadataModeEnv is an ENV name used to disable TLS communication
	// to bootstrap mounting plugins.
	PluginMetadataModeEnv = "VAULT_PLUGIN_METADATA_MODE"

	// PluginUnwrapTokenEnv is the ENV name used to pass unwrap tokens to the
	// plugin.
	PluginUnwrapTokenEnv = "VAULT_UNWRAP_TOKEN"
)

// PluginAPIClientMeta is a helper that plugins can use to configure TLS connections
// back to Vault.
type PluginAPIClientMeta struct {
	// These are set by the command line flags.
	flagCACert     string
	flagCAPath     string
	flagClientCert string
	flagClientKey  string
	flagInsecure   bool
}

// FlagSet returns the flag set for configuring the TLS connection
func (f *PluginAPIClientMeta) FlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("vault plugin settings", flag.ContinueOnError)

	fs.StringVar(&f.flagCACert, "ca-cert", "", "")
	fs.StringVar(&f.flagCAPath, "ca-path", "", "")
	fs.StringVar(&f.flagClientCert, "client-cert", "", "")
	fs.StringVar(&f.flagClientKey, "client-key", "", "")
	fs.BoolVar(&f.flagInsecure, "tls-skip-verify", false, "")

	return fs
}

// GetTLSConfig will return a TLSConfig based off the values from the flags
func (f *PluginAPIClientMeta) GetTLSConfig() *TLSConfig {
	// If we need custom TLS configuration, then set it
	if f.flagCACert != "" || f.flagCAPath != "" || f.flagClientCert != "" || f.flagClientKey != "" || f.flagInsecure {
		t := &TLSConfig{
			CACert:        f.flagCACert,
			CAPath:        f.flagCAPath,
			ClientCert:    f.flagClientCert,
			ClientKey:     f.flagClientKey,
			TLSServerName: "",
			Insecure:      f.flagInsecure,
		}

		return t
	}

	return nil
}

// VaultPluginTLSProvider is run inside a plugin and retrieves the response
// wrapped TLS certificate from vault. It returns a configured TLS Config.
func VaultPluginTLSProvider(apiTLSConfig *TLSConfig) func() (*tls.Config, error) {
	if os.Getenv(PluginMetadataModeEnv) == "true" {
		return nil
	}

	return func() (*tls.Config, error) {
		unwrapToken := os.Getenv(PluginUnwrapTokenEnv)

		parsedJWT, err := squarejwt.ParseSigned(unwrapToken)
		if err != nil {
			return nil, errwrap.Wrapf("error parsing wrapping token: {{err}}", err)
		}

		var allClaims = make(map[string]interface{})
		if err = parsedJWT.UnsafeClaimsWithoutVerification(&allClaims); err != nil {
			return nil, errwrap.Wrapf("error parsing claims from wrapping token: {{err}}", err)
		}

		addrClaimRaw, ok := allClaims["addr"]
		if !ok {
			return nil, errors.New("could not validate addr claim")
		}
		vaultAddr, ok := addrClaimRaw.(string)
		if !ok {
			return nil, errors.New("could not parse addr claim")
		}
		if vaultAddr == "" {
			return nil, errors.New(`no vault api_addr found`)
		}

		// Sanity check the value
		if _, err := url.Parse(vaultAddr); err != nil {
			return nil, errwrap.Wrapf("error parsing the vault api_addr: {{err}}", err)
		}

		// Unwrap the token
		clientConf := DefaultConfig()
		clientConf.Address = vaultAddr
		if apiTLSConfig != nil {
			err := clientConf.ConfigureTLS(apiTLSConfig)
			if err != nil {
				return nil, errwrap.Wrapf("error configuring api client {{err}}", err)
			}
		}
		client, err := NewClient(clientConf)
		if err != nil {
			return nil, errwrap.Wrapf("error during api client creation: {{err}}", err)
		}

		secret, err := client.Logical().Unwrap(unwrapToken)
		if err != nil {
			return nil, errwrap.Wrapf("error during token unwrap request: {{err}}", err)
		}
		if secret == nil {
			return nil, errors.New("error during token unwrap request: secret is nil")
		}

		// Retrieve and parse the server's certificate
		serverCertBytesRaw, ok := secret.Data["ServerCert"].(string)
		if !ok {
			return nil, errors.New("error unmarshalling certificate")
		}

		serverCertBytes, err := base64.StdEncoding.DecodeString(serverCertBytesRaw)
		if err != nil {
			return nil, errwrap.Wrapf("error parsing certificate: {{err}}", err)
		}

		serverCert, err := x509.ParseCertificate(serverCertBytes)
		if err != nil {
			return nil, errwrap.Wrapf("error parsing certificate: {{err}}", err)
		}

		// Retrieve and parse the server's private key
		serverKeyB64, ok := secret.Data["ServerKey"].(string)
		if !ok {
			return nil, errors.New("error unmarshalling certificate")
		}

		serverKeyRaw, err := base64.StdEncoding.DecodeString(serverKeyB64)
		if err != nil {
			return nil, errwrap.Wrapf("error parsing certificate: {{err}}", err)
		}

		serverKey, err := x509.ParseECPrivateKey(serverKeyRaw)
		if err != nil {
			return nil, errwrap.Wrapf("error parsing certificate: {{err}}", err)
		}

		// Add CA cert to the cert pool
		caCertPool := x509.NewCertPool()
		caCertPool.AddCert(serverCert)

		// Build a certificate object out of the server's cert and private key.
		cert := tls.Certificate{
			Certificate: [][]byte{serverCertBytes},
			PrivateKey:  serverKey,
			Leaf:        serverCert,
		}

		// Setup TLS config
		tlsConfig := &tls.Config{
			ClientCAs:  caCertPool,
			RootCAs:    caCertPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
			// TLS 1.2 minimum
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
			ServerName:   serverCert.Subject.CommonName,
		}
		tlsConfig.BuildNameToCertificate()

		return tlsConfig, nil
	}
}
