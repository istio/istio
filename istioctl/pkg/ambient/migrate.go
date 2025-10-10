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

package ambient

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"istio.io/api/annotation"
	"istio.io/api/label"
	apisecurityv1beta1 "istio.io/api/security/v1beta1"
	v1beta1 "istio.io/api/type/v1beta1"
	securityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/operator/pkg/version"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

var (
	outputDir           string
	namespace           string
	allNamespaces       bool
	dryRun              bool
	compatibilityCheck  bool
	ambientSetup        bool
	deployWaypointsFlag bool
	policyMigration     bool
	waypointConfig      bool
	policyCleanup       bool
	sidecarRemoval      bool
)

// MigrationStep represents a step in the migration process
type MigrationStep string

const (
	StepCompatibilityCheck    MigrationStep = "compatibility-check"
	StepAmbientSetup          MigrationStep = "ambient-setup"
	StepWaypointDeploy        MigrationStep = "deploy-waypoints"
	StepPolicyMigration       MigrationStep = "policy-migration"
	StepWaypointConfiguration MigrationStep = "waypoint-configuration"
	StepPolicyCleanup         MigrationStep = "policy-cleanup"
	StepSidecarRemoval        MigrationStep = "sidecar-removal"
)

// MigrationResult contains the result of a migration step
type MigrationResult struct {
	Step            MigrationStep
	Success         bool
	Message         string
	StatusMessages  []string
	Recommendations []string
	Warnings        []string
	Errors          []string
}

// NamespaceInfo contains information about a namespace for migration
type NamespaceInfo struct {
	Name                     string
	HasSidecars              bool
	HasAuthorizationPolicies bool
	NeedsWaypoint            bool
	HasWaypoints             bool
	Services                 []ServiceInfo
	AuthorizationPolicies    []string
}

// ServiceInfo contains information about a service
type ServiceInfo struct {
	Name               string
	Namespace          string
	HasHTTPPolicies    bool
	NeedsWaypoint      bool
	VirtualServiceName string
}

// WaypointInfo contains information about a waypoint
type WaypointInfo struct {
	Name      string
	Namespace string
}

// DetailedNamespaceAnalysis provides comprehensive analysis of a namespace for migration
type DetailedNamespaceAnalysis struct {
	Name                     string
	HasSidecars              bool
	HasAuthorizationPolicies bool
	NeedsWaypoint            bool
	Services                 []ServiceAnalysis
	AuthorizationPolicies    []AuthorizationPolicyAnalysis
	Pods                     []PodAnalysis
	StatusMessages           []string
	Recommendations          []string
}

// ServiceAnalysis provides detailed analysis of a service
type ServiceAnalysis struct {
	Name             string
	Namespace        string
	HasHTTPPolicies  bool
	NeedsWaypoint    bool
	PolicyReferences []string
}

// AuthorizationPolicyAnalysis provides analysis of authorization policies
type AuthorizationPolicyAnalysis struct {
	Name          string
	Namespace     string
	HasHTTPRules  bool
	TargetRef     string
	NeedsWaypoint bool
}

// PodAnalysis provides analysis of pods in the namespace
type PodAnalysis struct {
	Name       string
	Namespace  string
	HasSidecar bool
	IsAmbient  bool
}

func MigrateCmd(ctx cli.Context) *cobra.Command {
	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate from sidecar mesh to ambient mesh",
		Long: `Migrate from sidecar mesh to ambient mesh.

This command provides a comprehensive migration process from Istio sidecar mesh to ambient mesh.
The migration analyzes your cluster and provides step-by-step recommendations for a zero-downtime transition.



The migration process will check for these features and provide guidance if they are detected.

You can run specific analysis steps using individual flags, or run all steps by default.
`,
		Example: `  # Run complete migration analysis and get recommendations
  istioctl ambient migrate

  # Check compatibility only
  istioctl ambient migrate --compatibility-check

  # Deploy waypoints for ambient mesh
  istioctl ambient migrate --deploy-waypoints

  # Check policy migration requirements
  istioctl ambient migrate --policy-migration

  # Dry run to see what would be done
  istioctl ambient migrate --dry-run

  # Migrate specific namespace
  istioctl ambient migrate --namespace my-namespace

  # Output results to directory
  istioctl ambient migrate --output-dir /tmp/migration`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigration(ctx, cmd)
		},
	}

	migrateCmd.PersistentFlags().StringVar(&outputDir, "output-dir", "/tmp/istio-migrate",
		"Directory to output migration results and recommendations")
	migrateCmd.PersistentFlags().StringVar(&namespace, "namespace", "",
		"Specific namespace to migrate (if not specified, migrates all eligible namespaces)")
	migrateCmd.PersistentFlags().BoolVar(&allNamespaces, "all-namespaces", false,
		"Migrate all namespaces in the cluster")
	migrateCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false,
		"Show what would be done without making changes")

	// Individual analysis flags
	migrateCmd.PersistentFlags().BoolVar(&compatibilityCheck, "compatibility-check", false,
		"Check prerequisites for ambient migration")
	migrateCmd.PersistentFlags().BoolVar(&ambientSetup, "ambient-setup", false,
		"Verify cluster is ready for ambient mesh")
	migrateCmd.PersistentFlags().BoolVar(&deployWaypointsFlag, "deploy-waypoints", false,
		"Deploy and configure waypoints for ambient mesh")
	migrateCmd.PersistentFlags().BoolVar(&policyMigration, "policy-migration", false,
		"Analyze authorization policies for migration to waypoints")
	migrateCmd.PersistentFlags().BoolVar(&waypointConfig, "waypoint-config", false,
		"Analyze waypoint configuration requirements")
	migrateCmd.PersistentFlags().BoolVar(&policyCleanup, "policy-cleanup", false,
		"Analyze policy cleanup requirements")
	migrateCmd.PersistentFlags().BoolVar(&sidecarRemoval, "sidecar-removal", false,
		"Analyze sidecar removal requirements")

	return migrateCmd
}

// Helper functions for step display
func getStepDescription(step MigrationStep) string {
	switch step {
	case StepCompatibilityCheck:
		return "Checking compatibility requirements"
	case StepAmbientSetup:
		return "Verifying ambient mesh setup"
	case StepWaypointDeploy:
		return "Deploying waypoints"
	case StepPolicyMigration:
		return "Migrating authorization policies"
	case StepWaypointConfiguration:
		return "Configuring waypoint usage"
	case StepPolicyCleanup:
		return "Cleaning up redundant policies"
	case StepSidecarRemoval:
		return "Planning sidecar removal"
	default:
		return string(step)
	}
}

