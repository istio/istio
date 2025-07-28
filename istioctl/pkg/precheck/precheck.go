// Copyright © 2021 NAME HERE <EMAIL ADDRESS>
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

package precheck

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	authorizationapi "k8s.io/api/authorization/v1"
	crd "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/install/k8sversion"
	"istio.io/istio/istioctl/pkg/util/formatting"
	istiocluster "istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/pkg/config/analysis/diag"
	legacykube "istio.io/istio/pkg/config/analysis/legacy/source/kube"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/url"
	"istio.io/istio/pkg/util/sets"
)

func Cmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	var skipControlPlane bool
	outputThreshold := formatting.MessageThreshold{Level: diag.Warning}
	var msgOutputFormat string
	var fromCompatibilityVersion string
	// cmd represents the upgradeCheck command
	cmd := &cobra.Command{
		Use:   "precheck",
		Short: "Check whether Istio can safely be installed or upgraded",
		Long:  `precheck inspects a Kubernetes cluster for Istio install and upgrade requirements.`,
		Example: `  # Verify that Istio can be installed or upgraded
  istioctl x precheck

  # Check only a single namespace
  istioctl x precheck --namespace default

  # Check for behavioral changes since a specific version
  istioctl x precheck --from-version 1.10`,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			msgs := diag.Messages{}
			if !skipControlPlane {
				msgs, err = checkControlPlane(ctx)
				if err != nil {
					return err
				}
			}

			if fromCompatibilityVersion != "" {
				m, err := checkFromVersion(ctx, opts.Revision, fromCompatibilityVersion)
				if err != nil {
					return err
				}
				msgs = append(msgs, m...)
			}

			// Print all the messages to stdout in the specified format
			msgs = msgs.SortedDedupedCopy()
			outputMsgs := diag.Messages{}
			for _, m := range msgs {
				if m.Type.Level().IsWorseThanOrEqualTo(outputThreshold.Level) {
					outputMsgs = append(outputMsgs, m)
				}
			}
			output, err := formatting.Print(outputMsgs, msgOutputFormat, true)
			if err != nil {
				return err
			}

			if len(outputMsgs) == 0 {
				message := " No issues found when checking the cluster. Istio is safe to install or upgrade!"
				message += "\n  To get started, check out https://istio.io/latest/docs/setup/getting-started/."
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), color.New(color.FgGreen).Sprint("✔")+message)
			} else {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), output)
			}
			for _, m := range msgs {
				if m.Type.Level().IsWorseThanOrEqualTo(diag.Warning) {
					e := fmt.Sprintf(`Issues found when checking the cluster. Istio may not be safe to install or upgrade.
See %s for more information about causes and resolutions.`, url.ConfigAnalysis)
					return errors.New(e)
				}
			}
			return nil
		},
	}
	cmd.PersistentFlags().BoolVar(&skipControlPlane, "skip-controlplane", false, "skip checking the control plane")
	cmd.PersistentFlags().Var(&outputThreshold, "output-threshold",
		fmt.Sprintf("The severity level of precheck at which to display messages. Valid values: %v", diag.GetAllLevelStrings()))
	cmd.PersistentFlags().StringVarP(&msgOutputFormat, "output", "o", formatting.LogFormat,
		fmt.Sprintf("Output format: one of %v", formatting.MsgOutputFormatKeys))
	cmd.PersistentFlags().StringVarP(&fromCompatibilityVersion, "from-version", "f", "",
		"check changes since the provided version")
	opts.AttachControlPlaneFlags(cmd)
	return cmd
}

func checkFromVersion(ctx cli.Context, revision, version string) (diag.Messages, error) {
	cli, err := ctx.CLIClientWithRevision(ctx.RevisionOrDefault(revision))
	if err != nil {
		return nil, err
	}
	major, minors, ok := strings.Cut(version, ".")
	if !ok {
		return nil, fmt.Errorf("invalid version %v, expected format like '1.0'", version)
	}
	if major != "1" {
		return nil, fmt.Errorf("expected major version 1, got %v", version)
	}
	minor, err := strconv.Atoi(minors)
	if err != nil {
		return nil, fmt.Errorf("minor version is not a number: %v", minors)
	}

	var messages diag.Messages = make([]diag.Message, 0)

	if minor <= 21 {
		if err := checkTracing(cli, &messages); err != nil {
			return nil, err
		}
	}
	return messages, nil
}

