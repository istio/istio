package render

import (
	"encoding/json"
	"fmt"

	yaml2 "gopkg.in/yaml.v2" // nolint: depguard // needed for weird tricks
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis"
	"istio.io/istio/operator/pkg/component"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/tpath"
	"istio.io/istio/operator/pkg/util"
	"istio.io/istio/operator/pkg/values"
)

func postProcess(comp component.Component, spec apis.GatewayComponentSpec, manifests []manifest.Manifest) ([]manifest.Manifest, error) {
	if spec.Kubernetes == nil {
		return manifests, nil
	}
	type Patch struct {
		Kind, Name string
		Patch      string
	}
	rn := comp.ResourceName
	if spec.Name != "" {
		// Gateways can override the name
		rn = spec.Name
	}
	if comp.UserFacingName == "pilot" {
		// TODO: if revision and istiod += -revision
	}
	rt := comp.ResourceType
	patches := map[string]Patch{
		"affinity":            {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"affinity":%s}}}}`},
		"env":                 {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "env": %%s}]}}}}`, comp.ContainerName)},
		"hpaSpec":             {Kind: "HorizontalPodAutoscaler", Name: rn, Patch: `{"spec":%s}`},
		"imagePullPolicy":     {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "imagePullPolicy": %%s}]}}}}`, comp.ContainerName)},
		"nodeSelector":        {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"nodeSelector":%s}}}}`},
		"podDisruptionBudget": {Kind: "PodDisruptionBudget", Name: rn, Patch: `{"spec":%s}`},
		"podAnnotations":      {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"metadata":{"annotations":%s}}}}`},
		"priorityClassName":   {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"priorityClassName":%s}}}}`},
		"readinessProbe":      {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "readinessProbe": %%s}]}}}}`, comp.ContainerName)},
		"replicaCount":        {Kind: rt, Name: rn, Patch: `{"spec":{"replicas":%s}}`},
		"resources":           {Kind: rt, Name: rn, Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "resources": %%s}]}}}}`, comp.ContainerName)},
		"strategy":            {Kind: rt, Name: rn, Patch: `{"spec":{"strategy":%s}}`},
		"tolerations":         {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"tolerations":%s}}}}`},
		"serviceAnnotations":  {Kind: "Service", Name: rn, Patch: `{"metadata":{"annotations":%s}}`},
		"service":             {Kind: "Service", Name: rn, Patch: `{"spec":%s}`},
		"securityContext":     {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"securityContext":%s}}}}`},
	}
	needPatching := map[int][]string{}
	for field, k := range patches {
		v, ok := values.Map(spec.Raw).GetPath("k8s." + field)
		if !ok {
			continue
		}
		inner, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		patch := fmt.Sprintf(k.Patch, inner)
		// Find which manifests need the patch
		for idx, m := range manifests {
			if k.Kind == m.GetKind() && k.Name == m.GetName() {
				needPatching[idx] = append(needPatching[idx], patch)
			}
		}
	}

	for idx, patches := range needPatching {
		m := manifests[idx]
		baseJSON, err := yaml.YAMLToJSON([]byte(m.Content))
		if err != nil {
			return nil, err
		}
		typed, err := scheme.Scheme.New(m.GroupVersionKind())
		if err != nil {
			return nil, err
		}

		for _, patch := range patches {
			newBytes, err := strategicpatch.StrategicMergePatch(baseJSON, []byte(patch), typed)
			if err != nil {
				return nil, fmt.Errorf("patch: %v", err)
			}
			baseJSON = newBytes
		}
		// Rebuild our manifest
		nm, err := manifest.FromJson(baseJSON)
		if err != nil {
			return nil, err
		}
		manifests[idx] = nm
	}

	for _, o := range spec.Kubernetes.Overlays {
		for idx, m := range manifests {
			if o.Kind != m.GetKind() {
				continue
			}
			// While patches have ApiVersion, this is ignored for legacy compatibility
			if o.Name != m.GetName() {
				continue
			}
			mfs, err := applyPatches(m, o.Patches)
			if err != nil {
				return nil, err
			}
			manifests[idx] = mfs
		}
	}

	return manifests, nil
}

// applyPatches applies the given patches against the given object. It returns the resulting patched YAML if successful,
// or a list of errors otherwise.
func applyPatches(base manifest.Manifest, patches []apis.Patch) (manifest.Manifest, error) {
	bo := make(map[any]any)
	// Use yaml2 specifically to allow interface{} as key which WritePathContext treats specially
	err := yaml2.Unmarshal([]byte(base.Content), &bo)
	if err != nil {
		return manifest.Manifest{}, err
	}
	var errs util.Errors
	for _, p := range patches {
		v := p.Value
		inc, _, err := tpath.GetPathContext(bo, util.PathFromString(p.Path), true)
		if err != nil {
			errs = util.AppendErr(errs, err)
			continue
		}

		err = tpath.WritePathContext(inc, v)
		if err != nil {
			errs = util.AppendErr(errs, err)
		}
	}
	oy, err := yaml2.Marshal(bo)
	if err != nil {
		return manifest.Manifest{}, util.AppendErr(errs, err).ToError()
	}

	return manifest.FromYaml(oy)
}