func runMigration(ctx cli.Context, cmd *cobra.Command) error {
	kubeClient, err := ctx.CLIClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// Create output directory if it doesn't exist
	if !dryRun {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %v", err)
		}
	}

	// Check if any specific flags are set
	hasSpecificFlags := compatibilityCheck || ambientSetup || deployWaypointsFlag ||
		policyMigration || waypointConfig || policyCleanup || sidecarRemoval

	if hasSpecificFlags {
		fmt.Fprintf(cmd.OutOrStdout(), "🔍 Running specific migration checks...\n")
		if dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "🔍 Running in dry-run mode - no changes will be made to your cluster\n")
		}
		return runSpecificAnalysis(ctx, kubeClient, cmd)
	}

	// Run all analysis steps by default
	fmt.Fprintf(cmd.OutOrStdout(), "🚀 Starting Istio sidecar to ambient mesh migration checks...\n")
	fmt.Fprintf(cmd.OutOrStdout(), "📋 This will check your cluster and provide step-by-step migration recommendations\n")
	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "🔍 Running in dry-run mode - no changes will be made to your cluster\n")
	}

	// Run all checks
	results, err := runFullMigrationAnalysis(ctx, kubeClient, cmd)
	if err != nil {
		return err
	}

	// All checks completed successfully
	fmt.Fprintf(cmd.OutOrStdout(), "\n🎉 Migration checks completed successfully!\n")
	fmt.Fprintf(cmd.OutOrStdout(), "📄 All results and recommendations have been saved to: %s\n", outputDir)
	fmt.Fprintf(cmd.OutOrStdout(), "📖 Review the output files and follow the recommendations to complete your migration.\n")

	// Write final summary to output directory
	if !dryRun {
		if err := writeMigrationSummary(outputDir, results); err != nil {
			return fmt.Errorf("failed to write migration summary: %v", err)
		}
	}

	return nil
}

func runSpecificAnalysis(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) error {
	var allResults []MigrationResult

	// Run only the requested analysis steps
	if compatibilityCheck {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running compatibility check...\n")
		result, err := runStep(ctx, kubeClient, StepCompatibilityCheck, cmd)
		if err != nil {
			return fmt.Errorf("compatibility check failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("compatibility check failed")
		}
	}

	if ambientSetup {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running ambient setup check...\n")
		result, err := runStep(ctx, kubeClient, StepAmbientSetup, cmd)
		if err != nil {
			return fmt.Errorf("ambient setup analysis failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("ambient setup analysis failed")
		}
	}

	if deployWaypointsFlag {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running waypoint deployment...\n")
		result, err := runStep(ctx, kubeClient, StepWaypointDeploy, cmd)
		if err != nil {
			return fmt.Errorf("waypoint analysis failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("waypoint analysis failed")
		}
	}

	if policyMigration {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running policy migration check...\n")
		result, err := runStep(ctx, kubeClient, StepPolicyMigration, cmd)
		if err != nil {
			return fmt.Errorf("policy migration analysis failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("policy migration analysis failed")
		}
	}

	if waypointConfig {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running waypoint configuration check...\n")
		result, err := runStep(ctx, kubeClient, StepWaypointConfiguration, cmd)
		if err != nil {
			return fmt.Errorf("waypoint configuration deployment failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("waypoint configuration deployment failed")
		}
	}

	if policyCleanup {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running policy cleanup check...\n")
		result, err := runStep(ctx, kubeClient, StepPolicyCleanup, cmd)
		if err != nil {
			return fmt.Errorf("policy cleanup analysis failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("policy cleanup analysis failed")
		}
	}

	if sidecarRemoval {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 Running sidecar removal check...\n")
		result, err := runStep(ctx, kubeClient, StepSidecarRemoval, cmd)
		if err != nil {
			return fmt.Errorf("sidecar removal analysis failed: %v", err)
		}
		allResults = append(allResults, result)
		printStepResult(cmd, result)
		if !result.Success {
			return fmt.Errorf("sidecar removal analysis failed")
		}
	}

	// Write results to output directory
	if !dryRun {
		if err := writeMigrationSummary(outputDir, allResults); err != nil {
			return fmt.Errorf("failed to write migration summary: %v", err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n✅ Specific checks completed successfully!\n")
	fmt.Fprintf(cmd.OutOrStdout(), "📄 Results have been saved to: %s\n", outputDir)
	return nil
}

func runFullMigrationAnalysis(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) ([]MigrationResult, error) {
	steps := []MigrationStep{
		StepCompatibilityCheck,
		StepAmbientSetup,
		StepWaypointDeploy,
		StepPolicyMigration,
		StepWaypointConfiguration,
		StepPolicyCleanup,
		StepSidecarRemoval,
	}

	var allResults []MigrationResult

	for _, step := range steps {
		fmt.Fprintf(cmd.OutOrStdout(), "\n🔍 %s\n", getStepDescription(step))

		result, err := runStep(ctx, kubeClient, step, cmd)
		if err != nil {
			return nil, fmt.Errorf("%s failed: %v", step, err)
		}

		allResults = append(allResults, result)
		printStepResult(cmd, result)

		// Stop execution if this step failed
		if !result.Success {
			fmt.Fprintf(cmd.ErrOrStderr(), "\n❌ %s failed! Analysis stopped.\n", getStepDescription(step))
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠️ Please review and fix the issues above before continuing.\n")

			// Write partial summary to output directory
			if !dryRun {
				if err := writeMigrationSummary(outputDir, allResults); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "⚠️ Warning: Failed to write migration summary: %v\n", err)
				}
			}

			return nil, fmt.Errorf("analysis stopped due to failed: %s", step)
		}
	}

	return allResults, nil
}

func runStep(ctx cli.Context, kubeClient kube.CLIClient, step MigrationStep, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    step,
		Success: true,
	}

	switch step {
	case StepCompatibilityCheck:
		return checkPrerequisites(ctx, kubeClient, cmd)
	case StepAmbientSetup:
		return checkClusterSetup(ctx, kubeClient, cmd)
	case StepWaypointDeploy:
		return deployWaypoints(ctx, kubeClient, cmd)
	case StepPolicyMigration:
		return migratePolicies(ctx, kubeClient, cmd)
	case StepWaypointConfiguration:
		return useWaypoints(ctx, kubeClient, cmd)
	case StepPolicyCleanup:
		return simplifyPolicies(ctx, kubeClient, cmd)
	case StepSidecarRemoval:
		return removeSidecars(ctx, kubeClient, cmd)
	default:
		return result, fmt.Errorf("unknown step: %s", step)
	}
}

func checkPrerequisites(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepCompatibilityCheck,
		Success: true,
	}

	// Check cluster CNI compatibility
	cniCompatible, err := checkCNICompatibility(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check CNI compatibility: %v", err))
		result.Success = false
	} else if cniCompatible {
		result.StatusMessages = append(result.StatusMessages, "✅ Cluster CNI compatibility: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Cluster CNI compatibility: failed")
		result.Success = false
	}

	// Check Istio version compatibility
	versionCompatible, err := checkIstioVersionCompatibility(kubeClient, ctx.IstioNamespace())
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check Istio version compatibility: %v", err))
		result.Success = false
	} else if versionCompatible {
		result.StatusMessages = append(result.StatusMessages, "✅ Istio version compatibility: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Istio version compatibility: failed")
		result.Success = false
	}

	// Check multicluster usage compatibility
	multiclusterCompatible, err := checkMulticlusterCompatibility(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check multicluster compatibility: %v", err))
		result.Success = false
	} else if multiclusterCompatible {
		result.StatusMessages = append(result.StatusMessages, "✅ Multicluster usage compatibility: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Multicluster usage compatibility: failed")
		result.Success = false
	}

	// Check Virtual Machine usage compatibility
	vmCompatible, err := checkVMCompatibility(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check VM compatibility: %v", err))
		result.Success = false
	} else if vmCompatible {
		result.StatusMessages = append(result.StatusMessages, "✅ Virtual Machine usage compatibility: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Virtual Machine usage compatibility: failed")
		result.Success = false
	}

	// Check SPIRE usage compatibility
	spireCompatible, err := checkSPIRECompatibility(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check SPIRE compatibility: %v", err))
		result.Success = false
	} else if spireCompatible {
		result.StatusMessages = append(result.StatusMessages, "✅ SPIRE usage compatibility: passed")
	} else {
		result.Errors = append(result.Errors, "❌ SPIRE usage compatibility: failed")
		result.Success = false
	}

	if result.Success {
		result.Message = "All compatibility checks passed"
	} else {
		result.Message = "Some compatibility checks failed"
	}

	return result, nil
}

func checkClusterSetup(ctx cli.Context, kubeClient kube.CLIClient, _ *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepAmbientSetup,
		Success: true,
	}

	// Check if ambient mode is enabled
	ambientEnabled, err := checkAmbientModeEnabled(kubeClient, ctx.IstioNamespace())
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check ambient mode: %v", err))
		result.Success = false
	} else if ambientEnabled {
		result.StatusMessages = append(result.StatusMessages, "✅ Ambient mode enabled: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Ambient mode enabled: failed. istiod must have 'PILOT_ENABLE_AMBIENT=true'. Upgrade Istio with '--set profile=ambient'.")
		result.Success = false
	}

	// Check if required DaemonSets are deployed
	ztunnelDeployed, err := checkZtunnelDeployed(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check ztunnel deployment: %v", err))
		result.Success = false
	} else if ztunnelDeployed {
		result.StatusMessages = append(result.StatusMessages, "✅ ztunnel DaemonSet deployed: passed")
	} else {
		result.Errors = append(result.Errors, "❌ DaemonSets deployed: failed - ztunnel not found")
		result.Success = false
	}

	// Check if Istio CNI agent is deployed
	cniAgentDeployed, err := checkCNIAgentDeployed(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check CNI agent deployment: %v", err))
		result.Success = false
	} else if cniAgentDeployed {
		result.StatusMessages = append(result.StatusMessages, "✅ Istio CNI agent deployed: passed")
	} else {
		result.Errors = append(result.Errors, "❌ DaemonSets deployed: failed - Istio CNI agent not found")
		result.Success = false
	}

	// Check if sidecars support ambient mode
	sidecarsCompatible, err := checkSidecarsSupportAmbient(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check sidecar compatibility: %v", err))
		result.Success = false
	} else if sidecarsCompatible {
		result.StatusMessages = append(result.StatusMessages, "✅ Sidecars support ambient mode: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Sidecars support ambient mode: failed")
		result.Success = false
	}

	// Check if required CRDs are installed
	crdsInstalled, err := checkRequiredCRDsInstalled(kubeClient)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Failed to check CRDs: %v", err))
		result.Success = false
	} else if crdsInstalled {
		result.StatusMessages = append(result.StatusMessages, "✅ Required CRDs installed: passed")
	} else {
		result.Errors = append(result.Errors, "❌ Required CRDs installed: failed")
		result.Success = false
	}

	if result.Success {
		result.Message = "Ambient mesh setup is complete and ready"
	} else {
		result.Message = "Ambient mesh setup needs attention"
	}

	return result, nil
}

// checkExistingWaypoints checks if waypoints are already deployed
func checkExistingWaypoints(kubeClient kube.CLIClient) ([]WaypointInfo, error) {
	var waypoints []WaypointInfo

	// Get all namespaces
	namespaces, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces.Items {
		// Check for existing waypoint Gateway in this namespace
		gateways, err := kubeClient.GatewayAPI().GatewayV1().Gateways(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			continue // Skip namespaces we can't analyze
		}

		for _, gateway := range gateways.Items {
			// Check if this is a waypoint Gateway
			if gateway.Spec.GatewayClassName == "istio-waypoint" {
				waypoints = append(waypoints, WaypointInfo{
					Name:      gateway.Name,
					Namespace: gateway.Namespace,
				})
			}
		}
	}

	return waypoints, nil
}

// checkExistingWaypointPolicies checks if waypoint policies are already deployed
func checkExistingWaypointPolicies(kubeClient kube.CLIClient) ([]WaypointInfo, error) {
	var policies []WaypointInfo

	// Get all namespaces
	namespaces, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, ns := range namespaces.Items {
		// Get authorization policies in this namespace
		authzPolicies, err := kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			continue // Skip namespaces we can't analyze
		}

		// Look for any waypoint policies (those with the migrate.istio.io/source-policy label)
		for i := range authzPolicies.Items {
			policy := authzPolicies.Items[i]
			if policy.Labels != nil && policy.Labels["migrate.istio.io/source-policy"] != "" {
				policies = append(policies, WaypointInfo{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				})
			}
		}
	}

	return policies, nil
}