func checkTracing(cli kube.CLIClient, messages *diag.Messages) error {
	// In 1.22, we remove the default tracing config which points to zipkin.istio-system
	// This has no effect for users, unless they have this service.
	svc, err := cli.Kube().CoreV1().Services("istio-system").Get(context.Background(), "zipkin", metav1.GetOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		return err
	}
	if err != nil {
		// not found
		return nil
	}
	// found
	res := ObjectToInstance(svc)
	messages.Add(msg.NewUpdateIncompatibility(res,
		"meshConfig.defaultConfig.tracer", "1.21",
		"tracing is no longer by default enabled to send to 'zipkin.istio-system.svc'; "+
			"follow https://istio.io/latest/docs/tasks/observability/distributed-tracing/telemetry-api/",
		"1.21"))
	return nil
}

func ObjectToInstance(c controllers.Object) *resource.Instance {
	return &resource.Instance{
		Origin: &legacykube.Origin{
			Type: kubetypes.GvkFromObject(c),
			FullName: resource.FullName{
				Namespace: resource.Namespace(c.GetNamespace()),
				Name:      resource.LocalName(c.GetName()),
			},
			ResourceVersion: resource.Version(c.GetResourceVersion()),
			Ref:             nil,
			FieldsMap:       nil,
		},
	}
}

func checkControlPlane(ctx cli.Context) (diag.Messages, error) {
	cli, err := ctx.CLIClient()
	if err != nil {
		return nil, err
	}
	msgs := diag.Messages{}

	m, err := checkServerVersion(cli)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, m...)

	msgs = append(msgs, checkInstallPermissions(cli, ctx.IstioNamespace())...)
	gwMsg, err := checkGatewayAPIs(cli)
	if err != nil {
		return nil, err
	}
	msgs = append(msgs, gwMsg...)

	// TODO: add more checks

	sa := local.NewSourceAnalyzer(
		analysis.Combine("upgrade precheck", &maturity.AlphaAnalyzer{}),
		resource.Namespace(ctx.Namespace()),
		resource.Namespace(ctx.IstioNamespace()),
		nil,
	)
	if err != nil {
		return nil, err
	}
	sa.AddRunningKubeSource(cli)
	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if err != nil {
		return nil, err
	}
	if result.Messages != nil {
		msgs = append(msgs, result.Messages...)
	}

	return msgs, nil
}

// Checks that if the user has gateway APIs, they are the minimum version.
// It is ok to not have them, but they must be at least v1beta1 if they do.
func checkGatewayAPIs(cli kube.CLIClient) (diag.Messages, error) {
	msgs := diag.Messages{}
	res, err := cli.Ext().ApiextensionsV1().CustomResourceDefinitions().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	stableKinds := sets.New(gvk.KubernetesGateway.Kind, gvk.GatewayClass.Kind, gvk.HTTPRoute.Kind, gvk.GRPCRoute.Kind)
	stableVersion := "v1"
	betaKinds := sets.New(gvk.KubernetesGateway.Kind, gvk.GatewayClass.Kind, gvk.HTTPRoute.Kind, gvk.ReferenceGrant.Kind)
	betaVersion := "v1beta1"
	for _, r := range res.Items {
		if r.Spec.Group != gvk.KubernetesGateway.Group {
			continue
		}

		versions := extractCRDVersions(&r)
		has := "none"
		if len(versions) > 0 {
			has = strings.Join(sets.SortedList(versions), ",")
		}

		origin := legacykube.Origin{
			Type: gvk.CustomResourceDefinition,
			FullName: resource.FullName{
				Namespace: resource.Namespace(r.Namespace),
				Name:      resource.LocalName(r.Name),
			},
			ResourceVersion: resource.Version(r.ResourceVersion),
		}
		ri := &resource.Instance{
			Origin: &origin,
		}
		if betaKinds.Contains(r.Spec.Names.Kind) && !versions.Contains(betaVersion) {
			msgs.Add(msg.NewUnsupportedGatewayAPIVersion(ri, has, betaVersion))
		}
		if stableKinds.Contains(r.Spec.Names.Kind) && !versions.Contains(stableVersion) {
			msgs.Add(msg.NewFutureUnsupportedGatewayAPIVersion(ri, has, stableVersion))
		}
	}
	return msgs, nil
}

