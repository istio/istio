package common

// TlsSettings defines TLS configuration for Echo server
type TlsSettings struct {
	RootCert   string
	ClientCert string
	Key        string
}