func deployWaypoints(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepWaypointDeploy,
		Success: true,
	}

	// Check if waypoints are already deployed
	existingWaypoints, err := checkExistingWaypoints(kubeClient)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to check existing waypoints: %v", err))
		return result, nil
	}

	if len(existingWaypoints) > 0 {
		result.StatusMessages = append(result.StatusMessages, "✅ Waypoints are already deployed")
		for _, waypoint := range existingWaypoints {
			result.StatusMessages = append(result.StatusMessages,
				fmt.Sprintf("✅ Waypoint %s/%s is already deployed", waypoint.Namespace, waypoint.Name))
		}
		result.Message = fmt.Sprintf("Found %d existing waypoints - analysis complete", len(existingWaypoints))
		return result, nil
	}

	// Analyze namespaces to determine which need waypoints
	namespaces, err := analyzeNamespacesForWaypoints(kubeClient)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to analyze namespaces: %v", err))
		return result, nil
	}

	// Analyze services to determine which need waypoints
	services, err := analyzeServicesForWaypoints(kubeClient)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to analyze services: %v", err))
		return result, nil
	}

	// Generate waypoint configurations
	if len(services) > 0 || len(namespaces) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "📋 Waypoint analysis completed with recommendations:\n")

		// Show namespace recommendations
		for _, ns := range namespaces {
			if ns.NeedsWaypoint {
				fmt.Fprintf(cmd.OutOrStdout(), "  📌 Namespace \"%s\" requires waypoint for these services:\n", ns.Name)
				for _, svc := range ns.Services {
					if svc.NeedsWaypoint {
						fmt.Fprintf(cmd.OutOrStdout(), "     → Service \"%s/%s\" depends on VirtualService \"%s/%s\"\n", ns.Name, svc.Name, ns.Name, svc.VirtualServiceName)
					}
				}
			}
		}

		// Show service recommendations
		for _, svc := range services {
			if svc.NeedsWaypoint {
				fmt.Fprintf(cmd.OutOrStdout(), "  📌 Service \"%s/%s\" requires waypoint for HTTP-based authorization policies\n", svc.Namespace, svc.Name)
			}
		}

		// Generate waypoint YAML
		waypointYAML, err := generateWaypointYAML(namespaces, services)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, fmt.Sprintf("failed to generate waypoint YAML: %v", err))
			return result, nil
		}

		outputDir := getOutputDir()
		filename := filepath.Join(outputDir, "recommended-waypoints.yaml")
		if err := os.WriteFile(filename, []byte(waypointYAML), 0644); err != nil {
			result.Success = false
			result.Errors = append(result.Errors, fmt.Sprintf("failed to write waypoint YAML: %v", err))
			return result, nil
		}

		fmt.Fprintf(cmd.OutOrStdout(), "  💾 Generated waypoint configuration saved to: %s\n", filename)
		result.Recommendations = append(result.Recommendations, fmt.Sprintf("Apply waypoint configuration: kubectl apply -f %s", filename))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "✅ Waypoint analysis completed successfully!\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  ✨ No waypoints required: All services can operate with ztunnel only\n")
	}

	return result, nil
}

