package operator

import (
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"

	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/image"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

var (
	IOPTestDataPath = filepath.Join(env.IstioSrc, "operator/cmd/mesh/testdata/manifest-generate/input")
)

func TestController(t *testing.T) {
	framework.
		NewTest(t).
		RequiresEnvironment(environment.Kube).
		Run(func(ctx framework.TestContext) {
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})
			workDir, err := ctx.CreateTmpDirectory("operator-controller-test")
			if err != nil {
				t.Fatal("failed to create test directory")
			}
			compareInClusterWithGenerated(t, ctx, istioCtl, workDir, path.Join(IOPTestDataPath, "all_on.yaml"))
			//TODO: add more input file to test
		})
}

func compareInClusterWithGenerated(t *testing.T, ctx framework.TestContext, istioCtl istioctl.Instance, workDir string, iopFile string) {
	generateCmd := []string{
		"manifest", "generate",
		"-f", iopFile,
	}
	out := istioCtl.InvokeOrFail(t, generateCmd)

	s, err := image.SettingsFromCommandLine()
	if err != nil {
		t.Fatal(err)
	}
	iop, err := ioutil.ReadFile(iopFile)
	if err != nil {
		t.Fatalf("failed to read iop file: %v", err)
	}
	metadataYAML := `
metadata:
  name: test-istiocontrolplane
  namespace: istio-system
`
	iopcr, err := util.OverlayYAML(string(iop), metadataYAML)
	if err != nil {
		t.Fatalf("failed to overlay iop with metadata: %v", err)
	}
	iopCRFile := filepath.Join(workDir, "iop_cr.yaml")
	if err := ioutil.WriteFile(iopCRFile, []byte(iopcr), os.ModePerm); err != nil {
		t.Fatalf("failed to write iop cr file: %v", err)
	}
	initCmd := []string{
		"operator", "init",
		"--wait",
		"-f", iopCRFile,
		"--hub=" + s.Hub,
		"--tag=" + s.Tag,
	}
	istioCtl.InvokeOrFail(t, initCmd)

	k8sObjects, err := object.ParseK8sObjectsFromYAMLManifest(out)
	if err != nil {
		t.Errorf("failed to parse manifest generate output: %v", err)
	}

	cluster := ctx.Environment().(*kube.Environment).KubeClusters[0]
	for _, k8sObject := range k8sObjects {
		gnYAML, err := k8sObject.YAML()
		t.Logf("checking object of name: %s, kind: %s. ns: %s", k8sObject.Name, k8sObject.Kind, k8sObject.Namespace)
		if err != nil {
			t.Errorf("failed to get yaml for object: %v", err)
		}
		gnSpecYaml, err := tpath.GetSpecSubtree(string(gnYAML))
		if err != nil {
			t.Fatalf("failed to get spec from object yaml: %v", err)
		}
		name, ns := k8sObject.Name, k8sObject.Namespace
		switch k8sObject.Kind {
		case "Service":
			svc, err := cluster.GetService(ns, name)
			if err != nil {
				t.Fatalf("failed to get service: %v", err)
			}
			icYAML, err := yaml.Marshal(svc.Spec)
			if err != nil {
				t.Fatalf("failed to marshal service spec: %v", err)
			}
			if !util.IsYAMLEqual(gnSpecYaml, string(icYAML)) {
				t.Errorf("incluster Service not equal to generated Service. diff: %v",
					util.YAMLDiff(gnSpecYaml, string(icYAML)))
			}
		case "Deployment":
			dp, err := cluster.GetDeployment(ns, name)
			if err != nil {
				t.Fatalf("failed to get deployment: %v", err)
			}
			icYAML, err := yaml.Marshal(dp.Spec)
			if err != nil {
				t.Fatalf("failed to marshal service spec: %v", err)
			}
			if !util.IsYAMLEqual(gnSpecYaml, string(icYAML)) {
				t.Errorf("incluster Service not equal to generated Service. diff: %v",
					util.YAMLDiff(gnSpecYaml, string(icYAML)))
			}
		case "ConfigMap":
			dataYaml, err := tpath.GetConfigSubtree(string(gnYAML), "data")
			if err != nil {
				t.Fatalf("failed to get data from configmap: %v", err)
			}
			cm, err := cluster.GetConfigMap(name, ns)
			if err != nil {
				t.Fatalf("failed to get configMap of name: %s, ns: %s", name, ns)
			}
			icYAML, err := yaml.Marshal(cm.Data)
			if err != nil {
				t.Fatalf("failed to marshal configMap")
			}
			if !util.IsYAMLEqual(dataYaml, string(icYAML)) {
				t.Errorf("incluster ConfigMap not equal to generated ConfigMap, diff: %s",
					util.YAMLDiff(dataYaml, string(icYAML)))
			}
		}
	}
}
