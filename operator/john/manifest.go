package john

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"istio.io/istio/operator/pkg/apis/istio"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/operator/pkg/helm"
	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/translate"
	"istio.io/istio/operator/pkg/validate"
	pkgversion "istio.io/istio/pkg/version"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/yaml"
	yaml2 "sigs.k8s.io/yaml"

	"istio.io/istio/manifests"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/ptr"
)

func GenerateManifest(inFilename []string, setFlags []string, force bool, filter []string, client kube.Client, l clog.Logger) error {
	// we should start with the base default.yaml?
	merged, err := ReadLayeredYAMLs2(inFilename)
	if err != nil {
		return err
	}
	withSet, err := overlaySetFlagValues2(merged, setFlags)
	// We do not do NewReverseTranslator. do we really need it???
	//t := translate.NewReverseTranslator()
	//overlayYAML, err := t.TranslateK8SfromValueToIOP(string(b))
	//l.Print(overlayYAML)
	b, _ := json.MarshalIndent(withSet, "", " ")
	l.Print(string(b))
	iop, err := unmarshalAndValidateIOP(string(b), force, false, l)
	_ = iop


	t := translate.NewTranslator()
	//t.TranslateHelmValues(iop, )
	//apv, _ := t.ProtoToValues(iop.Spec)
	// TODO setComponentProperties?
	//l.Print(apv)
	c := name.PilotComponentName
	hsd := t.ComponentMap(string(c)).HelmSubdir
	renderer := helm.NewHelmRenderer(iop.Spec.InstallPackagePath, hsd, string(c), "istio-system", nil)
	renderer.Run()
	done, err := renderer.RenderManifest(string(b))
	l.Print(fmt.Sprintf("%v", err))
	l.Print(done)
	// We are skipping TranslateHelmValues
	// TODO: istioNamespace -> IOP.namespace
	// TODO: set components based on profile
	// TODO: ValuesEnablementPathMap? This enables the ingress or egress
	return nil
}

func addHubAndTag2() ([]string) {
	hub := pkgversion.DockerInfo.Hub
	tag := pkgversion.DockerInfo.Tag
	if hub != "unknown" && tag != "unknown" {
		return []string{"hub="+hub, "tag="+tag}
	}
	return nil
}


func ReadLayeredYAMLs2(filenames []string) ([]byte, error) {
	var stdin bool
	fs := manifests.BuiltinOrDir("")
	f, err := fs.Open("profiles/default.yaml")
	if err != nil {
		return nil, err
	}
	base, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	values, err := yaml.YAMLToJSON(base)
	if err != nil {
		return nil, err
	}

	// TODO: add hub and tag prelay
	// TODO: add cluster specific settings prelay (GKE version)

	for _, fn := range filenames {
		var b []byte
		var err error
		if fn == "-" {
			if stdin {
				continue
			}
			stdin = true
			b, err = io.ReadAll(os.Stdin)
		} else {
			b, err = os.ReadFile(strings.TrimSpace(fn))
		}
		if err != nil {
			return nil, err
		}
		//multiple := false
		//multiple, err = hasMultipleIOPs(string(b))
		//if err != nil {
		//	return nil, err
		//}
		//if multiple {
		//	return nil, fmt.Errorf("input file %s contains multiple IstioOperator CRs, only one per file is supported", fn)
		//}
		values, err = OverlayIOP2(values, b)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

// OverlayIOP overlays over base using JSON strategic merge.
func OverlayIOP2(base, overlay []byte) ([]byte, error) {
	if len(bytes.TrimSpace(base)) == 0 {
		return overlay, nil
	}
	if len(bytes.TrimSpace(overlay)) == 0 {
		return base, nil
	}
	// Incoming may be yaml
	oj, err := yaml2.YAMLToJSON(overlay)
	if err != nil {
		return nil, err
	}

	return strategicpatch.StrategicMergePatch(base, oj, &util.IopMergeStruct)
}

func FromYaml[T any](overlay []byte) (T, error) {
	v := new(T)
	err := yaml.Unmarshal(overlay, &v)
	if err != nil {
		return ptr.Empty[T](), err
	}
	return *v, nil
}

func FromJson[T any](overlay []byte) (T, error) {
	v := new(T)
	err := json.Unmarshal(overlay, &v)
	if err != nil {
		return ptr.Empty[T](), err
	}
	return *v, nil
}

// overlaySetFlagValues overlays each of the setFlags on top of the passed in IOP YAML string.
func overlaySetFlagValues2(base []byte, setFlags []string) (map[string]any, error) {
	iop, err := FromJson[map[string]any](base)
	if err != nil {
		return nil, err
	}

	for _, sf := range setFlags {
		p, v, err := getPV2(sf)
		if err != nil {
			return nil, err
		}
		p = strings.TrimPrefix(p, "spec.")
		inc, _, err := tpath.GetPathContext(iop, util.PathFromString("spec."+p), true)
		if err != nil {
			return nil, err
		}
		// input value type is always string, transform it to correct type before setting.
		var val any = v
		if !isAlwaysString(p) {
			val = util.ParseValue(v)
		}
		if err := tpath.WritePathContext(inc, val, false); err != nil {
			return nil, err
		}
	}

	return iop, nil
}
func unmarshalAndValidateIOP(iopsYAML string, force, allowUnknownField bool, l clog.Logger) (*v1alpha1.IstioOperator, error) {
	iop, err := istio.UnmarshalIstioOperator(iopsYAML, allowUnknownField)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal merged YAML: %s\n\nYAML:\n%s", err, iopsYAML)
	}
	if errs := validate.CheckIstioOperatorSpec(iop.Spec); len(errs) != 0 && !force {
		l.LogAndError("Run the command with the --force flag if you want to ignore the validation error and proceed.")
		return iop, fmt.Errorf(errs.Error())
	}
	return iop, nil
}