func migratePolicies(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepPolicyMigration,
		Success: true,
	}

	// Check if waypoint policies are already deployed
	existingWaypointPolicies, err := checkExistingWaypointPolicies(kubeClient)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Sprintf("failed to check existing waypoint policies: %v", err))
		return result, nil
	}

	if len(existingWaypointPolicies) > 0 {
		result.StatusMessages = append(result.StatusMessages, "✅ Waypoint policies are already deployed")
		for _, policy := range existingWaypointPolicies {
			result.StatusMessages = append(result.StatusMessages,
				fmt.Sprintf("✅ Waypoint policy %s/%s is already deployed", policy.Namespace, policy.Name))
		}
		result.Message = fmt.Sprintf("Found %d existing waypoint policies - policy migration complete", len(existingWaypointPolicies))
		return result, nil
	}

	// Get namespaces to analyze
	var namespaces []string
	if namespace != "" {
		namespaces = []string{namespace}
	} else if allNamespaces {
		nsList, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return result, fmt.Errorf("failed to list namespaces: %v", err)
		}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	} else {
		namespaces = []string{ctx.NamespaceOrDefault("default")}
	}

	var migratedPolicies []string
	var recommendedPolicies []string

	for _, nsName := range namespaces {
		// Get authorization policies in the namespace
		authzPolicies, err := kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies(nsName).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list authorization policies in namespace %s: %v", nsName, err))
			continue
		}

		for _, policy := range authzPolicies.Items {
			// Check if policy has L7 (HTTP) rules that need waypoint migration
			if hasL7Rules(policy) {
				// Generate waypoint-compatible policy
				waypointPolicy := generateWaypointCompatiblePolicy(policy, nsName)
				recommendedPolicies = append(recommendedPolicies, waypointPolicy)
				migratedPolicies = append(migratedPolicies, fmt.Sprintf("%s/%s", nsName, policy.Name))
			}
		}
	}

	// Write recommended policies to file
	if !dryRun && len(recommendedPolicies) > 0 {
		policiesFile := filepath.Join(outputDir, "recommended-policies.yaml")
		combinedPolicies := strings.Join(recommendedPolicies, "\n---\n")

		if err := os.WriteFile(policiesFile, []byte(combinedPolicies), 0644); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to write recommended policies: %v", err))
		} else {
			result.Recommendations = append(result.Recommendations,
				fmt.Sprintf("Recommended policies written to %s", policiesFile))
		}
	}

	result.Message = fmt.Sprintf("Policy migration completed for %d authorization policies", len(migratedPolicies))
	if len(migratedPolicies) > 0 {
		result.Recommendations = append(result.Recommendations,
			fmt.Sprintf("Apply recommended policies with: kubectl apply -f %s/recommended-policies.yaml", outputDir))

		for _, policyName := range migratedPolicies {
			result.Recommendations = append(result.Recommendations,
				fmt.Sprintf("Policy %s has been migrated for waypoint compatibility", policyName))
		}
	}

	return result, nil
}

// generateWaypointCompatiblePolicy creates a waypoint-compatible version of an authorization policy
func generateWaypointCompatiblePolicy(originalPolicy *securityv1beta1.AuthorizationPolicy, namespace string) string {
	// Create a new policy that targets the waypoint instead of the workload
	waypointPolicy := &securityv1beta1.AuthorizationPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.istio.io/v1beta1",
			Kind:       "AuthorizationPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      originalPolicy.Name + "-waypoint",
			Namespace: namespace,
			Labels: map[string]string{
				"migrate.istio.io/source-policy": namespace + "." + originalPolicy.Name,
			},
		},
		Spec: apisecurityv1beta1.AuthorizationPolicy{
			TargetRefs: []*v1beta1.PolicyTargetReference{
				{
					Group: "gateway.networking.k8s.io",
					Kind:  "Gateway",
					Name:  "waypoint",
				},
			},
			Rules: originalPolicy.Spec.Rules,
		},
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(waypointPolicy)
	if err != nil {
		return fmt.Sprintf("# Error marshaling policy: %v", err)
	}

	return string(yamlBytes)
}

func useWaypoints(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepWaypointConfiguration,
		Success: true,
	}

	// Get namespaces that need waypoint enablement
	var namespaces []string
	if namespace != "" {
		namespaces = []string{namespace}
	} else if allNamespaces {
		nsList, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return result, fmt.Errorf("failed to list namespaces: %v", err)
		}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	} else {
		namespaces = []string{ctx.NamespaceOrDefault("default")}
	}

	enabledWaypoints := 0

	for _, nsName := range namespaces {
		// Check if namespace needs waypoint enablement
		analysis, err := analyzeNamespaceDetailed(kubeClient, nsName)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to analyze namespace %s: %v", nsName, err))
			continue
		}

		if analysis.NeedsWaypoint {
			// Check if waypoint exists
			waypoints, err := kubeClient.GatewayAPI().GatewayV1().Gateways(nsName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list waypoints in namespace %s: %v", nsName, err))
				continue
			}

			hasWaypoint := false
			for _, gw := range waypoints.Items {
				if gw.Spec.GatewayClassName == "istio-waypoint" {
					hasWaypoint = true
					break
				}
			}

			if hasWaypoint {
				// Check if namespace is configured to use the waypoint
				ns, err := kubeClient.Kube().CoreV1().Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get namespace %s: %v", nsName, err))
					continue
				}

				hasWaypointLabel := false
				if ns.Labels != nil {
					if waypointName, exists := ns.Labels["istio.io/use-waypoint"]; exists && waypointName != "" {
						hasWaypointLabel = true
					}
				}

				if !hasWaypointLabel {
					fmt.Fprintf(cmd.OutOrStdout(), "  ⚙️ Namespace %s needs waypoint configuration: kubectl label namespace %s istio.io/use-waypoint=waypoint\n", nsName, nsName)
					result.Recommendations = append(result.Recommendations,
						fmt.Sprintf("kubectl label namespace %s istio.io/use-waypoint=waypoint", nsName))
				} else {
					enabledWaypoints++
					fmt.Fprintf(cmd.OutOrStdout(), "  ✅ Namespace %s is already configured to use waypoints\n", nsName)
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "  ⚠️ Unused waypoint detected: %s/waypoint is not configured for any namespace\n", nsName)
			}
		}
	}

	if len(result.Recommendations) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "📋 Waypoint configuration analysis completed with recommendations:\n")
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "✅ Waypoint configuration analysis completed successfully!\n")
		fmt.Fprintf(cmd.OutOrStdout(), "  ✨ All waypoints are properly configured and ready to use\n")
	}

	return result, nil
}

