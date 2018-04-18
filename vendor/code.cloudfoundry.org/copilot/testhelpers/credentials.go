package testhelpers

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/square/certstrap/pkix"
)

type Credentials struct {
	CA, Key, Cert []byte
}

type MTLSCredentials struct {
	Server      *Credentials
	Client      *Credentials
	OtherClient *Credentials
	TempDir     string
}

func GenerateMTLS() MTLSCredentials {
	return MTLSCredentials{
		Server:      generateCredentials("serverCA", "CopilotServer"),
		Client:      generateCredentials("clientCA", "CopilotClient"),
		OtherClient: generateCredentials("otherClientCA", "OtherCopilotClient"),
		TempDir:     TempDir(),
	}
}

func (m MTLSCredentials) CleanupTempFiles() {
	if len(m.TempDir) > 0 {
		assertNoError(os.RemoveAll(m.TempDir))
	}
}

func generateCredentials(caCommonName string, commonName string) *Credentials {
	rootKey, err := pkix.CreateRSAKey(1024)
	assertNoError(err)
	certAuthority, err := pkix.CreateCertificateAuthority(rootKey, "some-ou", time.Now().Add(1*time.Hour), "some-org", "some-country", "", "", caCommonName)
	assertNoError(err)

	key, err := pkix.CreateRSAKey(1024)
	assertNoError(err)
	csr, err := pkix.CreateCertificateSigningRequest(key, "some-ou", []net.IP{net.IPv4(127, 0, 0, 1)}, nil, "some-org", "some-country", "", "", commonName)
	assertNoError(err)
	cert, err := pkix.CreateCertificateHost(certAuthority, rootKey, csr, time.Now().Add(1*time.Hour))
	assertNoError(err)

	caBytes, err := certAuthority.Export()
	assertNoError(err)
	keyBytes, err := key.ExportPrivate()
	assertNoError(err)
	certBytes, err := cert.Export()
	assertNoError(err)

	return &Credentials{CA: caBytes, Key: keyBytes, Cert: certBytes}
}

func (m MTLSCredentials) ServerTLSConfig() *tls.Config {
	clientCAs := x509.NewCertPool()
	ok := clientCAs.AppendCertsFromPEM(m.Client.CA)
	if !ok {
		panic("failed appending certs")
	}

	serverCert, err := tls.X509KeyPair(m.Server.Cert, m.Server.Key)
	assertNoError(err)

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		Certificates: []tls.Certificate{serverCert},
	}
}

func (m MTLSCredentials) ServerTLSConfigForOtherClient() *tls.Config {
	otherClientCAs := x509.NewCertPool()
	ok := otherClientCAs.AppendCertsFromPEM(m.OtherClient.CA)
	if !ok {
		panic("failed appending certs")
	}

	serverCert, err := tls.X509KeyPair(m.Server.Cert, m.Server.Key)
	assertNoError(err)

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    otherClientCAs,
		Certificates: []tls.Certificate{serverCert},
	}
}

func (m MTLSCredentials) ClientTLSConfig() *tls.Config {
	rootCAs := x509.NewCertPool()
	ok := rootCAs.AppendCertsFromPEM(m.Server.CA)
	if !ok {
		panic("failed appending certs")
	}

	clientCert, err := tls.X509KeyPair(m.Client.Cert, m.Client.Key)
	assertNoError(err)

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{clientCert},
	}
}

func (m MTLSCredentials) OtherClientTLSConfig() *tls.Config {
	rootCAs := x509.NewCertPool()
	ok := rootCAs.AppendCertsFromPEM(m.Server.CA)
	if !ok {
		panic("failed appending certs")
	}

	otherClientCert, err := tls.X509KeyPair(m.OtherClient.Cert, m.OtherClient.Key)
	assertNoError(err)

	return &tls.Config{
		MinVersion:               tls.VersionTLS12,
		PreferServerCipherSuites: true,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{otherClientCert},
	}
}

type ServerTLSFilePaths struct {
	ClientCA, OtherClientCA, ServerCert, ServerKey string
}

func (m MTLSCredentials) saveTemp(shortName string, data []byte) string {
	pathOut := filepath.Join(m.TempDir, shortName)
	assertNoError(os.MkdirAll(filepath.Dir(pathOut), 0600))
	assertNoError(ioutil.WriteFile(pathOut, data, 0600))
	return pathOut
}

// CreateServerTLSFiles creates files that can be passed into the configuration
// for the server frontend
// Call m.CleanupTempFiles to remove these files
func (m MTLSCredentials) CreateServerTLSFiles() ServerTLSFilePaths {
	s := ServerTLSFilePaths{
		ClientCA:      m.saveTemp("client.ca", m.Client.CA),
		OtherClientCA: m.saveTemp("other-client.ca", m.OtherClient.CA),
		ServerCert:    m.saveTemp("server.crt", m.Server.Cert),
		ServerKey:     m.saveTemp("server.key", m.Server.Key),
	}
	return s
}

type ClientTLSFilePaths struct {
	ServerCA, ClientCert, ClientKey string
}

// CreateClientTLSFiles creates files that can be passed into the configuration
// for the server frontend
// Call m.CleanupTempFiles to remove these files
func (m MTLSCredentials) CreateClientTLSFiles() ClientTLSFilePaths {
	s := ClientTLSFilePaths{
		ServerCA:   m.saveTemp("server.ca", m.Server.CA),
		ClientCert: m.saveTemp("client.crt", m.Client.Cert),
		ClientKey:  m.saveTemp("client.key", m.Client.Key),
	}
	return s
}