func extractCRDVersions(r *crd.CustomResourceDefinition) sets.String {
	res := sets.New[string]()
	for _, v := range r.Spec.Versions {
		if v.Served {
			res.Insert(v.Name)
		}
	}
	return res
}

func checkInstallPermissions(cli kube.CLIClient, istioNamespace string) diag.Messages {
	Resources := []struct {
		namespace string
		group     string
		version   string
		resource  string
	}{
		{
			version:  "v1",
			resource: "namespaces",
		},
		{
			group:    "rbac.authorization.k8s.io",
			version:  "v1",
			resource: "clusterroles",
		},
		{
			group:    "rbac.authorization.k8s.io",
			version:  "v1",
			resource: "clusterrolebindings",
		},
		{
			group:    "apiextensions.k8s.io",
			version:  "v1",
			resource: "customresourcedefinitions",
		},
		{
			namespace: istioNamespace,
			group:     "rbac.authorization.k8s.io",
			version:   "v1",
			resource:  "roles",
		},
		{
			namespace: istioNamespace,
			version:   "v1",
			resource:  "serviceaccounts",
		},
		{
			namespace: istioNamespace,
			version:   "v1",
			resource:  "services",
		},
		{
			namespace: istioNamespace,
			group:     "apps",
			version:   "v1",
			resource:  "deployments",
		},
		{
			namespace: istioNamespace,
			version:   "v1",
			resource:  "configmaps",
		},
		{
			group:    "admissionregistration.k8s.io",
			version:  "v1",
			resource: "mutatingwebhookconfigurations",
		},
		{
			group:    "admissionregistration.k8s.io",
			version:  "v1",
			resource: "validatingwebhookconfigurations",
		},
	}
	msgs := diag.Messages{}
	for _, r := range Resources {
		err := checkCanCreateResources(cli, r.namespace, r.group, r.version, r.resource)
		if err != nil {
			msgs.Add(msg.NewInsufficientPermissions(&resource.Instance{Origin: clusterOrigin{}}, r.resource, err.Error()))
		}
	}
	return msgs
}

func checkCanCreateResources(c kube.CLIClient, namespace, group, version, resource string) error {
	s := &authorizationapi.SelfSubjectAccessReview{
		Spec: authorizationapi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     group,
				Version:   version,
				Resource:  resource,
			},
		},
	}

	response, err := c.Kube().AuthorizationV1().SelfSubjectAccessReviews().Create(context.Background(), s, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	if !response.Status.Allowed {
		if len(response.Status.Reason) > 0 {
			return errors.New(response.Status.Reason)
		}
		return errors.New("permission denied")
	}
	return nil
}

func checkServerVersion(cli kube.CLIClient) (diag.Messages, error) {
	v, err := cli.GetKubernetesVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get the Kubernetes version: %v", err)
	}
	compatible, err := k8sversion.CheckKubernetesVersion(v)
	if err != nil {
		return nil, err
	}
	if !compatible {
		return []diag.Message{
			msg.NewUnsupportedKubernetesVersion(&resource.Instance{Origin: clusterOrigin{}}, v.String(), fmt.Sprintf("1.%d", k8sversion.MinK8SVersion)),
		}, nil
	}
	return nil, nil
}

// clusterOrigin defines an Origin that refers to the cluster
type clusterOrigin struct{}

func (o clusterOrigin) ClusterName() istiocluster.ID {
	return "Cluster"
}

func (o clusterOrigin) String() string {
	return ""
}

func (o clusterOrigin) FriendlyName() string {
	return "Cluster"
}

func (o clusterOrigin) Comparator() string {
	return o.FriendlyName()
}

func (o clusterOrigin) Namespace() resource.Namespace {
	return ""
}

func (o clusterOrigin) Reference() resource.Reference {
	return nil
}

func (o clusterOrigin) FieldMap() map[string]int {
	return make(map[string]int)
}