func simplifyPolicies(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepPolicyCleanup,
		Success: true,
	}

	// Get namespaces to analyze
	var namespaces []string
	if namespace != "" {
		namespaces = []string{namespace}
	} else if allNamespaces {
		nsList, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return result, fmt.Errorf("failed to list namespaces: %v", err)
		}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	} else {
		namespaces = []string{ctx.NamespaceOrDefault("default")}
	}

	var policiesToRemove []string

	for _, nsName := range namespaces {
		// Check if namespace is using waypoints
		ns, err := kubeClient.Kube().CoreV1().Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get namespace %s: %v", nsName, err))
			continue
		}

		hasWaypointLabel := false
		if ns.Labels != nil {
			if waypointName, exists := ns.Labels["istio.io/use-waypoint"]; exists && waypointName != "" {
				hasWaypointLabel = true
			}
		}

		if hasWaypointLabel {
			// Get authorization policies that might be replaced by waypoint policies
			authzPolicies, err := kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies(nsName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list authorization policies in namespace %s: %v", nsName, err))
				continue
			}

			for _, policy := range authzPolicies.Items {
				// Check if this is a sidecar-based policy that has been migrated
				// Sidecar-based policies are those WITHOUT the migrate.istio.io/source-policy label
				// AND that have L7 rules (which would need waypoint migration)
				if (policy.Labels == nil || policy.Labels["migrate.istio.io/source-policy"] == "") && hasL7Rules(policy) {
					// Check if there's a corresponding waypoint policy
					waypointPolicyName := policy.Name + "-waypoint"
					waypointPolicy, err := kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies(nsName).Get(context.Background(), waypointPolicyName, metav1.GetOptions{})
					if err == nil && waypointPolicy.Labels != nil && waypointPolicy.Labels["migrate.istio.io/source-policy"] != "" {
						// This original sidecar-based policy has been migrated to waypoint, so it can be removed
						policiesToRemove = append(policiesToRemove, fmt.Sprintf("%s/%s", nsName, policy.Name))
					}
				}
			}
		}
	}

	result.Message = fmt.Sprintf("Policy cleanup analysis found %d sidecar-based policies that can be safely removed", len(policiesToRemove))

	if len(policiesToRemove) > 0 {
		result.Recommendations = append(result.Recommendations,
			"The following sidecar-based authorization policies have been migrated and can be safely removed:")

		for _, policyName := range policiesToRemove {
			result.Recommendations = append(result.Recommendations,
				fmt.Sprintf("kubectl delete authorizationpolicies.security.istio.io -n %s %s",
					strings.Split(policyName, "/")[0], strings.Split(policyName, "/")[1]))
		}
	} else {
		result.StatusMessages = append(result.StatusMessages,
			"✅ No sidecar-based policies found that need to be removed")
	}

	return result, nil
}

func removeSidecars(ctx cli.Context, kubeClient kube.CLIClient, cmd *cobra.Command) (MigrationResult, error) {
	result := MigrationResult{
		Step:    StepSidecarRemoval,
		Success: true,
	}

	// Get namespaces to analyze
	var namespaces []string
	if namespace != "" {
		namespaces = []string{namespace}
	} else if allNamespaces {
		nsList, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return result, fmt.Errorf("failed to list namespaces: %v", err)
		}
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	} else {
		namespaces = []string{ctx.NamespaceOrDefault("default")}
	}

	var namespacesToMigrate []string
	var podsWithSidecars []string

	for _, nsName := range namespaces {
		// Check if namespace has sidecar injection enabled
		ns, err := kubeClient.Kube().CoreV1().Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to get namespace %s: %v", nsName, err))
			continue
		}

		hasSidecarInjection := false
		if ns.Labels != nil {
			if injection, exists := ns.Labels["istio-injection"]; exists && injection == "enabled" {
				hasSidecarInjection = true
			}
		}

		// Check if namespace is already in ambient mode
		isAmbient := false
		if ns.Labels != nil {
			if mode, exists := ns.Labels["istio.io/dataplane-mode"]; exists && mode == "ambient" {
				isAmbient = true
			}
		}

		if hasSidecarInjection && isAmbient {
			// Namespace has both sidecar injection and ambient mode - remove sidecar injection
			namespacesToMigrate = append(namespacesToMigrate, nsName)
		} else if hasSidecarInjection && !isAmbient {
			// Namespace has sidecar injection but not ambient mode - needs migration
			namespacesToMigrate = append(namespacesToMigrate, nsName)

			// Check for pods with sidecars
			pods, err := kubeClient.Kube().CoreV1().Pods(nsName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to list pods in namespace %s: %v", nsName, err))
				continue
			}

			for _, pod := range pods.Items {
				for _, container := range pod.Spec.Containers {
					if strings.Contains(container.Name, "istio-proxy") || strings.Contains(container.Name, "envoy") {
						podsWithSidecars = append(podsWithSidecars, fmt.Sprintf("%s/%s", nsName, pod.Name))
						break
					}
				}
			}
		} else if isAmbient {
			result.StatusMessages = append(result.StatusMessages,
				fmt.Sprintf("✅ Namespace %s is already in ambient mode", nsName))
		}
	}

	result.Message = fmt.Sprintf("Sidecar removal analysis found %d namespaces with sidecar injection that can be migrated to ambient", len(namespacesToMigrate))

	if len(namespacesToMigrate) > 0 {
		result.Recommendations = append(result.Recommendations,
			"The following namespaces can be safely migrated from sidecar to ambient mode:")

		for _, nsName := range namespacesToMigrate {
			result.Recommendations = append(result.Recommendations,
				fmt.Sprintf("kubectl label namespace %s istio-injection- istio.io/dataplane-mode=ambient", nsName))
		}

		if len(podsWithSidecars) > 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("⚠️ Sidecars detected in %d pods. These will be removed when namespaces are migrated to ambient mode.", len(podsWithSidecars)))
		}
	} else {
		result.StatusMessages = append(result.StatusMessages,
			"✅ No namespaces found that need sidecar removal")
	}

	return result, nil
}

func printStepResult(cmd *cobra.Command, result MigrationResult) {
	stepDesc := getStepDescription(result.Step)

	if !result.Success {
		fmt.Fprintf(cmd.ErrOrStderr(), "❌ %s failed!\n", stepDesc)
	} else if len(result.Warnings) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "⚠️ %s completed with warnings\n", stepDesc)
	} else if len(result.Recommendations) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "📋 %s completed with recommendations\n", stepDesc)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "✅ %s completed successfully\n", stepDesc)
	}

	if result.Message != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", result.Message)
	}

	for _, status := range result.StatusMessages {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", status)
	}

	for _, warning := range result.Warnings {
		fmt.Fprintf(cmd.OutOrStdout(), "  ⚠️ Warning: %s\n", warning)
	}

	for _, recommendation := range result.Recommendations {
		fmt.Fprintf(cmd.OutOrStdout(), "  💡 %s\n", recommendation)
	}

	for _, err := range result.Errors {
		fmt.Fprintf(cmd.ErrOrStderr(), "  ❌ Error: %s\n", err)
	}
}

func writeMigrationSummary(outputDir string, results []MigrationResult) error {
	filename := filepath.Join(outputDir, "migration-summary.yaml")

	data := map[string]interface{}{
		"phases":    results,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	yamlData, err := yaml.Marshal(data)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, yamlData, 0644)
}

// isVersionGreaterOrEqual checks if version1 >= version2 using the existing version package
func isVersionGreaterOrEqual(version1, version2 *version.Version) bool {
	// Compare major versions
	if version1.Major > version2.Major {
		return true
	}
	if version1.Major < version2.Major {
		return false
	}

	// Compare minor versions
	if version1.Minor > version2.Minor {
		return true
	}
	if version1.Minor < version2.Minor {
		return false
	}

	// Compare patch versions
	return version1.Patch >= version2.Patch
}

