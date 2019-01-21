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

	multierror "github.com/hashicorp/go-multierror"

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
		t:           t,
	}
	newService := &serviceInfo{
		ServiceName: fmt.Sprintf("service2"),
		Namespace:   fmt.Sprintf("%s-conflict-2", tc.Info.RunID),
		t:           t,
	}

	// Create the older of thw two services. It's "old" because it will have an older creationTimestamp.
	oldService.start()
	defer oldService.stop()

	// Sleep a couple of seconds before pushing the next service. This ensures the creationTimestamps are different.
	time.Sleep(2 * time.Second)

	// Now create the "new" service.
	newService.start()
	defer newService.stop()

	for range tc.Kube.Clusters {
		testName := oldService.ServiceName
		runRetriableTest(t, testName, 10, func() error {
			infos, err := getPilotInfos()
			if err != nil {
				return err
			}

			for _, info := range infos {
				pushStatus := info.pushStatus
				conflict, ok := pushStatus.ProxyStatus[metricName]
				if !ok {
					err = multierror.Prefix(err, fmt.Sprintf("unable to find push status metric %s", metricName))
					// See if another pod has the status we're looking for.
					continue
				}
				event, ok := conflict[listenerName]
				if !ok {
					err = multierror.Prefix(err, fmt.Sprintf("unable to find push status conflict %s", listenerName))
					// See if another pod has the status we're looking for.
					continue
				}

				accepted, rejected, numServices, e := parseEventMessageFields(event.Message)
				if e != nil {
					// See if another pod has the status we're looking for.
					err = multierror.Prefix(err, e.Error())
					continue
				}

				if accepted == newService.fullName() {
					// We should never accept the port from the newer service.
					err = multierror.Prefix(err, fmt.Sprintf("expected to reject listener for %s, but it was accepted %v", accepted, pushStatus))
					// No need to check others statuses - this shouldn't happen.
					break
				}
				if accepted != oldService.fullName() {
					// Possibly a rejection for a different service? Just go to the next event.
					err = multierror.Prefix(err, fmt.Sprintf("unexpected service accepted %s", metricName))
					continue
				}

				// The old service was accepted. The rejection should be the new service.
				if rejected != newService.fullName() {
					err = multierror.Prefix(err, fmt.Sprintf("expected to reject listener for %s, but rejected %s", newService.fullName(), rejected))
					// No need to check others statuses - this shouldn't happen.
					break
				}
				if numServices != "1" {
					err = multierror.Prefix(err, fmt.Sprintf("expected 1 TCP services, but found %s", numServices))
					// No need to check others statuses - this shouldn't happen.
					break
				}
			}

			if err != nil {
				return multierror.Prefix(err, fmt.Sprintf("pilot PushContext failed. Result=%s", infos.String()))
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

type pilotInfos []*pilotInfo

func (i pilotInfos) String() string {
	v := ""
	for _, info := range i {
		v += fmt.Sprintf("[%s]\n", info.String())
	}
	return v
}

type pilotInfo struct {
	pod            string
	pushStatusJSON string
	pushStatus     *model.PushContext
}

func (i *pilotInfo) String() string {
	return fmt.Sprintf("pilotPod: %s, pushStatus: %s", i.pod, i.pushStatusJSON)
}

func getPilotInfos() (pilotInfos, error) {
	pods, err := getPilotPods()
	if err != nil {
		return nil, err
	}

	statuses := make(pilotInfos, len(pods))
	for i, pod := range pods {
		var err error
		statuses[i], err = getPilotInfo(pod)
		if err != nil {
			return nil, err
		}
	}
	return statuses, nil
}

func getPilotInfo(pod string) (*pilotInfo, error) {
	command := "curl http://127.0.0.1:8080/debug/push_status"
	result, err := util.PodExec(tc.Kube.Namespace, pod, "discovery", command, true, tc.Kube.KubeConfig)
	if err != nil {
		return nil, err
	}

	pushStatusStartIndex := strings.Index(result, "{")
	if pushStatusStartIndex < 0 {
		return nil, fmt.Errorf("unable to locate PushContext. Exec result: %s", result)
	}
	pushStatusEndIndex := strings.LastIndex(result, "}")
	if pushStatusEndIndex < 0 {
		return nil, fmt.Errorf("unable to locate PushContext. Exec result: %s", result)
	}
	pushStatusJSON := result[pushStatusStartIndex : pushStatusEndIndex+1]

	// Parse the push status.
	pushStatus := &model.PushContext{}
	if err := json.Unmarshal([]byte(pushStatusJSON), pushStatus); err != nil {
		return nil, err
	}

	return &pilotInfo{
		pod:            pod,
		pushStatusJSON: pushStatusJSON,
		pushStatus:     pushStatus,
	}, nil
}

func getPilotPods() ([]string, error) {
	res, err := util.Shell("kubectl get pods -n %s --kubeconfig=%s --selector istio=pilot -o=jsonpath='{range .items[*]}{.metadata.name}{\" \"}'",
		tc.Kube.Namespace, tc.Kube.KubeConfig)
	if err != nil {
		return nil, err
	}
	pods := strings.Split(res, " ")
	// Trim off the last (empty) element.
	return pods[0 : len(pods)-1], nil
}
