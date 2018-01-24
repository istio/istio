/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clusterregistry

import (
	"testing"
	//"github.com/ghodss/yaml"
	"text/template"
	"bytes"
	"fmt"
	"strconv"
)

var cluster_templ = `
---

apiVersion: clusterregistry.k8s.io/v1alpha1
kind: Cluster
metadata:
  name: {{.Name}}
  annotations:
    config.istio.io/pilotEndpoint: "{{.PilotIP}}:9080"
    config.istio.io/platform: "k8s"
    {{if .PilotCfgStore}}config.istio.io/pilotCfgStore: {{.PilotCfgStore}}
    {{end -}}
    config.istio.io/accessConfigFile: {{.Kubeconfig}}
spec:
  kubernetesApiEndpoints:
    serverEndpoints:
      - clientCIDR: "{{.ClientCidr}}"
        serverAddress: "{{.ServerEndpointIP}}"
`

type clusterData struct {
	Name string
	PilotIP string
	Kubeconfig string
	PilotCfgStore bool
	ServerEndpointIP string
	ClientCidr string
}

func TestReadClusters_file(t *testing.T) {
	cs, err := ReadClusters("testdata/")
	if err != nil {
		t.Error(err)
	}
	for _, cluster := range cs.clusters {
		t.Logf("found cluster: \"%s\", kubeconfig: \"%s\"",
				GetClusterName(cluster),
				GetClusterKubeConfig(cluster))
	}
}

