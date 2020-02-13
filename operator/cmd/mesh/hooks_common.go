package mesh

import (
	"fmt"

	"istio.io/istio/operator/pkg/hooks"
	"istio.io/istio/operator/pkg/manifest"
	pkgversion "istio.io/istio/operator/pkg/version"
)

// runPreApplyHook prepares and triggers the prerun hook for manifest apply command
func runPreApplyHook(args *rootArgs, maArgs *manifestApplyArgs, l *Logger) error {
	// Create a kube client from args.kubeConfigPath and  args.context
	kubeClient, err := manifest.NewClient(maArgs.kubeConfigPath, maArgs.context)
	if err != nil {
		return fmt.Errorf("failed to connect Kubernetes API server, error: %v", err)
	}
	hparams, err := generateHookParam(kubeClient, maArgs.inFilename, maArgs.force, l)
	if err != nil {
		return fmt.Errorf("failed to generate param for hook: %v", err)
	}
	errs := hooks.RunPreManifestApplyHooks(kubeClient, hparams, args.dryRun)
	if len(errs) != 0 && !maArgs.force {
		return errs.ToError()
	}
	return nil
}

// RunPreUpgradeHooks prepares and triggers the prerun hook for upgrade command
func RunPreUpgradeHooks(kubeClient *manifest.Client, args *rootArgs, ugArgs *upgradeArgs, l *Logger) error {
	// Run pre-upgrade hooks
	hparams, err := generateHookParam(kubeClient, ugArgs.inFilename, ugArgs.force, l)
	if err != nil {
		return fmt.Errorf("failed to generate param for hook: %v", err)
	}
	errs := hooks.RunPreUpgradeHooks(kubeClient, hparams, args.dryRun)
	if len(errs) != 0 && !ugArgs.force {
		return errs.ToError()
	}
	return nil
}

// RunPostUpgradeHooks prepares and triggers the postrun hook for upgrade command
func RunPostUpgradeHooks(kubeClient *manifest.Client, args *rootArgs, maArgs *upgradeArgs, l *Logger) error {
	hparams, err := generateHookParam(kubeClient, maArgs.inFilename, maArgs.force, l)
	if err != nil {
		return fmt.Errorf("failed to generate param for hook: %v", err)
	}
	errs := hooks.RunPostUpgradeHooks(kubeClient, hparams, args.dryRun)
	if len(errs) != 0 && !maArgs.force {
		return errs.ToError()
	}
	return nil
}

func generateHookParam(kubeClient *manifest.Client, inFilename []string, force bool, l *Logger) (*hooks.HookCommonParams, error) {
	// Generate IOPS objects
	_, targetIOPS, err := genIOPS(inFilename, "", "", "", force, kubeClient.Config, l)
	if err != nil {
		return nil, fmt.Errorf("failed to generate IOPS from file %s, error: %s", inFilename, err)
	}

	targetTag := targetIOPS.GetTag()
	targetVersion, err := pkgversion.TagToVersionString(targetTag)

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
