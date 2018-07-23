// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pilot

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"bytes"
	"text/template"
	"time"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/tests/util"
)

const (
	metricName              = "pilot_conflict_outbound_listener_tcp_over_current_tcp"
	listenerName            = "0.0.0.0:15151"
	conflictServiceTemplate = `
apiVersion: v1
kind: Namespace
metadata:
  name: {{.Namespace}}
---
apiVersion: v1
kind: Service
metadata:
  name: {{.ServiceName}}
  namespace: {{.Namespace}}
spec:
  clusterIP: None
  ports:
  - name: someport
    port: 15151
    protocol: TCP
    targetPort: somepodport
  selector:
    app: conflictapp
  sessionAffinity: None
  type: ClusterIP
`
)

var (
	messagePattern = regexp.MustCompile("^Listener=0.0.0.0:15151 AcceptedTCP=([0-9a-zA-Z-.]+) RejectedTCP=([0-9a-zA-Z-.]+) TCPServices=([0-9]+)$")
)

type serviceInfo struct {
	ServiceName string
	Namespace   string
	yaml        string
	t           *testing.T
}

func (i *serviceInfo) fullName() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", i.ServiceName, i.Namespace)
}

func (i *serviceInfo) start() {
	i.yaml = i.generateYaml()
	if err := util.KubeApplyContents("", i.yaml, tc.Kube.KubeConfig); err != nil {
		i.t.Fatal(err)
	}
}

func (i *serviceInfo) stop() {
	util.KubeDeleteContents("", i.yaml, tc.Kube.KubeConfig)
}

func (i *serviceInfo) generateYaml() string {
	var tmp bytes.Buffer
	err := template.Must(template.New("inject").Parse(conflictServiceTemplate)).Execute(&tmp, i)
	if err != nil {
		i.t.Fatal(err)
	}
	return tmp.String()
}

func TestListenerConflicts(t *testing.T) {
	if !tc.Kube.IsClusterWide() {
		t.Skip("Cluster-wide deployment is required for this test so that pilot will observe all namespaces.")
	}

	// Deploy 2 headless TCP services with conflicting ports.
	oldService := &serviceInfo{
		ServiceName: fmt.Sprintf("service1"),
		Namespace:   fmt.Sprintf("%s-conflict-1", tc.Info.RunID),
	}
	newService := &serviceInfo{
		ServiceName: fmt.Sprintf("service2"),
		Namespace:   fmt.Sprintf("%s-conflict-2", tc.Info.RunID),
	}

	// Create the older of thw two services. It's "old" because it will have an older creationTimestamp.
	oldService.start()
	defer oldService.stop()

	// Sleep a couple of seconds before pushing the next service. This ensures the creationTimestamps are different.
	time.Sleep(2 * time.Second)

	// Now create the "new" service.
	newService.start()
	defer newService.stop()

	for cluster := range tc.Kube.Clusters {
		testName := oldService.ServiceName
		runRetriableTest(t, cluster, testName, 5, func() error {
			pushStatus, pushStatusJSON, err := getPushStatus()
			if err != nil {
				return err
			}
			conflict, ok := pushStatus.ProxyStatus[metricName]
			if !ok {
				return fmt.Errorf("unable to find push status metric %s. PushStatusJSON=%s", metricName, pushStatusJSON)
			}
			event, ok := conflict[listenerName]
			if !ok {
				return fmt.Errorf("unable to find push status conflict %s. PushStatusJSON=%s", listenerName, pushStatusJSON)
			}

			accepted, rejected, numServices, err := parseEventMessageFields(event.Message)
			if err != nil {
				return err
			}

			if accepted == newService.fullName() {
				// We should never accept the port from the newer service.
				return fmt.Errorf("expected to reject listener for %s, but it was accepted. PushStatusJSON=%s", accepted, pushStatusJSON)
			}
			if accepted != oldService.fullName() {
				// Possibly a rejection for a different service? Just go to the next event.
				return fmt.Errorf("unexpected service accepted %s. PushStatusJSON=%s", metricName, pushStatusJSON)
			}

			// The old service was accepted. The rejection should be the new service.
			if rejected != newService.fullName() {
				return fmt.Errorf("expected to reject listener for %s, but rejected %s. PushStatusJSON=%s", newService.fullName(), rejected, pushStatusJSON)
			}
			if numServices != "1" {
				return fmt.Errorf("expected 1 TCP services, but found %s. PushStatusJSON=%s", numServices, pushStatusJSON)
			}

			return nil
		})
	}
}

func parseEventMessageFields(message string) (accepted string, rejected string, numServices string, err error) {
	matches := messagePattern.FindStringSubmatch(message)
	if len(matches) != 4 {
		err = fmt.Errorf("unexpected groups in message. Expected %d, found %d", 4, len(matches))
		return
	}

	accepted = matches[1]
	rejected = matches[2]
	numServices = matches[3]
	return
}

func getPushStatus() (*model.PushStatus, string, error) {
	pods, err := getPilotPods()
	if err != nil {
		return nil, "", err
	}
	pod := pods[0]
	result, err := util.PodExec(tc.Kube.Namespace, pod, "discovery", "curl http://127.0.0.1:8080/debug/push_status", true, tc.Kube.KubeConfig)
	if err != nil {
		return nil, "", err
	}

	pushStatusStartIndex := strings.Index(result, "{")
	if pushStatusStartIndex < 0 {
		return nil, "", fmt.Errorf("unable to locate PushStatus. Exec result: %s", result)
	}
	pushStatusEndIndex := strings.LastIndex(result, "}")
	if pushStatusEndIndex < 0 {
		return nil, "", fmt.Errorf("unable to locate PushStatus. Exec result: %s", result)
	}
	pushStatusJSON := result[pushStatusStartIndex : pushStatusEndIndex+1]

	// Parse the push status.
	pushStatus := &model.PushStatus{}
	if err := json.Unmarshal([]byte(pushStatusJSON), pushStatus); err != nil {
		return nil, "", err
	}

	return pushStatus, pushStatusJSON, nil
}

func getPilotPods() ([]string, error) {
	res, err := util.Shell("kubectl get pods -n %s --kubeconfig=%s --selector istio=pilot -o=jsonpath='{range .items[*]}{.metadata.name}{\" \"}'",
		tc.Kube.Namespace, tc.Kube.KubeConfig)
	if err != nil {
		return nil, err
	}
	return strings.Split(res, " "), nil
}
