package mesh

import (
	"fmt"

	"istio.io/istio/operator/pkg/hooks"
	"istio.io/istio/operator/pkg/manifest"
	pkgversion "istio.io/istio/operator/pkg/version"
)

// runPreApplyHook prepares and triggers the prerun hook for manifest apply command
func runPreApplyHook(kubeClient *manifest.Client, inFilenames []string, force bool, dryRun bool, l *Logger) error {
	hparams, err := generateHookParam(kubeClient, inFilenames, force, l)
	if err != nil {
		return fmt.Errorf("failed to generate param for hook: %v", err)
	}
	errs := hooks.RunPreManifestApplyHooks(kubeClient, hparams, dryRun)
	if len(errs) != 0 && !force {
		return errs.ToError()
	}
	return nil
}

// runPreUpgradeHooks prepares and triggers the prerun hook for upgrade command
func runPreUpgradeHooks(kubeClient *manifest.Client, args *rootArgs, ugArgs *upgradeArgs, l *Logger) error {
	// Run pre-upgrade hooks
	hparams, err := generateHookParam(kubeClient, ugArgs.inFilenames, ugArgs.force, l)
	if err != nil {
		return fmt.Errorf("failed to generate param for hook: %v", err)
	}
	errs := hooks.RunPreUpgradeHooks(kubeClient, hparams, args.dryRun)
	if len(errs) != 0 && !ugArgs.force {
		return errs.ToError()
	}
	return nil
}

// runPostUpgradeHooks prepares and triggers the postrun hook for upgrade command
func runPostUpgradeHooks(kubeClient *manifest.Client, args *rootArgs, maArgs *upgradeArgs, l *Logger) error {
	hparams, err := generateHookParam(kubeClient, maArgs.inFilenames, maArgs.force, l)
	if err != nil {
		return fmt.Errorf("failed to generate param for hook: %v", err)
	}
	errs := hooks.RunPostUpgradeHooks(kubeClient, hparams, args.dryRun)
	if len(errs) != 0 && !maArgs.force {
		return errs.ToError()
	}
	return nil
}

func generateHookParam(kubeClient *manifest.Client, inFilenames []string, force bool, l *Logger) (*hooks.HookCommonParams, error) {
	// Generate IOPS objects
	_, targetIOPS, err := GenerateConfig(inFilenames, "", force, nil, l)
	if err != nil {
		return nil, fmt.Errorf("failed to generate IOPS from file %s, error: %s", inFilenames, err)
	}

	targetTag := targetIOPS.Tag
	targetVersion, err := pkgversion.TagToVersionString(fmt.Sprint(targetTag))
	if err != nil && !force {
		return nil, fmt.Errorf("failed to convert the target tag '%s' into a valid version, "+
			"you can use --force flag to skip the version check if you know the tag is correct", targetTag)
	}

	istioNamespace := targetIOPS.MeshConfig.RootNamespace
	currentVersion, err := retrieveControlPlaneVersion(kubeClient, istioNamespace, l)
	if err != nil && force {
		return nil, fmt.Errorf("failed to read the current Istio version, error: %v", err)
	}

	nkMap, err := generateDefaultTelemetryManifest(l)
	if err != nil {
		return nil, fmt.Errorf("failed to generate default manifest for telemetry: %v", err)
	}
	return &hooks.HookCommonParams{
		SourceVer:                currentVersion,
		TargetVer:                targetVersion,
		DefaultTelemetryManifest: nkMap,
		SourceIOPS:               nil,
		TargetIOPS:               nil}, nil
}