func checkCNICompatibility(kubeClient kube.CLIClient) (bool, error) {
	// Check if the cluster has a compatible CNI
	// Most CNIs are compatible with ambient mesh, but we should verify
	// that a CNI is actually installed and working

	// Check for Cilium first (has specific requirements)
	ciliumDaemonSets, err := kubeClient.Kube().AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{
		LabelSelector: "k8s-app=cilium",
	})
	if err == nil && len(ciliumDaemonSets.Items) > 0 {
		return checkCiliumCompatibility(kubeClient)
	}

	// Check for other common CNI DaemonSets
	cniLabels := []string{
		"k8s-app=flannel",
		"k8s-app=calico-node",
		"k8s-app=aws-node",
		"k8s-app=azure-npm",
		"k8s-app=gke-metadata-server",
		"k8s-app=weave-net",
	}

	for _, label := range cniLabels {
		daemonSets, err := kubeClient.Kube().AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{
			LabelSelector: label,
		})
		if err != nil {
			continue
		}
		if len(daemonSets.Items) > 0 {
			return true, nil
		}
	}

	// If no specific CNI found, check if any DaemonSets exist in kube-system
	// that might be CNI-related
	daemonSets, err := kubeClient.Kube().AppsV1().DaemonSets("kube-system").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	// If we have DaemonSets in kube-system, assume CNI is working
	return len(daemonSets.Items) > 0, nil
}

// checkCiliumCompatibility performs specific checks for Cilium CNI compatibility with Istio ambient mesh
func checkCiliumCompatibility(kubeClient kube.CLIClient) (bool, error) {
	// Check for cilium-config ConfigMap
	configMaps, err := kubeClient.Kube().CoreV1().ConfigMaps("").List(context.Background(), metav1.ListOptions{
		FieldSelector: "metadata.name=cilium-config",
	})
	if err != nil {
		return false, fmt.Errorf("failed to check Cilium configuration: %v", err)
	}

	if len(configMaps.Items) == 0 {
		return false, fmt.Errorf("cilium-config ConfigMap not found")
	}

	// Check the cilium-config ConfigMap for required settings
	ciliumConfig := configMaps.Items[0]
	hasRequiredConfig := false
	hasProblematicConfig := false

	// Check for cni-exclusive setting
	if cniExclusive, exists := ciliumConfig.Data["cni-exclusive"]; exists {
		if cniExclusive == "false" {
			hasRequiredConfig = true
		}
	}

	// Check for enable-bpf-masquerade setting
	if enableBPFMasquerade, exists := ciliumConfig.Data["enable-bpf-masquerade"]; exists {
		if enableBPFMasquerade == "true" {
			hasProblematicConfig = true
		}
	}

	// Return specific error messages based on configuration
	if hasProblematicConfig {
		return false, fmt.Errorf("cilium CNI detected with problematic configuration: enable-bpf-masquerade is set to true which causes issues with Istio ambient mesh health checks")
	}

	if !hasRequiredConfig {
		return false, fmt.Errorf("cilium CNI detected but not properly configured for Istio ambient mesh: cni-exclusive must be set to false to support CNI chaining")
	}

	return true, nil
}

func checkIstioVersionCompatibility(kubeClient kube.CLIClient, istioNamespace string) (bool, error) {
	// Check if Istio version supports ambient mesh
	// Ambient mesh was introduced in Istio 1.25+
	minAmbientVersion, err := version.NewVersionFromString("1.25.0")
	if err != nil {
		return false, fmt.Errorf("failed to parse minimum ambient version: %v", err)
	}

	pods, err := kubeClient.Kube().CoreV1().Pods(istioNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=istiod",
	})
	if err != nil {
		return false, err
	}

	if len(pods.Items) == 0 {
		return false, fmt.Errorf("no istiod pods found")
	}

	// Check the image version of istiod
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == "discovery" {
				image := container.Image
				if strings.Contains(image, ":") {
					versionStr := strings.Split(image, ":")[1]

					currentVersion, err := version.NewVersionFromString(versionStr)
					if err != nil {
						continue
					}

					if isVersionGreaterOrEqual(currentVersion, minAmbientVersion) {
						return true, nil
					}
				}
			}
		}
	}

	return false, fmt.Errorf("istio version does not support ambient mesh (requires 1.25+)")
}

func checkMulticlusterCompatibility(kubeClient kube.CLIClient) (bool, error) {
	// Check if multicluster setup is being used (NOT compatible with ambient mesh)
	// Look for multicluster-related resources

	// Check for remote secrets (multicluster setup)
	secrets, err := kubeClient.Kube().CoreV1().Secrets("").List(context.Background(), metav1.ListOptions{
		LabelSelector: "istio/multiCluster=true",
	})
	if err != nil {
		return false, err
	}

	// If remote secrets found, multicluster is configured and NOT compatible with ambient
	if len(secrets.Items) > 0 {
		return false, fmt.Errorf("multicluster mesh detected - multicluster is not supported in ambient mode. Please remove multicluster configuration before migrating to ambient mesh")
	}

	// Check for east-west gateways (indicates multicluster setup)
	services, err := kubeClient.Kube().CoreV1().Services("").List(context.Background(), metav1.ListOptions{
		LabelSelector: "istio=eastwestgateway",
	})
	if err != nil {
		return false, err
	}

	// If east-west gateways found, multicluster is configured
	if len(services.Items) > 0 {
		return false, fmt.Errorf("east-west gateways detected - multicluster mesh is not supported in ambient mode. Please remove multicluster configuration before migrating to ambient mesh")
	}

	// No multicluster setup found, so it's compatible
	return true, nil
}

func checkVMCompatibility(kubeClient kube.CLIClient) (bool, error) {
	// Check if Virtual Machine workloads are being used (NOT compatible with ambient mesh)
	// Look for WorkloadEntry resources (used for VMs)
	workloadEntries, err := kubeClient.Istio().NetworkingV1alpha3().WorkloadEntries("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		// If we can't list WorkloadEntries, no VMs are configured so it's compatible
		return true, nil
	}

	// If WorkloadEntries found, VM integration could be in use and NOT compatible with ambient
	if len(workloadEntries.Items) > 0 {
		vmNames := make([]string, 0, len(workloadEntries.Items))
		for _, we := range workloadEntries.Items {
			vmNames = append(vmNames, fmt.Sprintf("%s/%s", we.Namespace, we.Name))
		}
		return false, fmt.Errorf("workload entries detected(%d WorkloadEntries: %s) - This could mean VM integration is in use. Ambient mesh does not support VM integration", len(workloadEntries.Items), strings.Join(vmNames, ", "))
	}

	return true, nil
}

func checkSPIRECompatibility(kubeClient kube.CLIClient) (bool, error) {
	// Check for pods with SPIRE managed identity label across all namespaces
	// This is the most reliable way to detect SPIRE usage
	spireManagedPods, err := kubeClient.Kube().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
		LabelSelector: "spiffe.io/spire-managed-identity",
	})
	if err != nil {
		return false, err
	}

	// If pods with SPIRE managed identity found, SPIRE is being used and NOT compatible with ambient
	if len(spireManagedPods.Items) > 0 {
		// Get unique namespaces and pod counts for better error reporting
		namespaceCounts := make(map[string]int)
		for _, pod := range spireManagedPods.Items {
			namespaceCounts[pod.Namespace]++
		}

		var namespaceInfo []string
		for namespace, count := range namespaceCounts {
			namespaceInfo = append(namespaceInfo, fmt.Sprintf("%s (%d pods)", namespace, count))
		}

		return false, fmt.Errorf("SPIRE workload identity detected in namespaces: %s - SPIRE is not supported in ambient mode. Please remove SPIRE configuration before migrating to ambient mesh", strings.Join(namespaceInfo, ", "))
	}

	// No SPIRE managed pods found, so it's compatible
	return true, nil
}

