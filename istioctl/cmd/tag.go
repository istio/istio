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

package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/istioctl/pkg/util/formatting"
	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/webhook"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/kube"
)

const (

	// help strings and long formatted user outputs
	skipConfirmationFlagHelpStr = `The skipConfirmation determines whether the user is prompted for confirmation.
If set to true, the user is not prompted and a Yes response is assumed in all cases.`
	overrideHelpStr = `If true, allow revision tags to be overwritten, otherwise reject revision tag updates that
overwrite existing revision tags.`
	revisionHelpStr = "Control plane revision to reference from a given revision tag"
	tagCreatedStr   = `Revision tag %q created, referencing control plane revision %q. To enable injection using this
revision tag, use 'kubectl label namespace <NAMESPACE> istio.io/rev=%s'
`
	webhookNameHelpStr          = "Name to use for a revision tag's mutating webhook configuration."
	autoInjectNamespacesHelpStr = "If set to true, the sidecars should be automatically injected into all namespaces by default"
)

// options for CLI
var (
	// revision to point tag webhook at
	revision             = ""
	manifestsPath        = ""
	overwrite            = false
	skipConfirmation     = false
	webhookName          = ""
	autoInjectNamespaces = false
)

type tagDescription struct {
	Tag        string   `json:"tag"`
	Revision   string   `json:"revision"`
	Namespaces []string `json:"namespaces"`
}

func tagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Command group used to interact with revision tags",
		Long: `Command group used to interact with revision tags. Revision tags allow for the creation of mutable aliases
referring to control plane revisions for sidecar injection.

With revision tags, rather than relabeling a namespace from "istio.io/rev=revision-a" to "istio.io/rev=revision-b" to
change which control plane revision handles injection, it's possible to create a revision tag "prod" and label our
namespace "istio.io/rev=prod". The "prod" revision tag could point to "1-7-6" initially and then be changed to point to "1-8-1"
at some later point.

This allows operators to change which Istio control plane revision should handle injection for a namespace or set of namespaces
without manual relabeling of the "istio.io/rev" tag.
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	cmd.AddCommand(tagSetCommand())
	cmd.AddCommand(tagGenerateCommand())
	cmd.AddCommand(tagListCommand())
	cmd.AddCommand(tagRemoveCommand())

	return cmd
}

func tagSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <revision-tag>",
		Short: "Create or modify revision tags",
		Long: `Create or modify revision tags. Tag an Istio control plane revision for use with namespace istio.io/rev
injection labels.`,
		Example: ` # Create a revision tag from the "1-8-0" revision
 istioctl tag set prod --revision 1-8-0

 # Point namespace "test-ns" at the revision pointed to by the "prod" revision tag
 kubectl label ns test-ns istio.io/rev=prod

 # Change the revision tag to reference the "1-8-1" revision
 istioctl tag set prod --revision 1-8-1 --overwrite

 # Make revision "1-8-1" the default revision, both resulting in that revision handling injection for "istio-injection=enabled"
 # and validating resources cluster-wide
 istioctl tag set default --revision 1-8-1

 # Rollout namespace "test-ns" to update workloads to the "1-8-1" revision
 kubectl rollout restart deployments -n test-ns
`,
		SuggestFor: []string{"create"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("must provide a tag for modification")
			}
			if len(args) > 1 {
				return fmt.Errorf("must provide a single tag for creation")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}

			return setTag(context.Background(), client, args[0], revision, istioNamespace, false, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, overrideHelpStr)
	cmd.PersistentFlags().StringVarP(&manifestsPath, "manifests", "d", "", mesh.ManifestsFlagHelpStr)
	cmd.PersistentFlags().BoolVarP(&skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&revision, "revision", "r", "", revisionHelpStr)
	cmd.PersistentFlags().StringVarP(&webhookName, "webhook-name", "", "", webhookNameHelpStr)
	cmd.PersistentFlags().BoolVar(&autoInjectNamespaces, "auto-inject-namespaces", false, autoInjectNamespacesHelpStr)
	_ = cmd.MarkPersistentFlagRequired("revision")

	return cmd
}

func tagGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate <revision-tag>",
		Short: "Generate configuration for a revision tag to stdout",
		Long: `Create a revision tag and output to the command's stdout. Tag an Istio control plane revision for use with namespace istio.io/rev
