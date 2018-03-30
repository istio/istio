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
	"bytes"
	"testing"
	"text/template"

	"io/ioutil"
	"os"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	k8s_cr "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"

	"github.com/pborman/uuid"
)

// type createCfgDataFilesFunc func(dir string, cData []clusterInfo) (err error)

type env struct {
	fsRoot string
}

var tmpl *template.Template

func init() {
	tmpl = template.Must(template.ParseFiles("clusterregistry.gotmpl", "clusterconfig.gotmpl"))
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

type clusterInfo struct {
	Kind                  string
	Name                  string
	PilotIP               string
	Platform              string
	AccessConfigSecret    string
	AccessConfigNamespace string
	PilotCfgStore         bool
	ServerEndpointIP      string
	ClientCidr            string
}

type clusterConfig struct {
	ClusterName              string
	ClusterIP                string
	CertificateAuthorityData string
	ClusterUserName          string
	ClientCertificateData    string
	ClientKeyData            string
}

func TestGetPilotAccessConfig(t *testing.T) {

	pilot := k8s_cr.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fakePilot",
		},
	}
	tests := []struct {
		testName    string
		cs          ClusterStore
		expectError bool
	}{
		{
			testName:    "No pilot in the store",
			cs:          ClusterStore{},
			expectError: true,
		},
		{
			testName: "Pilot in the store",
			cs: ClusterStore{
				cfgStore:      &pilot,
				clientConfigs: map[string]clientcmdapi.Config{"fakePilot": {}},
			},
			expectError: false,
		},
	}
	for _, test := range tests {

		if test.cs.GetPilotAccessConfig() == nil && !test.expectError {
			t.Errorf("Test '%s' failed", test.testName)
			continue
		}
	}
}

func TestGetPilotClusters(t *testing.T) {

	pilot := k8s_cr.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "fakePilot",
			Annotations: map[string]string{ClusterPilotEndpoint: "192.168.1.1:9080"},
		},
	}
	tests := []struct {
		testName       string
		cs             ClusterStore
		numberOfPilots int
	}{
		{
			testName:       "No pilots in the store",
			cs:             ClusterStore{},
			numberOfPilots: 0,
		},
		{
			testName: "2 out of 3 Pilot in the store",
			cs: ClusterStore{
				cfgStore: &pilot,
				clusters: []*k8s_cr.Cluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "fakePilot2",
							Annotations: map[string]string{ClusterPilotEndpoint: "192.168.1.1:9080"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "fakePilot3",
							Annotations: map[string]string{ClusterPilotEndpoint: "192.168.1.1:9080"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "fakePilot4",
							Annotations: map[string]string{ClusterPilotEndpoint: "192.168.2.1:9080"},
						},
					},
				},
				clientConfigs: map[string]clientcmdapi.Config{"fakePilot": {}},
			},
			numberOfPilots: 2,
		},
		{
			testName: "3 out of 3 Pilot in the store",
			cs: ClusterStore{
				cfgStore: &pilot,
				clusters: []*k8s_cr.Cluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "fakePilot2",
							Annotations: map[string]string{ClusterPilotEndpoint: "192.168.1.1:9080"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "fakePilot3",
							Annotations: map[string]string{ClusterPilotEndpoint: "192.168.1.1:9080"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "fakePilot4",
							Annotations: map[string]string{ClusterPilotEndpoint: "192.168.1.1:9080"},
						},
					},
				},
			},
			numberOfPilots: 3,
		},
	}
	for _, test := range tests {

		numberOfPilots := len(test.cs.GetPilotClusters())
		if numberOfPilots != test.numberOfPilots {
			t.Errorf("Test '%s' failed, expected: %d number of Pilots, got: %d ", test.testName,
				test.numberOfPilots, numberOfPilots)
			continue
		}
	}
}

func TestGetClusterConfig(t *testing.T) {
	tests := []struct {
		testName      string
		configMapName string
		ci            []clusterInfo
		cc            []clusterConfig
		expectError   bool
	}{
		{
			testName:      "Single node all good",
			configMapName: "clusterregistry" + "-" + uuid.New(),
			ci: []clusterInfo{
				{
					Kind:                  "Cluster",
					Name:                  "clusA",
					PilotIP:               "2.2.2.2",
					AccessConfigSecret:    "clusA",
					AccessConfigNamespace: "istio-system",
					PilotCfgStore:         true,
					ServerEndpointIP:      "192.168.4.10",
					ClientCidr:            "0.0.0.1/0",
				},
			},
			cc: []clusterConfig{
				{
					ClusterName:              "clusA",
					ClusterIP:                "192.168.4.10",
					CertificateAuthorityData: "blahblah",
					ClusterUserName:          "admin",
					ClientCertificateData:    "blahblah",
					ClientKeyData:            "blahblah",
				},
			},
			expectError: false,
		},
		{
			testName:      "Invalid kind",
			configMapName: "clusterregistry" + "-" + uuid.New(),
			ci: []clusterInfo{
				{
					Kind:                  "Invalid",
					Name:                  "clusA",
					PilotIP:               "2.2.2.2",
					AccessConfigSecret:    "clusA",
					AccessConfigNamespace: "istio-system",
					PilotCfgStore:         true,
					ServerEndpointIP:      "192.168.4.10",
					ClientCidr:            "0.0.0.1/0",
				},
			},
			cc:          []clusterConfig{},
			expectError: true,
		},
	}
	e := env{}
	err := e.setup()
	if err != nil {
		t.Error(err)
	}
	defer e.teardown()

	client := fake.NewSimpleClientset()

	for _, test := range tests {
		if err := buildConfigMap(client, test.ci, test.configMapName); err != nil {
			t.Errorf("Failed to build configmap(s) with error: %v", err)
		}
		if err := buildSecret(client, test.cc); err != nil {
			t.Errorf("Failed to build secret(s) with error: %v", err)
		}
		cs, err := getClustersConfigs(client, test.configMapName, "istio-system")
		if err != nil && !test.expectError {
			t.Errorf("Test '%s' failed, expected not to fail, but failed with error: %v", test.testName, err)
			continue
		}
		if err == nil && test.expectError {
			t.Errorf("Test '%s' failed, expected to fail, but did not", test.testName)
			continue
		}
		if err == nil {
			if cs == nil {
				t.Errorf("Test '%s' failed, the number of retrieved cluster config cannot be 0", test.testName)
				continue
			}
		}

	}

}

func buildConfigMap(k8s *fake.Clientset, ci []clusterInfo, configMapName string) error {
	configmap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: "istio-system",
		},
	}

	data := map[string]string{}
	for _, c := range ci {
		parsedConfig := new(bytes.Buffer)

		if err := tmpl.ExecuteTemplate(parsedConfig, "clusterregistry.gotmpl", c); err != nil {
			return err
		}
		data[c.Name+".yaml"] = parsedConfig.String()
	}
	configmap.Data = data

	_, err := k8s.CoreV1().ConfigMaps("istio-system").Create(configmap)

	return err
}

func buildSecret(k8s *fake.Clientset, cc []clusterConfig) error {

	for _, c := range cc {
		data := map[string][]byte{}
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      c.ClusterName,
				Namespace: "istio-system",
			},
		}

		parsedConfig := new(bytes.Buffer)
		if err := tmpl.ExecuteTemplate(parsedConfig, "clusterconfig.gotmpl", c); err != nil {
			return err
		}
		data[c.ClusterName+".yaml"] = parsedConfig.Bytes()
		secret.Data = data
		_, err := k8s.CoreV1().Secrets("istio-system").Create(secret)
		if err != nil {
			return err
		}

	}

	return nil
}
