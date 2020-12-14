package cmd

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/spf13/cobra"
	"io"
	admit_v1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"text/tabwriter"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
)

const istioTagLabel = "istio.io/tag"

var (
	revision  = ""
	overwrite = false
)

func tagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Command group used to interact with revision-tags",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	cmd.AddCommand(tagApplyCommand())
	cmd.AddCommand(tagListCommand())
	cmd.AddCommand(tagRemoveCommand())

	return cmd
}

func tagApplyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apply",
		Short:   "Create or modify revision tags",
		Example: "istioctl x tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("must provide a tag for creation")
			}
			if len(args) > 1 {
				return fmt.Errorf("must provide a single tag for creation")
			}

			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %v", err)
			}

			return applyTag(context.Background(), client, args[0])
		},
	}

	cmd.PersistentFlags().BoolVar(&overwrite, "overwrite", false, "whether to overwrite an existing tag")
	cmd.PersistentFlags().StringVarP(&revision, "revision", "r", "", "revision to point tag to")
	cmd.MarkPersistentFlagRequired("revision")

	return cmd
}

func tagListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List existing revision tags and their corresponding revisions",
		Example: "istioctl x tag list",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("tag list command does not accept arguments")
			}

			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %v", err)
			}

			return listTags(context.Background(), client, cmd.OutOrStdout())
		},
	}

	return cmd
}

func tagRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Short:   "Remove an existing revision tag",
		Example: "istioctl x tag remove prod",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("must provide a tag for removal")
			}
			if len(args) > 1 {
				return fmt.Errorf("must provide a single tag for removal")
			}

			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %v", err)
			}

			return removeTag(context.Background(), client, args[0])
		},
	}

	return cmd
}

// applyTag creates or modifies a revision tag
func applyTag(ctx context.Context, kubeClient kube.ExtendedClient, tag string) error {
	return nil
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
func listTags(ctx context.Context, kubeClient kube.ExtendedClient, writer io.Writer) error {
	tagWebhooks, err := getTagWebhooks(ctx, kubeClient)
	if err != nil {
		return fmt.Errorf("failed to retrieve tags: %v", err)
	}
	w := new(tabwriter.Writer).Init(writer, 0, 8, 1, ' ', 0)
	fmt.Fprintln(w, "TAG\tREVISION\tNAMESPACES")
	for _, wh := range tagWebhooks {
		tagName, err := getTagName(wh)
		if err != nil {
			return fmt.Errorf("error parsing webhook \"%s\": %v", wh.Name, err)
		}
		tagRevision, err := getTagRevision(wh)
		if err != nil {
			return fmt.Errorf("error parsing webhook \"%s\": %v", wh.Name, err)
		}
		tagNamespaces, err := getNamespacesWithTag(ctx, kubeClient, tagName)
		if err != nil {
			return fmt.Errorf("error parsing webhook \"%s\": %v", wh.Name, err)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\n", tagName, tagRevision, strings.Join(tagNamespaces[:], ","))
	}

	return w.Flush()
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
		LabelSelector: fmt.Sprintf("%s=%s", istioTagLabel, tag),
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

// getNamespacesWithTag retrieves all namespaces pointed at the given tag
func getNamespacesWithTag(ctx context.Context, client kube.ExtendedClient, tag string) ([]string, error) {
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", label.IstioRev, tag),
	})
	if err != nil {
		return nil, err
	}

	nsNames := make([]string, len(namespaces.Items))
	for i, ns := range namespaces.Items {
		nsNames[i] = ns.Name
	}
	return nsNames, nil
}

// getTagName extracts tag name from webhook object
func getTagName(wh admit_v1.MutatingWebhookConfiguration) (string, error) {
	if tagName, ok := wh.ObjectMeta.Labels[istioTagLabel]; ok {
		return tagName, nil
	}
	return "", fmt.Errorf("could not extract tag name from webhook")
}

// getRevision extracts tag target revision from webhook object
func getTagRevision(wh admit_v1.MutatingWebhookConfiguration) (string, error) {
	if tagName, ok := wh.ObjectMeta.Labels[label.IstioRev]; ok {
		return tagName, nil
	}
	return "", fmt.Errorf("could not extract tag name from webhook")
}

// buildDeleteTagConfirmation takes a list of webhooks and creates a message prompting confirmation for their deletion
func buildDeleteTagConfirmation(tag string, taggedNamespaces []string) string {
	var sb strings.Builder
	base := fmt.Sprintf("Caution, found %d namespaces still pointing to tag \"%s\":", len(taggedNamespaces), tag)
	sb.WriteString(base)
	for _, ns := range taggedNamespaces {
		sb.WriteString(" " + ns)
	}
	sb.WriteString("\nProceed with deletion? (y/N)")

	return sb.String()
}