func checkAmbientModeEnabled(kubeClient kube.CLIClient, istioNamespace string) (bool, error) {
	// Check if ambient mode is enabled in istiod
	pods, err := kubeClient.Kube().CoreV1().Pods(istioNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=istiod",
	})
	if err != nil {
		return false, err
	}

	if len(pods.Items) == 0 {
		return false, fmt.Errorf("no istiod pods found")
	}

	// Check if PILOT_ENABLE_AMBIENT is set to true
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if container.Name == "discovery" {
				for _, env := range container.Env {
					if env.Name == "PILOT_ENABLE_AMBIENT" && env.Value == "true" {
						return true, nil
					}
				}
			}
		}
	}

	istiodName := "istiod"
	if len(pods.Items) > 0 {
		istiodName = pods.Items[0].Name
	}

	return false, fmt.Errorf("istiod %s/%s must have 'PILOT_ENABLE_AMBIENT=true'. Upgrade Istio with '--set profile=ambient'", istioNamespace, istiodName)
}

func checkZtunnelDeployed(kubeClient kube.CLIClient) (bool, error) {
	// Check if ztunnel DaemonSet is deployed
	daemonSets, err := kubeClient.Kube().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
		LabelSelector: "app=ztunnel",
	})
	if err != nil {
		return false, err
	}

	if len(daemonSets.Items) == 0 {
		return false, fmt.Errorf("ztunnel not found")
	}

	return true, nil
}

func checkCNIAgentDeployed(kubeClient kube.CLIClient) (bool, error) {
	// Check if Istio CNI agent DaemonSet is deployed
	daemonSets, err := kubeClient.Kube().AppsV1().DaemonSets("").List(context.Background(), metav1.ListOptions{
		LabelSelector: "k8s-app=istio-cni-node",
	})
	if err != nil {
		return false, err
	}

	if len(daemonSets.Items) == 0 {
		return false, fmt.Errorf("istio CNI agent not found")
	}

	return true, nil
}

func checkSidecarsSupportAmbient(kubeClient kube.CLIClient) (bool, error) {
	// Check if existing sidecars support ambient mode
	// Look for pods with sidecar proxies and check for ISTIO_META_ENABLE_HBONE environment variable

	pods, err := kubeClient.Kube().CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return false, err
	}

	var incompatiblePods []string

	for _, pod := range pods.Items {
		hasSidecar := false
		hasHBONE := false
		isPodZtunnel := pod.Labels != nil && pod.Labels["app"] == "ztunnel"
		// Check if pod has sidecar container
		for _, container := range pod.Spec.Containers {
			if container.Name == "istio-proxy" && !isPodZtunnel {
				hasSidecar = true

				// Check for ENABLE_HBONE environment variable
				for _, env := range container.Env {
					if env.Name == "ISTIO_META_ENABLE_HBONE" {
						hasHBONE = true
						break
					}
				}
				break
			}
		}

		if hasSidecar && !hasHBONE {
			incompatiblePods = append(incompatiblePods, fmt.Sprintf("Sidecar %s/%s is missing 'ISTIO_META_ENABLE_HBONE'. Upgrade Istio with '--set profile=ambient' and restart the pod.", pod.Namespace, pod.Name))
		}
	}

	// If no incompatible sidecars found, return success
	if len(incompatiblePods) == 0 {
		return true, nil
	}

	// Return detailed error with all incompatible pods
	return false, fmt.Errorf("%d errors occurred:\n\t* %s", len(incompatiblePods), strings.Join(incompatiblePods, "\n\t* "))
}

func checkRequiredCRDsInstalled(kubeClient kube.CLIClient) (bool, error) {
	// Check if all required Gateway API CRDs are installed for ambient mesh
	// Gateway API CRDs are required for ambient mesh waypoints and traffic routing
	requiredGatewayCRDs := []string{
		"gateways.gateway.networking.k8s.io",
		"httproutes.gateway.networking.k8s.io",
		"referencegrants.gateway.networking.k8s.io",
		"grpcroutes.gateway.networking.k8s.io",
		"gatewayclasses.gateway.networking.k8s.io",
	}

	crdClient := kubeClient.Ext().ApiextensionsV1().CustomResourceDefinitions()

	var missingCRDs []string
	for _, crdName := range requiredGatewayCRDs {
		_, err := crdClient.Get(context.Background(), crdName, metav1.GetOptions{})
		if err != nil {
			missingCRDs = append(missingCRDs, crdName)
		}
	}

	if len(missingCRDs) > 0 {
		return false, fmt.Errorf("required Gateway API CRDs not found: %s. Please install Gateway API CRDs before migrating to ambient mesh", strings.Join(missingCRDs, ", "))
	}

	return true, nil
}

// Helper functions for the new deployWaypoints phase

func analyzeNamespacesForWaypoints(kubeClient kube.CLIClient) ([]NamespaceInfo, error) {
	// This function combines namespace analysis and service analysis
	// to determine which namespaces need waypoints
	var namespaces []NamespaceInfo

	// Get all namespaces
	nsList, err := kubeClient.Kube().CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, ns := range nsList.Items {
		// Skip system namespaces
		if ns.Name == "kube-system" || ns.Name == "istio-system" || ns.Name == "kube-public" {
			continue
		}

		nsInfo := NamespaceInfo{
			Name:          ns.Name,
			HasSidecars:   false,
			HasWaypoints:  false,
			NeedsWaypoint: false,
		}

		// Check for VirtualServices in this namespace
		vsList, err := kubeClient.Istio().NetworkingV1alpha3().VirtualServices(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			continue // Skip namespaces we can't analyze
		}

		// If there are VirtualServices, this namespace likely needs waypoints
		if len(vsList.Items) > 0 {
			nsInfo.NeedsWaypoint = true

			// Add services that depend on VirtualServices
			for _, vs := range vsList.Items {
				for _, http := range vs.Spec.Http {
					for _, route := range http.Route {
						if route.Destination != nil && route.Destination.Host != "" {
							serviceInfo := ServiceInfo{
								Name:               route.Destination.Host,
								Namespace:          ns.Name,
								VirtualServiceName: vs.Name,
								NeedsWaypoint:      true,
							}
							nsInfo.Services = append(nsInfo.Services, serviceInfo)
						}
					}
				}
			}
		}

		if nsInfo.NeedsWaypoint || len(nsInfo.Services) > 0 {
			namespaces = append(namespaces, nsInfo)
		}
	}

	return namespaces, nil
}

func analyzeServicesForWaypoints(kubeClient kube.CLIClient) ([]ServiceInfo, error) {
	// Analyze services that need waypoints due to authorization policies
	var services []ServiceInfo

	// Get all AuthorizationPolicies
	authzPolicies, err := kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies("").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Track services that need waypoints
	servicesNeedingWaypoints := make(map[string]bool)

	for _, policy := range authzPolicies.Items {
		// Check if policy has L7 (HTTP-based) rules (requires waypoint)
		if hasL7Rules(policy) && policy.Spec.Selector != nil && policy.Spec.Selector.MatchLabels != nil {
			// Find services that match this selector
			services, err := kubeClient.Kube().CoreV1().Services(policy.Namespace).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				continue
			}

			for _, svc := range services.Items {
				// Check if service matches the selector
				if labelsMatch(svc.Labels, policy.Spec.Selector.MatchLabels) {
					key := fmt.Sprintf("%s/%s", policy.Namespace, svc.Name)
					servicesNeedingWaypoints[key] = true
				}
			}
		}
	}

	// Convert to ServiceInfo slice
	for key := range servicesNeedingWaypoints {
		parts := strings.Split(key, "/")
		if len(parts) == 2 {
			serviceInfo := ServiceInfo{
				Name:          parts[1],
				Namespace:     parts[0],
				NeedsWaypoint: true,
			}
			services = append(services, serviceInfo)
		}
	}

	return services, nil
}

