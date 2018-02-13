package tcpFilterPeriodicalReport

import (
	"fmt"
	"testing"

	"istio.io/istio/mixer/test/client/env"
)

// Report attributes from a good POST request
const deltaReportAttributesOkPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "source.port": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "connection.mtls": false,
  "connection.received.bytes": 191,
  "connection.received.bytes_total": 191,
  "connection.sent.bytes": 0,
  "connection.sent.bytes_total": 0
}
`
const finalReportAttributesOkPost = `
{
  "context.protocol": "tcp",
  "context.time": "*",
  "mesh1.ip": "[1 1 1 1]",
  "source.ip": "[127 0 0 1]",
  "source.port": "*",
  "target.uid": "POD222",
  "target.namespace": "XYZ222",
  "destination.ip": "[127 0 0 1]",
  "destination.port": "*",
  "connection.mtls": false,
  "connection.received.bytes": 0,
  "connection.received.bytes_total": 191,
  "connection.sent.bytes": 138,
  "connection.sent.bytes_total": 138,
	"connection.duration": "*"
}
`

func TestTCPMixerFilterPeriodicalReport(t *testing.T) {
	s := env.NewTestSetup(env.TCPMixerFilterPeriodicalReportTest, t)
	env.SetTCPReportInterval(s.V2().TCPServerConf, 1)
	if err := s.SetUp(); err != nil {
		t.Fatalf("Failed to setup test: %v", err)
	}
	defer s.TearDown()

	url := fmt.Sprintf("http://localhost:%d/slowresponse", s.Ports().TCPProxyPort)

	tag := "OKPost"
	if _, _, err := env.ShortLiveHTTPPost(url, "text/plain", "Get Slow Response"); err != nil {
		t.Errorf("Failed in request %s: %v", tag, err)
	}

	s.VerifyReport("deltaReport", deltaReportAttributesOkPost)
	s.VerifyReport("finalReport", finalReportAttributesOkPost)
}
