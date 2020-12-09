package cmd

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"istio.io/istio/pkg/kube"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

const IstioTagLabel = "istio.io/tag"

var (
	fromRevision = false
	fromTag      = false
)

func tagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tag",
		Short:   "Command group used to interact with revision-tags",
		Example: "istioctl tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	cmd.AddCommand(tagApplyCommand())
	cmd.AddCommand(tagRemoveCommand())
	cmd.AddCommand(tagListCommand())

	return cmd
}

func tagApplyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apply",
		Short:   "Create or redirect an existing revision tag",
		Example: "istioctl tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	cmd.PersistentFlags().BoolVarP(&fromRevision, "revision", "r", false, "")
	cmd.PersistentFlags().BoolVarP(&fromTag, "tag", "t", false, "")

	return cmd
}

func tagListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List existing revision tags and their corresponding revisions",
		Example: "istioctl tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	return cmd
}

func tagRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an existing revision tag",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("must provide a tag for removal")
			}
			if len(args) > 1 {
				return fmt.Errorf("must provide a single tag for removal")
			}

			ctx := context.Background()
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %v", err)
			}

			return removeTag(ctx, client, args[0])
		},
	}

	return cmd
}

// applyTag creates or redirects a revision tag
func applyTag() {
	panic("not implemented")
}

// removeTag removes an existing revision tag
func removeTag(ctx context.Context, kubeClient kube.ExtendedClient, tag string) error {
	webhooks, err := getWebhooksWithTag(ctx, kubeClient, tag)
	if err != nil {
		return fmt.Errorf("failed to retrieve tag with name %s: %v", tag, err)
	}
	if len(webhooks) == 0 {
		return fmt.Errorf("revision tag %s does not exist", tag)
	}

	taggedNamespaces, err := getNamespacesWithTag(ctx, kubeClient, tag)
	if err != nil {
		return fmt.Errorf("could not retrieve namespaces dependent on tag: %s", tag)
	}
	// warn user if deleting a tag that still has namespaces pointed to it
	if len(taggedNamespaces) > 0 {
		fmt.Println(buildDeleteTagConfirmation(tag, taggedNamespaces))
	}

	// proceed with webhook deletion
	err = deleteTagWebhooks(ctx, kubeClient, webhooks)
	if err != nil {
		return err
	}
	return nil
}

// listTags lists existing revision
func listTags() {
	panic("not implemented")
}

// getTagWebhooks returns all webhooks tagged with istio.io/tag
func getTagWebhooks(ctx context.Context, client kube.ExtendedClient) ([]admit_v1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// getWebhooksWithTag returns webhooks tagged with istio.io/tag=<tag>
func getWebhooksWithTag(ctx context.Context, client kube.ExtendedClient, tag string) ([]admit_v1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", IstioTagLabel, tag),
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// deleteTagWebhooks deletes the given webhooks
func deleteTagWebhooks(ctx context.Context, client kube.ExtendedClient, webhooks []admit_v1.MutatingWebhookConfiguration) error {
	var result error
	for _, wh := range webhooks {
		result = multierror.Append(client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, wh.Name, metav1.DeleteOptions{})).ErrorOrNil()
	}
	return result
}

// namespacesPointedToTag
func getNamespacesWithTag(ctx context.Context, client kube.ExtendedClient, tag string) ([]corev1.Namespace, error) {
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("istio.io/rev=%s", tag),
	})
	if err != nil {
		return nil, err
	}

	return namespaces.Items, nil
}

// buildDeleteTagConfirmation takes a list of webhooks and creates a message prompting confirmation for their deletion
func buildDeleteTagConfirmation(tag string, taggedNamespaces []corev1.Namespace) string {
	var sb strings.Builder
	base := fmt.Sprintf("Caution, found %d namespaces still pointing to tag \"%s\":", len(taggedNamespaces), tag)
	sb.WriteString(base)
	for _, ns := range taggedNamespaces {
		sb.WriteString(" " + ns.Name)
	}
	sb.WriteString("\nProceed with deletion? (y/N)")

	return sb.String()
}
