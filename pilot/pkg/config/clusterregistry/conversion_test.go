// Copyright 2017 Istio Authors
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

package clusterregistry

import (
	"testing"
	//"github.com/ghodss/yaml"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"text/template"

	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"istio.io/istio/pilot/pkg/serviceregistry"
)

type createCfgDataFilesFunc func(dir string, cData []clusterData) (err error)

type env struct {
	fsRoot string
}

func (e *env) setup() error {
	e.fsRoot = createTempDir()
	return nil
}

func (e *env) teardown() {
	// Remove the temp dir.
	os.RemoveAll(e.fsRoot) // nolint: errcheck
}

func createTempDir() string {
	// Make the temporary directory
	dir, _ := ioutil.TempDir("/tmp/", "clusterregistry")
	_ = os.MkdirAll(dir, os.ModeDir|os.ModePerm)
	return dir
}

func createAccessCfgFiles(dir string, cData []clusterData) (err error) {
	for _, cD := range cData {
		if _, err = os.OpenFile(dir+"/"+cD.AccessConfig, os.O_RDONLY|os.O_CREATE, 0666); err != nil {
			return err
		}
	}
	return
}

var clusterTempl = `
---

apiVersion: clusterregistry.k8s.io/v1alpha1
kind: Cluster
metadata:
  name: {{.Name}}
  annotations:
    config.istio.io/pilotEndpoint: "{{.PilotIP}}:9080"
    config.istio.io/platform: {{if .Platform}}{{.Platform}}{{else}}"Kubernetes"{{end}}
    {{if .PilotCfgStore}}config.istio.io/pilotCfgStore: {{.PilotCfgStore}}
    {{end -}}
    config.istio.io/accessConfigFile: {{.AccessConfig}}
spec:
  kubernetesApiEndpoints:
    serverEndpoints:
      - clientCIDR: "{{.ClientCidr}}"
        serverAddress: "{{.ServerEndpointIP}}"
`

type clusterData struct {
	Name             string
	PilotIP          string
	Platform         string
	AccessConfig     string
	PilotCfgStore    bool
	ServerEndpointIP string
	ClientCidr       string
}