injection labels.`,
		Example: ` # Create a revision tag from the "1-8-0" revision
 istioctl tag generate prod --revision 1-8-0 > tag.yaml

 # Apply the tag to cluster
 kubectl apply -f tag.yaml

 # Point namespace "test-ns" at the revision pointed to by the "prod" revision tag
 kubectl label ns test-ns istio.io/rev=prod

 # Rollout namespace "test-ns" to update workloads to the "1-8-0" revision
 kubectl rollout restart deployments -n test-ns
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("must provide a tag for modification")
			}
			if len(args) > 1 {
				return fmt.Errorf("must provide a single tag for creation")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}

			return setTag(context.Background(), client, args[0], revision, istioNamespace, true, cmd.OutOrStdout(), cmd.OutOrStderr())
		},
	}

	cmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, overrideHelpStr)
	cmd.PersistentFlags().StringVarP(&manifestsPath, "manifests", "d", "", mesh.ManifestsFlagHelpStr)
	cmd.PersistentFlags().BoolVarP(&skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	cmd.PersistentFlags().StringVarP(&revision, "revision", "r", "", revisionHelpStr)
	cmd.PersistentFlags().StringVarP(&webhookName, "webhook-name", "", "", webhookNameHelpStr)
	cmd.PersistentFlags().BoolVar(&autoInjectNamespaces, "auto-inject-namespaces", false, autoInjectNamespacesHelpStr)
	_ = cmd.MarkPersistentFlagRequired("revision")

	return cmd
}

func tagListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List existing revision tags",
		Example: "istioctl tag list",
		Aliases: []string{"show"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("tag list command does not accept arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}
			return listTags(context.Background(), client.Kube(), cmd.OutOrStdout())
		},
	}

	return cmd
}

func tagRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <revision-tag>",
		Short: "Remove Istio control plane revision tag",
		Long: `Remove Istio control plane revision tag.

Removing a revision tag should be done with care. Removing a revision tag will disrupt sidecar injection in namespaces
that reference the tag in an "istio.io/rev" label. Verify that there are no remaining namespaces referencing a
revision tag before removing using the "istioctl tag list" command.
`,
		Example: ` # Remove the revision tag "prod"
	istioctl tag remove prod
`,
		Aliases: []string{"delete"},
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("must provide a tag for removal")
			}
			if len(args) > 1 {
				return fmt.Errorf("must provide a single tag for removal")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %v", err)
			}

			return removeTag(context.Background(), client.Kube(), args[0], skipConfirmation, cmd.OutOrStdout())
		},
	}

	cmd.PersistentFlags().BoolVarP(&skipConfirmation, "skip-confirmation", "y", false, skipConfirmationFlagHelpStr)
	return cmd
}

// setTag creates or modifies a revision tag.
func setTag(ctx context.Context, kubeClient kube.ExtendedClient, tagName, revision, istioNS string, generate bool, w, stderr io.Writer) error {
	opts := &tag.GenerateOptions{
		Tag:                  tagName,
		Revision:             revision,
		WebhookName:          webhookName,
		ManifestsPath:        manifestsPath,
		Generate:             generate,
		Overwrite:            overwrite,
		AutoInjectNamespaces: autoInjectNamespaces,
	}
	tagWhYAML, err := tag.Generate(ctx, kubeClient, opts, istioNS)
	if err != nil {
		return err
	}
	// Check the newly generated webhook does not conflict with existing ones.
	resName := webhookName
	if resName == "" {
		resName = fmt.Sprintf("%s-%s", "istio-revision-tag", tagName)
	}
	if err := analyzeWebhook(resName, tagWhYAML, revision, kubeClient.RESTConfig()); err != nil {
		// if we have a conflict, we will fail. If --skip-confirmation is set, we will continue with a
		// warning; when actually applying we will also confirm to ensure the user does not see the
		// warning *after* it has applied
		if !skipConfirmation {
			_, _ = stderr.Write([]byte(err.Error()))
			if !generate {
				if !confirm("Apply anyways? [y/N]", w) {
					return nil
				}
			}
		}
	}

	if generate {
		_, err := w.Write([]byte(tagWhYAML))
		if err != nil {
			return err
		}
		return nil
	}

	if err := tag.Create(kubeClient, tagWhYAML); err != nil {
		return fmt.Errorf("failed to apply tag webhook MutatingWebhookConfiguration to cluster: %v", err)
	}
	fmt.Fprintf(w, tagCreatedStr, tagName, revision, tagName)
	return nil
}

