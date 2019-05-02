package envvar

// Set of environment variables for VM Citadel Agent
const (
	// Name of the org for the cert.
	OrgName = "ISTIO_CA_ORG_NAME"
	// The requested TTL for the workload
	RequestedCertTTL = "ISTIO_CERT_TTL"
	// Size of the generated private key
	RSAKeySize = "ISTIO_RSA_KEY_SIZE"
	// Name of the CA, e.g. Citadel.
	CAProvider = "ISTIO_CA_PROVIDER"
	// CA endpoint.
	CAAddress = "ISTIO_CA_ADDR"
	// Whether this is a new or old protocol
	CAProtocol = "ISTIO_CA_PROTOCOL"
	// The environment in which the node agent run, e.g. GCP, on premise, etc.
	NodeEnv = "ISTIO_NODE_ENV"
	// The platform in which the node agent run, e.g. vm or k8s.
	NodePlatform = "ISTIO_NODE_PLATFORM"
	// Citadel Agent identity cert file
	CertChainFile = "ISTIO_CERT_CHAIN_FILE"
	// Citadel Agent private key file
	KeyFile = "ISTIO_KEY_FILE"
	// Citadel Agent root cert file
	RootCertFile = "ISTIo_ROOT_CERT_FILE"
	// Enable dual-use mode. Generates certificates with a CommonName identical to the SAN
	DualUse = "ISTIO_DUAL_USE"
	// Audience value from the mapper.
	MapperAudience = "ISTIO_MAPPER_AUDIENCE"
)