func compareParsedCluster(inData clusterData, cluster *k8s_cr.Cluster) error {
	if inData.AccessConfig != GetClusterAccessConfig(cluster) {
		return fmt.Errorf("AccessConfig mismatch for parsed cluster '%s'--"+
			"in:'%s' v. out:'%s'",
			GetClusterName(cluster), inData.AccessConfig, GetClusterAccessConfig(cluster))
	}
	inPilotEp := fmt.Sprintf("%s:9080", inData.PilotIP)
	if inPilotEp != cluster.ObjectMeta.Annotations[ClusterPilotEndpoint] {
		return fmt.Errorf("PilotEndpoint mismatch for parsed cluster '%s'--"+
			"in:'%s' v. out:'%s'",
			GetClusterName(cluster), inPilotEp, cluster.ObjectMeta.Annotations[ClusterPilotEndpoint])
	}
	cPilotCfgStore := false
	if cluster.ObjectMeta.Annotations[ClusterPilotCfgStore] != "" {
		var err error
		cPilotCfgStore, err = strconv.ParseBool(cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
		if err != nil {
			return fmt.Errorf("conversion error for pilotCfgStore in parsed cluster '%s'-- value: '%s'",
				cluster.Name,
				cluster.ObjectMeta.Annotations[ClusterPilotCfgStore])
		}
	}
	if inData.PilotCfgStore != cPilotCfgStore {
		return fmt.Errorf("pilotCfgStore mismatch for parsed cluster '%s'--"+
			"in:'%t' v. out:'%t'",
			GetClusterName(cluster), inData.PilotCfgStore,
			cPilotCfgStore)
	}
	plat := string(serviceregistry.KubernetesRegistry) // test template sets as default platform
	if inData.Platform != "" {
		plat = inData.Platform
	}
	if cluster.ObjectMeta.Annotations[ClusterPlatform] != plat {
		return fmt.Errorf("platform type mismatch for parsed cluster '%s'--"+
			"in:'%s' v. out:'%s'",
			GetClusterName(cluster), plat, cluster.ObjectMeta.Annotations[ClusterPlatform])
	}

	for _, kubeAPIEp := range cluster.Spec.KubernetesAPIEndpoints.ServerEndpoints {
		if inData.ServerEndpointIP != kubeAPIEp.ServerAddress {
			return fmt.Errorf("kubernetes API endpoint serverIP mismatch for parsed cluster '%s'--"+
				"in:'%s' v. out:'%s'",
				GetClusterName(cluster), inData.ServerEndpointIP, kubeAPIEp.ServerAddress)
		}
		if inData.ClientCidr != kubeAPIEp.ClientCIDR {
			return fmt.Errorf("kubernetes API endpoint ClientCIDR mismatch for parsed cluster '%s'--"+
				"in:'%s' v. out:'%s'",
				GetClusterName(cluster), inData.ClientCidr, kubeAPIEp.ClientCIDR)
		}
	}
	return nil
}

func checkClusterDataInInput(inDataList []clusterData, cluster *k8s_cr.Cluster) error {
	// Check that the cluster is matching with data from the test input data
	found := false
	for _, inData := range inDataList {
		if inData.Name == GetClusterName(cluster) {
			found = true
			return compareParsedCluster(inData, cluster)
		}
	}
	if !found {
		return fmt.Errorf("cluster named '%s' not found in test input data",
			GetClusterName(cluster))
	}
	return nil
}

func checkInputInClusterData(inData clusterData, clusters []*k8s_cr.Cluster) error {
	// Check that the input data is in the clusters list
	found := false
	for _, cluster := range clusters {
		if inData.Name == GetClusterName(cluster) {
			found = true
			return compareParsedCluster(inData, cluster)
		}
	}
	if !found {
		return fmt.Errorf("test input for cluster named '%s' not found in parsed result",
			inData.Name)
	}
	return nil
}

func checkClusterData(t *testing.T, inDataList []clusterData, clusters []*k8s_cr.Cluster) (err error) {
	// check each cluster is in the input data
	for _, cluster := range clusters {
		t.Logf("Parser built cluster: \"%s\", AccessConfig: \"%s\"",
			GetClusterName(cluster),
			GetClusterAccessConfig(cluster))
		err = checkClusterDataInInput(inDataList, cluster)
		if err != nil {
			return err
		}
	}

	// check the input data is in each cluster
	for _, testData := range inDataList {
		err = checkInputInClusterData(testData, clusters)
		if err != nil {
			return err
		}
	}
	return nil
}

func checkClusterStore(inDataList []clusterData, cs ClusterStore) (err error) {
	for _, cData := range inDataList {
		if cData.PilotCfgStore {
			pilotKubeConf := cs.GetPilotAccessConfig()
			if cData.AccessConfig != pilotKubeConf {
				return fmt.Errorf("mismatch PilotCfgStore AccessConfig in ClusterStore--"+
					"in: '%t' ; clusterStore '%s'", cData.PilotCfgStore, pilotKubeConf)
			}
			clusters := cs.GetPilotClusters()
			if len(clusters) == 0 {
				return fmt.Errorf("no pilot cluster found, expected '%s'", cData.Name)
			}
			for _, cluster := range clusters {
				if cluster.Name == cData.Name ||
					cluster.ObjectMeta.Annotations[ClusterPilotEndpoint] !=
						fmt.Sprintf("%s:9080", cData.PilotIP) {
					return fmt.Errorf("mismatch PilotCfgStore cluster in ClusterStore--"+
						"in: '%s' ; clusterStore '%s'", cData.Name, clusters[0].Name)
				}
			}
			break
		}
	}
	return nil
}

/* Create several files under a temp directory with clusterData */
func createFilePerCluster(dir string, cData []clusterData) (err error) {
	tmpl, err := template.New("test").Parse(clusterTempl)
	if err != nil {
		return err
	}
	for _, cD := range cData {
		f, fileErr := os.Create(fmt.Sprintf("%s/%s.yaml", dir, cD.Name))
		if fileErr != nil {
			return fileErr
		}
		err = tmpl.Execute(f, cD)
		if err != nil {
			return err
		}
	}
	return nil
}

/* Create single files under a temp directory with clusterData */
func createFileForAllClusters(dir string, cData []clusterData) (err error) {
	tmpl, err := template.New("test").Parse(clusterTempl)
	if err != nil {
		return err
	}
	f, fileErr := os.Create(fmt.Sprintf("%s/allClusters.yaml", dir))
	if fileErr != nil {
		return fileErr
	}
	for _, cD := range cData {
		err = tmpl.Execute(f, cD)
		if err != nil {
			return err
		}
	}
	return nil
}

func testClusterReadDir(t *testing.T, crFunc createCfgDataFilesFunc,
	dir string, cData []clusterData) {
	_ = os.MkdirAll(dir, os.ModeDir|os.ModePerm)
	defer os.RemoveAll(dir)
	if err := createAccessCfgFiles(dir, cData); err != nil {
		t.Fatal(err)
	}

	if err := crFunc(dir, cData); err != nil {
		t.Error(err)
	}
	cs, err := ReadClusters(dir)
	if err != nil {
		t.Error(err)
	}
	clusters := append(cs.clusters, cs.cfgStore)
	err = checkClusterData(t, cData, clusters)
	if err != nil {
		t.Error(err)
	}

	if err = checkClusterStore(cData, *cs); err != nil {
		t.Error(err)
	}
}

func TestReadClusters(t *testing.T) {
	cData := []clusterData{
		{
			Name:             "clusA",
			PilotIP:          "2.2.2.2",
			AccessConfig:     "A_kubeconfig",
			PilotCfgStore:    true,
			ServerEndpointIP: "192.168.4.10",
			ClientCidr:       "0.0.0.1/0",
		},
		{
			Name:             "clusB",
			PilotIP:          "2.2.2.2",
			AccessConfig:     "B_kubeconfig",
			PilotCfgStore:    false,
			ServerEndpointIP: "192.168.5.10",
			ClientCidr:       "0.0.0.0/0",
		},
		{
			Name:             "clusC",
			PilotIP:          "4.4.4.4",
			AccessConfig:     "C_kubeconfig",
			PilotCfgStore:    false,
			ServerEndpointIP: "192.168.6.10",
			ClientCidr:       "0.0.0.0/0",
		},
		{
			Name:             "clusD",
			PilotIP:          "5.5.5.5",
			AccessConfig:     "D_kubeconfig",
			PilotCfgStore:    false,
			ServerEndpointIP: "192.168.7.10",
			ClientCidr:       "0.0.0.0/0",
		},
	}
	e := env{}
	err := e.setup()
	if err != nil {
		t.Error(err)
	}
	defer e.teardown()

	testClusterReadDir(t, createFilePerCluster, e.fsRoot+"/multi", cData)
	testClusterReadDir(t, createFileForAllClusters, e.fsRoot+"/single", cData)
}

func TestReadClusters_nodir(t *testing.T) {
	_, err := ReadClusters("/bogusdir")
	if err == nil {
		t.Errorf("lack of error for invalid cluster config dir")
	} else {
		t.Logf("error found for invalid dir: %v", err)
	}
}

func TestParseClusters_templ(t *testing.T) {
	clusData := []clusterData{
		{
			Name:             "clusA",
			PilotIP:          "2.2.2.2",
			AccessConfig:     "A_kubeconfig",
			PilotCfgStore:    true,
			ServerEndpointIP: "192.168.4.10",
			ClientCidr:       "0.0.0.1/0",
		},
		{
			Name:             "clusB",
			PilotIP:          "3.3.3.3",
			AccessConfig:     "B_kubeconfig",
			PilotCfgStore:    false,
			ServerEndpointIP: "192.168.5.10",
			ClientCidr:       "0.0.0.0/0",
		},
		{
			Name:             "clusC",
			PilotIP:          "4.4.4.4",
			AccessConfig:     "C_kubeconfig",
			PilotCfgStore:    false,
			ServerEndpointIP: "192.168.6.10",
			ClientCidr:       "0.0.0.0/0",
		},
		{
			Name:             "clusD",
			PilotIP:          "5.5.5.5",
			AccessConfig:     "D_AccessConfig",
			PilotCfgStore:    false,
			ServerEndpointIP: "192.168.7.10",
			ClientCidr:       "0.0.0.0/0",
		},
	}
	e := env{}
	err := e.setup()
	if err != nil {
		t.Error(err)
	}
	defer e.teardown()
	if err = createAccessCfgFiles(e.fsRoot, clusData); err != nil {
		t.Fatal(err)
	}

	tmpl, err := template.New("test").Parse(clusterTempl)
	if err != nil {
		t.Error(err)
	}
	testDataBuf := new(bytes.Buffer)
	for _, cD := range clusData {
		err = tmpl.Execute(testDataBuf, cD)
		if err != nil {
			t.Error(err)
		}
	}

	t.Logf("YAML to convert:\n%s\n", testDataBuf.String())

	clusters, err := parseClusters(e.fsRoot, testDataBuf.Bytes())
	if err != nil {
		t.Error(err)
	}
	err = checkClusterData(t, clusData, clusters)
	if err != nil {
		t.Error(err)
	}

}

func TestParseClusters_fail(t *testing.T) {
	clusData := []clusterData{
		{
			Name:             "clusA",
			PilotIP:          "2.2.2.2",
			AccessConfig:     "A_kubeconfig",
			PilotCfgStore:    true,
			ServerEndpointIP: "192.168.4.10",
			ClientCidr:       "0.0.0.1/0",
		},
	}
	e := env{}
	err := e.setup()
	if err != nil {
		t.Error(err)
	}
	defer e.teardown()
	if err := createAccessCfgFiles(e.fsRoot, clusData); err != nil {
		t.Fatal(err)
	}

	// Map of bad cluster registry input templates to test validity checks in parseClusters
	testData := map[string]string{
		"BadKind": `---

apiVersion: clusterregistry.k8s.io/v1alpha1
kind: InvalidKind
metadata:
  name: {{.Name}}
  annotations:
    config.istio.io/pilotEndpoint: "{{.PilotIP}}:9080"
    config.istio.io/platform: {{if .Platform}}{{.Platform}}{{else}}"Kubernetes"{{end}}
    {{if .PilotCfgStore}}config.istio.io/pilotCfgStore: {{.PilotCfgStore}}
    {{end -}}
    config.istio.io/accessConfigFile: {{.AccessConfig}}
spec:
  kubernetesApiEndpoints:
    serverEndpoints:
      - clientCIDR: "{{.ClientCidr}}"
        serverAddress: "{{.ServerEndpointIP}}"
`,
		/*		"NoName":
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
				`,*/
	}
	for testType, testItem := range testData {
		tmpl, err := template.New("test").Parse(testItem)
		if err != nil {
			t.Error(err)
		}
		testDataBuf := new(bytes.Buffer)
		err = tmpl.Execute(testDataBuf, clusData[0])
		if err != nil {
			t.Error(err)
		}
		clusters, err := parseClusters(e.fsRoot, testDataBuf.Bytes())
		if err == nil {
			t.Error(fmt.Errorf("bad input test '%s' not getting error code", testType))
			if len(clusters) > 0 {
				t.Errorf("cluster data was instantiated during bad input test '%s'", testType)
			}
		}
	}
}
