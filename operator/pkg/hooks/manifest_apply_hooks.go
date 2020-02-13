package hooks

import (
	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/operator/pkg/util"
)

var (
	// preManifestApplyHooks is a list of hook version constraint pairs mapping to a slide of corresponding hooks to run
	// before manifest apply.
	preManifestApplyHooks = []hookVersionMapping{
		{
			sourceVersionConstraint: ">=1.4",
			targetVersionConstraint: ">=1.4",
			hooks:                   []hook{checkMixerTelemetry},
		},
	}
	// postManifestApplyHooks is a list of hook version constraint pairs mapping to a slide of corresponding hooks to run
	// before manifest apply.
	postManifestApplyHooks []hookVersionMapping
)


func RunPreManifestApplyHooks(kubeClient manifest.ExecClient, hc *HookCommonParams, dryRun bool) util.Errors {
	return runUpgradeHooks(preManifestApplyHooks, kubeClient, hc, dryRun)
}

func RunPostManifestApplyHooks(kubeClient manifest.ExecClient, hc *HookCommonParams, dryRun bool) util.Errors {
	return runUpgradeHooks(postManifestApplyHooks, kubeClient, hc, dryRun)
}