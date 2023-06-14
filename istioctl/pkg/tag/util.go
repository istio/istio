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

package tag

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	admitv1 "k8s.io/api/admissionregistration/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/util"
)

func GetTagWebhooks(ctx context.Context, client kubernetes.Interface) ([]admitv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: IstioTagLabel,
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// GetWebhooksWithTag returns webhooks tagged with istio.io/tag=<tag>.
func GetWebhooksWithTag(ctx context.Context, client kubernetes.Interface, tag string) ([]admitv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", IstioTagLabel, tag),
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// GetWebhooksWithRevision returns webhooks tagged with istio.io/rev=<rev> and NOT TAGGED with istio.io/tag.
// this retrieves the webhook created at revision installation rather than tag webhooks
func GetWebhooksWithRevision(ctx context.Context, client kubernetes.Interface, rev string) ([]admitv1.MutatingWebhookConfiguration, error) {
	webhooks, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s,!%s", label.IoIstioRev.Name, rev, IstioTagLabel),
	})
	if err != nil {
		return nil, err
	}
	return webhooks.Items, nil
}

// GetNamespacesWithTag retrieves all namespaces pointed at the given tag.
func GetNamespacesWithTag(ctx context.Context, client kubernetes.Interface, tag string) ([]string, error) {
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", label.IoIstioRev.Name, tag),
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

// GetWebhookTagName extracts tag name from webhook object.
func GetWebhookTagName(wh admitv1.MutatingWebhookConfiguration) (string, error) {
	if tagName, ok := wh.ObjectMeta.Labels[IstioTagLabel]; ok {
		return tagName, nil
	}
	return "", fmt.Errorf("could not extract tag name from webhook")
}

// GetWebhookRevision extracts tag target revision from webhook object.
func GetWebhookRevision(wh admitv1.MutatingWebhookConfiguration) (string, error) {
	if tagName, ok := wh.ObjectMeta.Labels[label.IoIstioRev.Name]; ok {
		return tagName, nil
	}
	return "", fmt.Errorf("could not extract tag revision from webhook")
}

// DeleteTagWebhooks deletes the given webhooks.
func DeleteTagWebhooks(ctx context.Context, client kubernetes.Interface, tag string) error {
	webhooks, err := GetWebhooksWithTag(ctx, client, tag)
	if err != nil {
		return err
	}
	var result error
	for _, wh := range webhooks {
		result = multierror.Append(result, client.AdmissionregistrationV1().MutatingWebhookConfigurations().Delete(ctx, wh.Name, metav1.DeleteOptions{})).ErrorOrNil()
	}
	return result
}

// DeleteDeprecatedValidator deletes the deprecated validating webhook configuration. This is used after a user explicitly
// sets a new default revision.
func DeleteDeprecatedValidator(ctx context.Context, client kubernetes.Interface) error {
	vwhs, err := client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: "app=istiod",
	})
	if err != nil {
		return err
	}
	var errs *multierror.Error
	for _, vwh := range vwhs.Items {
		// hacky but we want to remove the validators that used to be in base, not the per-revision webhooks.
		if !strings.Contains(vwh.Name, "validator") {
			errs = multierror.Append(errs,
				client.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, vwh.Name, metav1.DeleteOptions{}))
		}
	}
	if kerrors.IsNotFound(err) {
		return nil
	}
	return errs.ErrorOrNil()
}

// PreviousInstallExists checks whether there is an existing Istio installation. Should be used in installer when deciding
// whether to make an installation the default.
func PreviousInstallExists(ctx context.Context, client kubernetes.Interface) bool {
	mwhs, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(ctx, metav1.ListOptions{
		LabelSelector: "app=sidecar-injector",
	})
	if err != nil {
		return false
	}
	return len(mwhs.Items) > 0
}

// DeactivateIstioInjectionWebhook deactivates the istio-injection webhook from the given MutatingWebhookConfiguration if exists.
// used rather than just deleting the webhook since we want to keep it around after changing the default so user can later
// switch back to it. This is a hack but it is meant to cover a corner case where a user wants to migrate from a non-revisioned
// old version and then later decides to switch back to the old revision again.
func DeactivateIstioInjectionWebhook(ctx context.Context, client kubernetes.Interface) error {
	whs, err := GetWebhooksWithRevision(ctx, client, DefaultRevisionName)
	if err != nil {
		return err
	}
	if len(whs) == 0 {
		// no revision with default, no action required.
		return nil
	}
	if len(whs) > 1 {
		return fmt.Errorf("expected a single webhook for default revision")
	}
	webhook := whs[0]
	for i := range webhook.Webhooks {
		wh := webhook.Webhooks[i]
		// this is an abomination, but if this isn't a per-revision webhook, we want to make it ineffectual
		// without deleting it. Add a nonsense match.
		wh.NamespaceSelector = util.NeverMatch
		wh.ObjectSelector = util.NeverMatch
		webhook.Webhooks[i] = wh
	}
	admit := client.AdmissionregistrationV1().MutatingWebhookConfigurations()
	_, err = admit.Update(ctx, &webhook, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}
