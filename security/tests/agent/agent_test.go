package agent

import (
	"flag"
	"strings"
	"testing"
	"time"

	"istio.io/istio/security/pkg/testing/sdsc"
)

var (
	sdsUdsPath = flag.String("istio.testing.citadelagent.uds", "", "The agent server uds path.")
	skip       = flag.Bool("istio.testing.citadelagent.skip", true, "Whether skipping the citadel agent integration test.")
)

// TODO(incfly): refactor node_agent_k8s/main.go to be able to start a server within the test.
func TestAgentFailsRequestWithoutToken(t *testing.T) {
	if *skip {
		t.Skip("Test is skipped until Citadel agent is setup in test.")
	}
	client, err := sdsc.NewClient(sdsc.ClientOptions{
		ServerAddress: *sdsUdsPath,
	})
	if err != nil {
		t.Errorf("failed to create sds client")
	}
	client.Start()
	defer client.Stop()
	client.Send()
	errmsg := "no credential token"
	_, err = client.WaitForUpdate(3 * time.Second)
	if err == nil || strings.Contains(err.Error(), errmsg) {
		t.Errorf("got [%v], want error with substring [%v]", err, errmsg)
	}
}