func compareParsedCluster(inData clusterData, cluster *Cluster) (error) {
	if inData.Kubeconfig != GetClusterKubeConfig(cluster) {
		return fmt.Errorf("kubeconfig mismatch for parsed cluster '%s'--" +
			"in:'%s' v. out:'%s'",
			GetClusterName(cluster), inData.Kubeconfig, GetClusterKubeConfig(cluster))
	}
	inPilotEp := fmt.Sprintf("%s:9080", inData.PilotIP)
	if  inPilotEp != cluster.ObjectMeta.Annotations[ClusterPilotEndpoint] {
		return fmt.Errorf("PilotEndpoint mismatch for parsed cluster '%s'--" +
			"in:'%s' v. out:'%s'",
			GetClusterName(cluster), inPilotEp, cluster.ObjectMeta.Annotations[ClusterPilotEndpoint])
	}
	cPilotCfgStore := false
	if cluster.ObjectMeta.Annotations[ClusterPilotCfgStore] != "" {
		var err error
		cPilotCfgStore, err = strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
		if err != nil {
			return fmt.Errorf("PilotCfgStore bad bool conversion for parsed cluster '%s'--'%s'",
				cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
		}
	}
	if  inData.PilotCfgStore != cPilotCfgStore {
		return fmt.Errorf("PilotCfgStore mismatch for parsed cluster '%s'--" +
			"in:'%t' v. out:'%t'",
			GetClusterName(cluster), inData.PilotCfgStore,
			cPilotCfgStore)
	}
	if cluster.ObjectMeta.Annotations[ClusterPlatform] != "k8s" {
		return fmt.Errorf("ClusterPlatform mismatch for parsed cluster '%s'--" +
			"in:'%s' v. out:'%s'",
			GetClusterName(cluster), "k8s", cluster.ObjectMeta.Annotations[ClusterPlatform] )
	}

	for _, kubeApiEp := range cluster.Spec.KubernetesAPIEndpoints.ServerEndpoints {
		if inData.ServerEndpointIP != kubeApiEp.ServerAddress {
			return fmt.Errorf("Kubernetes API Endpoint ServerIP mismatch for parsed cluster '%s'--" +
				"in:'%s' v. out:'%s'",
				GetClusterName(cluster), inData.ServerEndpointIP, kubeApiEp.ServerAddress)
		}
		if inData.ClientCidr != kubeApiEp.ClientCIDR {
			return fmt.Errorf("Kubernetes API Endpoint ClientCIDR mismatch for parsed cluster '%s'--" +
				"in:'%s' v. out:'%s'",
				GetClusterName(cluster), inData.ClientCidr, kubeApiEp.ClientCIDR)
		}
	}
	return nil
}

func checkClusterDataInInput(inDataList *[]clusterData, cluster *Cluster) (error) {
	// Check that the cluster is matching with data from the test input data
	found := false
	for _, inData := range *inDataList {
		if inData.Name == GetClusterName(cluster) {
			found = true
			return compareParsedCluster(inData, cluster)
		}
	}
	if !found {
		return fmt.Errorf("Cluster named '%s' not found in test input data.",
						  GetClusterName(cluster))
	}
	return nil
}

func checkInputInClusterData(inData clusterData, clusters []*Cluster) (error) {
	// Check that the input data is in the clusters list
	found := false
	for _, cluster := range clusters {
		if inData.Name == GetClusterName(cluster) {
			found = true
			return compareParsedCluster(inData, cluster)
		}
	}
	if !found {
		return fmt.Errorf("Test input for cluster named '%s' not found in parsed result.",
			inData.Name)
	}
	return nil
}

func checkClusterData(t *testing.T, inDataList *[]clusterData, clusters []*Cluster) (err error) {
	// check each cluster is in the input data
	for _, cluster := range clusters {
		t.Logf("Parser built cluster: \"%s\", kubeconfig: \"%s\"",
			GetClusterName(cluster),
			GetClusterKubeConfig(cluster))
		err = checkClusterDataInInput(inDataList, cluster)
		if err != nil { return err }
	}

	// check the input data is in each cluster
	for _, testData := range *inDataList {
		err = checkInputInClusterData(testData, clusters)
		if err != nil { return err }
	}
	return nil
}

func TestParseClusters_templ(t *testing.T) {
	testdata_buf := new(bytes.Buffer)
	clus_data := []clusterData{
		clusterData {
			Name: "clusA",
			PilotIP: "2.2.2.2",
			Kubeconfig: "A_kubeconfig",
			PilotCfgStore: true,
			ServerEndpointIP: "192.168.4.10",
			ClientCidr: "0.0.0.1/0",
		},
		clusterData {
			Name: "clusB",
			PilotIP: "3.3.3.3",
			Kubeconfig: "B_kubeconfig",
			PilotCfgStore: false,
			ServerEndpointIP: "192.168.5.10",
			ClientCidr: "0.0.0.0/0",
		},
		clusterData {
			Name: "clusC",
			PilotIP: "4.4.4.4",
			Kubeconfig: "C_kubeconfig",
			PilotCfgStore: false,
			ServerEndpointIP: "192.168.6.10",
			ClientCidr: "0.0.0.0/0",
		},
		clusterData {
			Name: "clusD",
			PilotIP: "5.5.5.5",
			Kubeconfig: "D_kubeconfig",
			PilotCfgStore: false,
			ServerEndpointIP: "192.168.7.10",
			ClientCidr: "0.0.0.0/0",
		},
	}

	tmpl, err := template.New("test").Parse(cluster_templ)
	if err != nil { t.Error(err) }
	for _, c_d := range clus_data {
		err = tmpl.Execute(testdata_buf, c_d)
		if err != nil { t.Error(err) }
	}

	t.Logf("YAML to convert:\n%s\n", testdata_buf.String())

	clusters, err := parseClusters([]byte(testdata_buf.String()))
	if err != nil {
		t.Error(err)
	}
	err = checkClusterData(t, &clus_data, clusters)
	if err != nil { t.Error(err)}

}

func TestParseClusters_fail(t *testing.T) {
	testData := map[string]string{
		"BadKind":
		`---

apiVersion: clusterregistry.k8s.io/v1alpha1
kind: BlusterZ
metadata:
  name: clusterFoo
  annotations:
    config.istio.io/pilotEndpoint: "1.1.1.1:9080"
    config.istio.io/platform: "k8s"
    config.istio.io/pilotCfgStore: true
    config.istio.io/accessConfigFile: Foo-config
spec:
  kubernetesApiEndpoints:
    serverEndpoints:
      - clientCidr: "0.0.0.0/0"
        serverAddress: "192.168.1.1"
`,
		"NoName":
		`---

apiVersion: clusterregistry.k8s.io/v1alpha1
kind: Cluster
metadata:
  annotations:
    config.istio.io/pilotEndpoint: "1.1.1.1:9080"
    config.istio.io/platform: "k8s"
    config.istio.io/pilotCfgStore: true
    config.istio.io/accessConfigFile: c1-config
spec:
  kubernetesApiEndpoints:
    serverEndpoints:
      - clientCidr: "0.0.0.0/0"
        serverAddress: "192.168.1.1"
`,
	}
	for testType, testItem := range testData {
		clusters, err := parseClusters([]byte(testItem))
		if err == nil {
			t.Error(fmt.Errorf("Bad input test failed for test type '%s'", testType))
			if len(clusters) > 0 {
				t.Logf("Some cluster stuff got filled in for bad input test '%s'", testType)
			}
		}
	}
}