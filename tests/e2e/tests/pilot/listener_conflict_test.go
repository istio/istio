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
	"bytes"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"encoding/json"
	"regexp"

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
}

func (i *serviceInfo) fullName() string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", i.ServiceName, i.Namespace)
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
	for _, c := range []*serviceInfo{oldService, newService} {
		yaml := getConflictServiceYaml(c, t)
		kubeApplyConflictService(yaml, t)
		defer util.KubeDeleteContents("", yaml, tc.Kube.KubeConfig)
	}

	for cluster := range tc.Kube.Clusters {
		testName := oldService.ServiceName
		runRetriableTest(t, cluster, testName, 5, func() error {
			// Scrape the pilot logs and verify that the newer service is filtered in favor of the older service.
			logs := getPilotLogs(t)
			pushStatuses, err := getPushStatus(logs)
			if err != nil {
				return err
			}

			for _, pushStatus := range pushStatuses {
				conflict, ok := pushStatus.ProxyStatus[metricName]
				if !ok {
					continue
				}
				event, ok := conflict[listenerName]
				if !ok {
					continue
				}

				accepted, rejected, numServices, err := parseEventMessageFields(event.Message)
				if err != nil {
					return err
				}

				if accepted == newService.fullName() {
					// We should never accept the port from the newer service.
					return fmt.Errorf("expected to reject listener for %s, but it was accepted", accepted)
				}
				if accepted != oldService.fullName() {
					// Possibly a rejection for a different service? Just go to the next event.
					continue
				}

				// The old service was accepted. The rejection should be the new service.
				if rejected != newService.fullName() {
					return fmt.Errorf("expected to reject listener for %s, but rejected %s", newService.fullName(), rejected)
				}
				if numServices != "1" {
					return fmt.Errorf("expected 1 TCP services, but found %s", numServices)
				}

				return nil
			}

			return fmt.Errorf("failed to find ADS push status in pilot logs")
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

func getPushStatus(logs string) ([]*model.PushStatus, error) {
	lines := strings.Split(logs, "\n")
	statuses := make([]*model.PushStatus, 0)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.Contains(line, "ads\tPush finished") {
			// Gather the push status JSON.
			pushStatusJSON := "{\n"
			i++
			for ; i < len(lines); i++ {
				line = lines[i]
				if strings.HasPrefix(line, "}") {
					// Found the end of the json.
					pushStatusJSON += "}"

					// Parse the push status.
					pushStatus := &model.PushStatus{}
					if err := json.Unmarshal([]byte(pushStatusJSON), pushStatus); err != nil {
						return nil, err
					}
					statuses = append(statuses, pushStatus)
					break
				}
				// This line is part of the push status.
				pushStatusJSON += line
			}
		}
	}

	if len(statuses) == 0 {
		return nil, fmt.Errorf("no push status found in logs")
	}

	return statuses, nil
}

func getPilotLogs(t *testing.T) string {
	logs := ""
	pods := getPilotPods(t)
	for _, pod := range pods {
		logs += util.GetPodLogs(tc.Kube.Namespace, pod, "discovery", false, false, tc.Kube.KubeConfig)
	}
	return logs
}

func getPilotPods(t *testing.T) []string {
	res, err := util.Shell("kubectl get pods -n %s --kubeconfig=%s --selector istio=pilot -o=jsonpath='{range .items[*]}{.metadata.name}{\" \"}'",
		tc.Kube.Namespace, tc.Kube.KubeConfig)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(res, " ")
}

func kubeApplyConflictService(yaml string, t *testing.T) {
	t.Helper()
	if err := util.KubeApplyContents("", yaml, tc.Kube.KubeConfig); err != nil {
		t.Fatal(err)
	}
}

func getConflictServiceYaml(params interface{}, t *testing.T) string {
	t.Helper()
	var tmp bytes.Buffer
	err := template.Must(template.New("inject").Parse(conflictServiceTemplate)).Execute(&tmp, params)
	if err != nil {
		t.Fatal(err)
	}
	return tmp.String()
}
