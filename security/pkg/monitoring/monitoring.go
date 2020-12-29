package monitoring

import "istio.io/pkg/monitoring"

var (
	RequestType = monitoring.MustCreateLabel("request_type")
)

const (
	TokenExchange = "token_exchange"
	CSR           = "csr"
)

var (
	NumOutgoingRetries = monitoring.NewSum(
		"num_outgoing_retries",
		"Number of outgoing retry requests (e.g. to a token exchange server, CA, etc.)",
		monitoring.WithLabels(RequestType))
)

func init() {
	monitoring.MustRegister(
		NumOutgoingRetries,
	)
}

func Reset() {
	NumOutgoingRetries.Record(0)
}
