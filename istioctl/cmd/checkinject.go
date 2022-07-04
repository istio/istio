package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func checkInjectCommand() *cobra.Command {
	checkCommand := &cobra.Command{
		Use:   "check-inject [<type>/]<name>[.<namespace>]",
		Short: "Check the injection status or inject-ability of a given resource, explains why it is (or will be) injected or not",
		Long: `
Checks associated resources of the given resource, and running webhooks to examine whether the pod can be or will be injected or not.`,
		Example: `	# Check the injection status of a pod
	istioctl check-inject details-v1-fcff6c49c-kqnfk.test
	
	# Check the injection status of a pod under a deployment
	istioctl check-inject deployment/details-v1
`,
		Args: func(cmd *cobra.Command, args []string) error {
			//if len(args) != 1 {
			//	cmd.Println(cmd.UsageString())
			//	return fmt.Errorf("check-inject requires only <resource-name>[.<namespace>]")
			//}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return err
			}
			//podName, podNs, err := handlers.InferPodInfoFromTypedResource(args[0],
			//	handlers.HandleNamespace(namespace, defaultNamespace),
			//	client.UtilFactory())
			//if err != nil {
			//	return err
			//}
			whs, err := client.Kube().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				return err
			}
			if len(whs.Items) == 0 {
				return fmt.Errorf("ERROR: no webhooks found for sidecar injection")
			}

			// label use GetFirstPod
			// check pod anno or label
			// check ns istio-injection and rev label
			// check validating default namespaces config
			// determine if there are multiple injectors, which will have unexpected behaviors
			return nil
		},
	}
	return checkCommand
}

type webhookAnalysis struct {
	name     string
	revision string
	injected string
	reason   string
}

//func analyzeRunningWebhooks(client kube.ExtendedClient, whs []*admissionv1.MutatingWebhookConfiguration,
//	podName, podNamespace string) ([]webhookAnalysis, error) {
//results := make([]webhookAnalysis, 0)
//ns, err := client.Kube().CoreV1().Namespaces().Get(context.TODO(), podNamespace, metav1.GetOptions{})
//if err != nil {
//	return nil, err
//}
//for _, wh := range whs {
//	rev := wh.GetLabels()[label.IoIstioRev.Name]
//
//}
//}
