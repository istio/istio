package hooks

import (
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-version"

	"istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util"
	"istio.io/pkg/log"
)

// hook is a callout function that may be called during an upgrade to check state or modify the cluster.
// hooks should only be used for version-specific actions.
type hook func(kubeClient manifest.ExecClient, params HookCommonParams) util.Errors
type hooks []hook

// hookVersionMapping is a mapping between a hashicorp/go-version formatted constraints for the source and target
// versions and the list of hooks that should be run if the constraints match.
type hookVersionMapping struct {
	sourceVersionConstraint string
	targetVersionConstraint string
	hooks                   hooks
}

// HookCommonParams is a set of common params passed to all hooks.
type HookCommonParams struct {
	SourceVer                string
	TargetVer                string
	DefaultTelemetryManifest map[string]*object.K8sObject
	SourceIOPS               *v1alpha1.IstioOperatorSpec
	TargetIOPS               *v1alpha1.IstioOperatorSpec
}

var (
	// TODO: add full list
	CRKindNamesMap = map[string][]string{
		"instance": {"requestsize", "requestcount, requestduration", "attributes"},
		"rule":     {"promhttp", "kubeattrgenrulerule"},
		"handler":  {"prometheus", "kubernetesenv"},
	}

	KindResourceMap = map[string]string{
		"instance": "instances",
		"rule":     "rules",
		"handler":  "handlers",
	}
)

const (
	// TODO: Instead of failing the upgrade request to v2, give a helpful message and fallback to mixerv1 installation.
	CustomMixerHelpMessage = ""
)

// runUpgradeHooks checks a list of hook version map entries and runs the hooks in each entry whose constraints match
// the source/target versions in hc.
func runUpgradeHooks(hml []hookVersionMapping, kubeClient manifest.ExecClient, hc *HookCommonParams, dryRun bool) util.Errors {
	var errs util.Errors
	_, err := version.NewVersion(hc.SourceVer)
	if err != nil {
		return util.NewErrs(err)
	}
	_, err = version.NewVersion(hc.TargetVer)
	if err != nil {
		return util.NewErrs(err)
	}

	for _, h := range hml {
		matches, err := checkHookListEntry(h, hc)
		if err != nil {
			errs = util.AppendErr(errs, err)
			continue
		}
		if !matches {
			continue
		}
		log.Infof("Running the following hooks which match source->target versions %s->%s: %s", hc.SourceVer, hc.TargetVer, h.hooks)
		if dryRun {
			log.Info("(Skipping running hooks due to dry-run being set.)")
			continue
		}
		for _, hf := range h.hooks {
			log.Infof("Running hook %s", hf)
			errs = util.AppendErrs(errs, hf(kubeClient, *hc))
		}
	}
	return errs
}

// checkHookListEntry checks a hookVersionMapping against the source/target versions in hc and returns true if it
// matches.
func checkHookListEntry(h hookVersionMapping, hc *HookCommonParams) (bool, error) {
	ch, err := checkConstraint(hc.SourceVer, h.sourceVersionConstraint)
	if err != nil {
		return false, err
	}
	if !ch {
		log.Infof("Source version %s does not satisfy source constraint %s, skip hooks", hc.SourceVer, h.sourceVersionConstraint)
		return false, nil
	}

	ch, err = checkConstraint(hc.TargetVer, h.targetVersionConstraint)
	if err != nil {
		return false, err
	}
	if !ch {
		log.Infof("Target version %s does not satisfy target constraint %s, skip hooks", hc.TargetVer, h.targetVersionConstraint)
		return false, nil
	}
	return true, nil
}

// checkConstraint reports whether SemVer formatted string verStr matches hashicorp/go-version formatted constraints
// in constraintStr.
func checkConstraint(verStr, constraintStr string) (bool, error) {
	ver, err := version.NewVersion(verStr)
	if err != nil {
		return false, err
	}
	constraint, err := version.NewConstraint(constraintStr)
	if err != nil {
		return false, err
	}
	return constraint.Check(ver), nil
}

// checkMixerTelemetry compares default mixer telemetry configs with corresponding in-cluster configs
// consider these cases with difference:
// 1. new in-cluster CR which does not exists in default configs
// 2. same CR but with difference in fields
// 3. remove CR from default configs(upgrade can proceed for this case)
func checkMixerTelemetry(kubeClient manifest.ExecClient, params HookCommonParams) util.Errors {
	knMapDefault, err := extractTargetKNMapFromDefault(params.DefaultTelemetryManifest)
	if err != nil {
		return util.NewErrs(err)
	}
	knMapCluster, err := extractKNMapFromCluster(kubeClient)
	if err != nil {
		return util.NewErrs(err)
	}

	for nk, inclusterCR := range knMapCluster {
		defaultCR, ok := knMapDefault[nk]
		if !ok {
			// for case 1
			return util.NewErrs(fmt.Errorf("there are extra non default mixer configs in cluster " + CustomMixerHelpMessage))
		}
		// for case 2
		diff := util.YAMLDiff(defaultCR, inclusterCR)
		if diff != "" {
			return util.NewErrs(fmt.Errorf("customized config exists for %s,"+
				" diff is: %s. \n" + CustomMixerHelpMessage, nk, diff))
		}
	}
	return nil
}

func extractTargetKNMapFromDefault(nkMap map[string]*object.K8sObject) (map[string]string, error) {
	checkMap := make(map[string]string)
	for kind, names := range CRKindNamesMap {
		for _, name := range names {
			knKey := kind + ":" + name
			msObject, ok := nkMap[knKey]
			if !ok {
				continue
			}
			item := msObject.GetObject()
			spec, ok := item.UnstructuredContent()["spec"].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("failed to get spec from unstructured item"+
					" of kind: %s, name: %s", kind, name)
			}
			specYAML, err := yaml.Marshal(spec)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal spec of kind: %s, name: %s", kind, name)
			}
			checkMap[knKey] = string(specYAML)
		}
	}
	return checkMap, nil
}

func extractKNMapFromCluster(kubeClient manifest.ExecClient) (map[string]string, error) {
	knYAMLMap := make(map[string]string)
	for kind := range CRKindNamesMap {
		uls, err := kubeClient.GetGroupVersionResource(configAPIGroup, configAPIVersion, KindResourceMap[kind], istioNamespace, "")
		if err != nil {
			return nil, err
		}
		for _, item := range uls.Items {
			meta, _ := item.UnstructuredContent()["metadata"].(map[string]interface{})
			name, _ := meta["name"].(string)
			spec, ok := item.UnstructuredContent()["spec"].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("failed to get spec from unstructured item"+
					" of kind: %s, name: %s", kind, name)
			}
			specYAML, err := yaml.Marshal(spec)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal spec of kind: %s, name: %s", kind, name)
			}
			key := kind + ":" + name
			knYAMLMap[key] = string(specYAML)
		}
	}
	return knYAMLMap, nil
}
