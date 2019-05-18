package agent

import (
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"istio.io/istio/security/pkg/nodeagent/sds"
	"istio.io/istio/security/pkg/testing/sdsc"
)

var (
	// skip = env.RegisterBoolVar("CITADEL_SKIP_AGENT_TEST", true,
	// 	"Skip the test util we are able to setup the citadel agent in test").Get()
	// TODO: should init the tmp path inside the test.
	// sdsServer = env.RegisterStringVar("CITADEL_SERVER_UDS_PATH", "",
	// 	"The agent server uds path.")
	skip = flag.Bool("citadelagent.sds.skip", true, "whether skipping the citadel agent.")
)

func startAgentServer(t *testing.T) (string, *sds.Server) {
	t.Helper()
	tmpfile, err := ioutil.TempFile("/tmp", "agent-server*")
	if err != nil {
		t.Errorf("failed to create uds socket")
	}
	server, err := sds.NewServer(sds.Options{
		WorkloadUDSPath:   tmpfile.Name(),
		EnableWorkloadSDS: true,
	}, nil, nil)
	if err != nil {
		t.Errorf("failed to create sds service %v", err)
	}
	return tmpfile.Name(), server
}

// TODO(incfly): refactor node_agent_k8s/main.go and start a serve.
func TestAgentFailsRequestWithoutToken(t *testing.T) {
	if *skip {
		t.Skip("Test is skipped until Citadel agent is setup in test.")
	}
	uds, server := startAgentServer(t)
	defer server.Stop()
	fmt.Println("jianfieh debug uds ", uds)
	// time.Sleep(3 * time.Second)
	client, err := sdsc.NewClient(sdsc.ClientOptions{
		ServerAddress: uds,
	})
	if err != nil {
		t.Errorf("failed to create sds client")
	}
	client.Start()
	defer client.Stop()
	_, err = client.Send()
	errmsg := "no credential token"
	_, err = client.WaitForUpdate(3 * time.Second)
	if err == nil || strings.Contains(err.Error(), errmsg) {
		t.Errorf("got [%v], want error with substring [%v]", err, errmsg)
	}
}