func analyzeWebhook(name, wh, revision string, config *rest.Config) error {
	sa := local.NewSourceAnalyzer(analysis.Combine("webhook", &webhook.Analyzer{}),
		resource.Namespace(selectedNamespace), resource.Namespace(istioNamespace), nil, true, analysisTimeout)
	if err := sa.AddReaderKubeSource([]local.ReaderSource{{Name: "", Reader: strings.NewReader(wh)}}); err != nil {
		return err
	}
	k, err := kube.NewClient(kube.NewClientConfigForRestConfig(config))
	if err != nil {
		return err
	}
	sa.AddRunningKubeSourceWithRevision(k, revision)
	res, err := sa.Analyze(make(chan struct{}))
	if err != nil {
		return err
	}
	relevantMessages := diag.Messages{}
	for _, msg := range res.Messages.FilterOutLowerThan(diag.Error) {
		if msg.Resource.Metadata.FullName.Name == resource.LocalName(name) {
			relevantMessages = append(relevantMessages, msg)
		}
	}
	if len(relevantMessages) > 0 {
		o, err := formatting.Print(relevantMessages, formatting.LogFormat, colorize)
		if err != nil {
			return err
		}
		// nolint
		return fmt.Errorf("creating tag would conflict, pass --skip-confirmation to proceed:\n%v\n", o)
	}
	return nil
}

// removeTag removes an existing revision tag.
func removeTag(ctx context.Context, kubeClient kubernetes.Interface, tagName string, skipConfirmation bool, w io.Writer) error {
	webhooks, err := tag.GetWebhooksWithTag(ctx, kubeClient, tagName)
	if err != nil {
		return fmt.Errorf("failed to retrieve tag with name %s: %v", tagName, err)
	}
	if len(webhooks) == 0 {
		return fmt.Errorf("cannot remove tag %q: cannot find MutatingWebhookConfiguration for tag", tagName)
	}

	taggedNamespaces, err := tag.GetNamespacesWithTag(ctx, kubeClient, tagName)
	if err != nil {
		return fmt.Errorf("failed to retrieve namespaces dependent on tag %q", tagName)
	}
	// warn user if deleting a tag that still has namespaces pointed to it
	if len(taggedNamespaces) > 0 && !skipConfirmation {
		if !confirm(buildDeleteTagConfirmation(tagName, taggedNamespaces), w) {
			fmt.Fprintf(w, "Aborting operation.\n")
			return nil
		}
	}

	// proceed with webhook deletion
	err = tag.DeleteTagWebhooks(ctx, kubeClient, tagName)
	if err != nil {
		return fmt.Errorf("failed to delete Istio revision tag MutatingConfigurationWebhook: %v", err)
	}

	fmt.Fprintf(w, "Revision tag %s removed\n", tagName)
	return nil
}

// listTags lists existing revision.
func listTags(ctx context.Context, kubeClient kubernetes.Interface, writer io.Writer) error {
	tagWebhooks, err := tag.GetTagWebhooks(ctx, kubeClient)
	if err != nil {
		return fmt.Errorf("failed to retrieve revision tags: %v", err)
	}
	if len(tagWebhooks) == 0 {
		fmt.Fprintf(writer, "No Istio revision tag MutatingWebhookConfigurations to list\n")
		return nil
	}
	tags := make([]tagDescription, 0)
	for _, wh := range tagWebhooks {
		tagName, err := tag.GetWebhookTagName(wh)
		if err != nil {
			return fmt.Errorf("error parsing tag name from webhook %q: %v", wh.Name, err)
		}
		tagRevision, err := tag.GetWebhookRevision(wh)
		if err != nil {
			return fmt.Errorf("error parsing revision from webhook %q: %v", wh.Name, err)
		}
		tagNamespaces, err := tag.GetNamespacesWithTag(ctx, kubeClient, tagName)
		if err != nil {
			return fmt.Errorf("error retrieving namespaces for tag %q: %v", tagName, err)
		}
		tagDesc := tagDescription{
			Tag:        tagName,
			Revision:   tagRevision,
			Namespaces: tagNamespaces,
		}
		tags = append(tags, tagDesc)
	}

	switch revArgs.output {
	case jsonFormat:
		return printJSON(writer, tags)
	case tableFormat:
	default:
		return fmt.Errorf("unknown format: %s", revArgs.output)
	}
	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "TAG\tREVISION\tNAMESPACES")
	for _, t := range tags {
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.Tag, t.Revision, strings.Join(t.Namespaces, ","))
	}

	return w.Flush()
}

// buildDeleteTagConfirmation takes a list of webhooks and creates a message prompting confirmation for their deletion.
func buildDeleteTagConfirmation(tag string, taggedNamespaces []string) string {
	var sb strings.Builder
	base := fmt.Sprintf("Caution, found %d namespace(s) still injected by tag %q:", len(taggedNamespaces), tag)
	sb.WriteString(base)
	for _, ns := range taggedNamespaces {
		sb.WriteString(" " + ns)
	}
	sb.WriteString("\nProceed with operation? [y/N]")

	return sb.String()
}

// confirm waits for a user to confirm with the supplied message.
func confirm(msg string, w io.Writer) bool {
	fmt.Fprintf(w, "%s ", msg)

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false
	}
	response = strings.ToUpper(response)
	return response == "Y" || response == "YES"
}
