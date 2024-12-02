// Copyright Istio Authors
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

type patchContext struct {
	Patch       string
	PostProcess func([]byte) ([]byte, error)
}

// postProcess applies any manifest manipulation to be done after Helm chart rendering.
func postProcess(comp component.Component, spec apis.GatewayComponentSpec, manifests []manifest.Manifest, vals values.Map) ([]manifest.Manifest, error) {
	if spec.Kubernetes == nil {
		// No post-processing steps to apply
		return manifests, nil
	}
	type Patch struct {
		Kind, Name  string
		Patch       string
		PostProcess func([]byte) ([]byte, error)
	}
	rn := comp.ResourceName
	if spec.Name != "" {
		// Gateways can override the name
		rn = spec.Name
	}
	if comp.UserFacingName == component.PilotComponentName {
		if rev := vals.GetPathStringOr("spec.values.revision", "default"); rev != "default" {
			rn = rn + "-" + rev
		}
	}
	rt := comp.ResourceType
	// Setup our patches. Each patch takes from some top level field under the component.k8s spec (the map key), and applies to some resource.
	// The patch is just a standard StrategicMergePatch.
	patches := map[string]Patch{
		"affinity": {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"affinity":%s}}}}`},
		"env": {
			Kind:  rt,
			Name:  rn,
			Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "env": %%s}]}}}}`, comp.ContainerName),
		},
		"hpaSpec": {Kind: "HorizontalPodAutoscaler", Name: rn, Patch: `{"spec":%s}`},
		"imagePullPolicy": {
			Kind:  rt,
			Name:  rn,
			Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "imagePullPolicy": %%s}]}}}}`, comp.ContainerName),
		},
		"nodeSelector": {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"nodeSelector":%s}}}}`},
		"podDisruptionBudget": {
			Kind:        "PodDisruptionBudget",
			Name:        rn,
			Patch:       `{"spec":%s}`,
			PostProcess: postProcessPodDisruptionBudget,
		},
		"podAnnotations":    {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"metadata":{"annotations":%s}}}}`},
		"priorityClassName": {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"priorityClassName":%s}}}}`},
		"readinessProbe": {
			Kind:  rt,
			Name:  rn,
			Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "readinessProbe": %%s}]}}}}`, comp.ContainerName),
		},
		"replicaCount": {Kind: rt, Name: rn, Patch: `{"spec":{"replicas":%s}}`},
		"resources": {
			Kind:  rt,
			Name:  rn,
			Patch: fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":%q, "resources": %%s}]}}}}`, comp.ContainerName),
		},
		"strategy":           {Kind: rt, Name: rn, Patch: `{"spec":{"strategy":%s}}`},
		"tolerations":        {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"tolerations":%s}}}}`},
		"serviceAnnotations": {Kind: "Service", Name: rn, Patch: `{"metadata":{"annotations":%s}}`},
		"service":            {Kind: "Service", Name: rn, Patch: `{"spec":%s}`},
		"securityContext":    {Kind: rt, Name: rn, Patch: `{"spec":{"template":{"spec":{"securityContext":%s}}}}`},
	}
	// needPatching builds a map of manifest index -> patch. This ensures we only do the full round-tripping once per object.
	needPatching := map[int][]patchContext{}
	for field, k := range patches {
		if field == "service" && comp.IsGateway() {
			// Hack: https://github.com/kubernetes/kubernetes/issues/103544 means strategy merge is ~broken for service ports.
			// We already handle the service ports as helm values (since they are used for other things as well, like Pod container pod),
			// so simply remove this. We cannot skip it entirely since there could be other parts of service they are patching.
			_ = values.Map(spec.Raw).SetPath("k8s.service.ports", []any{})
		}
		v, ok := values.Map(spec.Raw).GetPath("k8s." + field)
		if !ok {
			continue
		}
		// Get the users patch...
		inner, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		// And insert it into our patch template
		patch := fmt.Sprintf(k.Patch, inner)
		// Find which manifests need the patch
		for idx, m := range manifests {
			if k.Kind == m.GetKind() && k.Name == m.GetName() {
				needPatching[idx] = append(needPatching[idx], patchContext{Patch: patch, PostProcess: k.PostProcess})
			}
		}
	}

	// For anything needing a patch, apply them.
	for idx, patches := range needPatching {
		m := manifests[idx]
		// Convert to JSON, which the StrategicMergePatch requires
		baseJSON, err := yaml.YAMLToJSON([]byte(m.Content))
		if err != nil {
			return nil, err
		}
		typed, err := scheme.Scheme.New(m.GroupVersionKind())
		if err != nil {
			return nil, err
		}

		// Apply all the patches
		for _, patch := range patches {
			newBytes, err := strategicpatch.StrategicMergePatch(baseJSON, []byte(patch.Patch), typed)
			if err != nil {
				return nil, fmt.Errorf("patch: %v", err)
			}
			if patch.PostProcess != nil {
				newBytes, err = patch.PostProcess(newBytes)
				if err != nil {
					return nil, fmt.Errorf("patch post process: %v", err)
				}
			}
			baseJSON = newBytes
		}
		// Rebuild our manifest
		nm, err := manifest.FromJSON(baseJSON)
		if err != nil {
			return nil, err
		}
		// Update the manifests list.
		manifests[idx] = nm
	}

	// In addition to the structured patches, we also allow arbitrary overlays.
	for _, o := range spec.Kubernetes.Overlays {
		for idx, m := range manifests {
			if o.Kind != m.GetKind() {
				continue
			}
			// While patches have ApiVersion, this is ignored for legacy compatibility
			if o.Name != m.GetName() {
				continue
			}
			// Overlay applies to this manifest, apply it and update
			mfs, err := applyPatches(m, o.Patches)
			if err != nil {
				return nil, err
			}
			manifests[idx] = mfs
		}
	}

	return manifests, nil
}

// PDB does not allow both minAvailable and maxUnavailable to be set on the same object, but the strategic merge patch
// configuration does not account for this. Since Istio defaults `minAvailable`, this would otherwise prevent users from
// setting maxUnavailable.
func postProcessPodDisruptionBudget(bytes []byte) ([]byte, error) {
	v, err := values.MapFromJSON(bytes)
	if err != nil {
		return nil, err
	}
	_, hasMax := v.GetPath("spec.maxUnavailable")
	_, hasMin := v.GetPath("spec.minAvailable")
	if hasMax && hasMin {
		if err := v.SetPath("spec.minAvailable", nil); err != nil {
			return nil, err
		}
	}
	return []byte(v.JSON()), nil
}

// applyPatches applies the given patches against the given object. It returns the resulting patched YAML if successful,
// or a list of errors otherwise.
func applyPatches(base manifest.Manifest, patches []apis.Patch) (manifest.Manifest, error) {
	bo := make(map[any]any)
	// Use yaml2 specifically to allow interface{} as key which WritePathContext treats specially
	// TODO: can we get away from using yaml2 and tpath?
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
