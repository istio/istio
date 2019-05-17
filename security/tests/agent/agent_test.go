package agent

import (
	"testing"

	"istio.io/istio/security/pkg/testing/sds"

	"istio.io/pkg/env"
)

var (
	skip = env.RegisterBoolVar("CITADEL_SKIP_AGENT_TEST", true,
		"Skip the test util we are able to setup the citadel agent in test").Get()
	// TODO: should init the tmp path inside the test.
	sdsServer = env.RegisterStringVar("CITADEL_SERVER_UDS_PATH", "",
		"The agent server uds path.")
)

func TestAgent(t *testing.T) {
	if skip {
		t.Skip("Test is skipped until Citadel agent is setup in test.")
	}
	_, err := sds.NewClient(sds.ClientOptions{
		ServerAddress: sdsServer.Get(),
	})
	if err != nil {
		t.Errorf("failed to create sds client")
	}
	// TODO(incfly): implement this, refactoring node_agent_k8s/main.go and start a serve.
}