func labelsMatch(podLabels, selectorLabels map[string]string) bool {
	for key, value := range selectorLabels {
		if podLabels[key] != value {
			return false
		}
	}
	return true
}

func generateWaypointYAML(namespaces []NamespaceInfo, services []ServiceInfo) (string, error) {
	// Create waypoints for namespaces that need them
	namespacesNeedingWaypoints := make(map[string]bool)
	for _, ns := range namespaces {
		if ns.NeedsWaypoint {
			namespacesNeedingWaypoints[ns.Name] = true
		}
	}

	// Add namespaces from services that need waypoints
	for _, svc := range services {
		if svc.NeedsWaypoint {
			namespacesNeedingWaypoints[svc.Namespace] = true
		}
	}

	// Generate Gateway resources using proper structs
	var gatewayYAMLs []string
	for nsName := range namespacesNeedingWaypoints {
		// Create the complete Gateway resource
		gatewayResource := map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "waypoint",
				"namespace": nsName,
			},
			"spec": map[string]interface{}{
				"gatewayClassName": "istio-waypoint",
				"listeners": []map[string]interface{}{
					{
						"name":     "mesh",
						"port":     15008,
						"protocol": "HBONE",
					},
				},
			},
		}

		// Marshal to YAML
		yamlData, err := yaml.Marshal(gatewayResource)
		if err != nil {
			return "", err
		}

		gatewayYAMLs = append(gatewayYAMLs, string(yamlData))
	}

	// Join all Gateway YAMLs with document separators
	return strings.Join(gatewayYAMLs, "\n---\n"), nil
}

func getOutputDir() string {
	if outputDir != "" {
		return outputDir
	}
	return "/tmp/istio-migrate"
}

// analyzeNamespaceDetailed performs comprehensive namespace analysis
func analyzeNamespaceDetailed(kubeClient kube.CLIClient, nsName string) (DetailedNamespaceAnalysis, error) {
	analysis := DetailedNamespaceAnalysis{
		Name: nsName,
	}

	// Get namespace
	ns, err := kubeClient.Kube().CoreV1().Namespaces().Get(context.Background(), nsName, metav1.GetOptions{})
	if err != nil {
		return analysis, err
	}

	// Check if namespace has sidecar injection enabled
	if ns.Labels != nil {
		if injection, exists := ns.Labels["istio-injection"]; exists && injection == "enabled" {
			analysis.HasSidecars = true
		}
		// Check if namespace is already in ambient mode
		if mode, exists := ns.Labels[label.IoIstioDataplaneMode.Name]; exists && mode == constants.DataplaneModeAmbient {
			analysis.StatusMessages = append(analysis.StatusMessages,
				fmt.Sprintf("✅ Namespace %s is already in ambient mode", nsName))
		}
	}

	// Analyze services
	services, err := kubeClient.Kube().CoreV1().Services(nsName).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return analysis, err
	}

	for _, svc := range services.Items {
		serviceAnalysis := ServiceAnalysis{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		analysis.Services = append(analysis.Services, serviceAnalysis)
	}

	// Analyze authorization policies
	authzPolicies, err := kubeClient.Istio().SecurityV1beta1().AuthorizationPolicies(nsName).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return analysis, err
	}

	if len(authzPolicies.Items) > 0 {
		analysis.HasAuthorizationPolicies = true

		for _, policy := range authzPolicies.Items {
			policyAnalysis := AuthorizationPolicyAnalysis{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			}

			// Check if policy has HTTP rules using the hasL7Rules function
			if hasL7Rules(policy) {
				policyAnalysis.HasHTTPRules = true
				analysis.NeedsWaypoint = true
			}

			// Check target reference
			if policy.Spec.Selector != nil && len(policy.Spec.Selector.MatchLabels) > 0 {
				// This is a workload-level policy
				policyAnalysis.TargetRef = "workload"
			} else {
				// This is a namespace-level policy
				policyAnalysis.TargetRef = "namespace"
			}

			if policyAnalysis.HasHTTPRules {
				policyAnalysis.NeedsWaypoint = true
			}

			analysis.AuthorizationPolicies = append(analysis.AuthorizationPolicies, policyAnalysis)
		}
	}

	// Analyze pods
	pods, err := kubeClient.Kube().CoreV1().Pods(nsName).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return analysis, err
	}

	for _, pod := range pods.Items {
		podAnalysis := PodAnalysis{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		}

		// Check if pod has sidecar
		for _, container := range pod.Spec.Containers {
			if strings.Contains(container.Name, "istio-proxy") || strings.Contains(container.Name, "envoy") {
				podAnalysis.HasSidecar = true
				break
			}
		}

		// Check if pod is in ambient mode
		if pod.Annotations != nil {
			if redirection, exists := pod.Annotations[annotation.AmbientRedirection.Name]; exists && redirection == constants.AmbientRedirectionEnabled {
				podAnalysis.IsAmbient = true
			}
		}

		analysis.Pods = append(analysis.Pods, podAnalysis)
	}

	// Generate recommendations
	if analysis.NeedsWaypoint {
		analysis.Recommendations = append(analysis.Recommendations,
			fmt.Sprintf("Namespace %s needs a waypoint due to HTTP-based authorization policies", nsName))
	}

	if analysis.HasSidecars && !analysis.HasAuthorizationPolicies {
		analysis.Recommendations = append(analysis.Recommendations,
			fmt.Sprintf("Namespace %s has sidecars but no authorization policies - can be migrated directly to ambient", nsName))
	}

	return analysis, nil
}

// hasL7Rules checks if an AuthorizationPolicy has Layer 7 (HTTP) rules that require waypoints
func hasL7Rules(policy *securityv1beta1.AuthorizationPolicy) bool {
	if policy == nil {
		return false
	}
	for _, rule := range policy.Spec.Rules {
		// Check rule.To operations for L7 attributes
		for _, to := range rule.To {
			if to.Operation != nil {
				op := to.Operation
				// Check for L7 operation attributes
				if len(op.Hosts) > 0 || len(op.NotHosts) > 0 ||
					len(op.Methods) > 0 || len(op.NotMethods) > 0 ||
					len(op.Paths) > 0 || len(op.NotPaths) > 0 {
					return true
				}
			}
		}

		// Check rule.From sources for L7 attributes
		for _, from := range rule.From {
			if from.Source != nil {
				src := from.Source
				// Check for L7 source attributes
				if len(src.RemoteIpBlocks) > 0 || len(src.NotRemoteIpBlocks) > 0 ||
					len(src.RequestPrincipals) > 0 || len(src.NotRequestPrincipals) > 0 {
					return true
				}
			}
		}

		// Check rule.When conditions for L7 attributes
		for _, when := range rule.When {
			// L4 attributes that do NOT require waypoints
			l4Attributes := map[string]bool{
				"source.ip":        true,
				"source.namespace": true,
				"source.principal": true,
				"destination.ip":   true,
				"destination.port": true,
			}

			// If the when key is NOT in L4 attributes, it's L7 and requires waypoint
			if !l4Attributes[when.Key] {
				return true
			}
		}
	}
	return false
}
