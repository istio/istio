package apps

import (
	"fmt"
	"io/ioutil"
	"testing"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/framework/components/deployment"
)

func TestNewDeploymentByAppParams(t *testing.T) {
	deployment.InitializeSettingsForUnitTest()
	for _, tc := range []struct {
		param AppParam
	}{
		{
			param: AppParam{Name: "basicapp"},
		},
		{
			param: AppParam{
				Name:     "localityapp",
				Locality: "us-central1-a",
			},
		},
		{
			param: AppParam{
				Name: "annotatedapp",
				PodAnnotations: map[string]string{
					"sidecar.istio.io/inject": "false",
				},
				ServiceAnnotations: map[string]string{
					"alpha.istio.io/kubernetes-serviceaccounts": "spiffe://cluster.local/ns/ns-a/sa/sa-b",
				},
			},
		},
	} {
		d := newDeploymentByAppParm(tc.param)
		name := tc.param.Name
		got, err := d.renderTemplate()
		if err != nil {
			t.Errorf("[%v] deployment creation fail, error %v", name, err)
		}
		wantFilePath := fmt.Sprintf("testdata/%v.yaml", name)
		golden, err := ioutil.ReadFile(wantFilePath)
		gotBytes := []byte(got)
		wantBytes := []byte(golden)
		if err != nil {
			t.Errorf("[%v] gold file not found", name)
		}
		if util.Refresh() {
			util.RefreshGoldenFile(gotBytes, wantFilePath, t)
		}
		util.CompareBytes(gotBytes, wantBytes, wantFilePath, t)
	}
}
