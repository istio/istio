package install

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	authorizationapi "k8s.io/api/authorization/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/kubectl/genericclioptions/resource"
)

const (
	minK8SVersion = "1.11"
)

type targetResource struct {
	namespace string
	group     string
	version   string
	name      string
}

func installPreCheck(istioNamespaceFlag *string, restClientGetter resource.RESTClientGetter, writer io.Writer) error {
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Checking the cluster to make sure it is ready for Istio installation...\n")
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "kubernetes-api\n")
	fmt.Fprintf(writer, "-----------------------\n")
	kubeClient, err := createKubeClient(restClientGetter)
	if err != nil {
		return fmt.Errorf("failed to initialize the k8s client: %v", err)
	}
	fmt.Fprintf(writer, "can initialize the k8s client\n")
	v, err := kubeClient.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to query the Kubernetes API Server: %v", err)
	}
	fmt.Fprintf(writer, "can query the Kubernetes API Server\n")

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "kubernetes-version\n")
	fmt.Fprintf(writer, "-----------------------\n")

	if !checkKubernetesVersion(v) {
		return fmt.Errorf("The Kubernetes API version: %v is lower than the minimum version: "+minK8SVersion, v)
	}
	fmt.Fprintf(writer, "is running the minimum Kubernetes API version\n")

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "Istio-existence\n")
	fmt.Fprintf(writer, "-----------------------\n")
	ns, _ := kubeClient.CoreV1().Namespaces().Get(*istioNamespaceFlag, meta_v1.GetOptions{})
	if ns != nil {
		return fmt.Errorf("control plane namespace: %v already exist", *istioNamespaceFlag)
	}
	fmt.Fprintf(writer, "control plane namespace does not already exist\n")

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "kubernetes-setup\n")
	fmt.Fprintf(writer, "-----------------------\n")
	Resources := make([]targetResource, 0)
	Resources = append(Resources, targetResource{"", "", "v1", "Namespace"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "rbac.authorization.k8s.io", "v1beta1", "ClusterRole"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "rbac.authorization.k8s.io", "v1beta1", "ClusterRoleBinding"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "apiextensions.k8s.io", "v1beta1", "CustomResourceDefinition"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "rbac.authorization.k8s.io", "v1beta1", "Role"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "", "v1", "ServiceAccount"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "", "v1", "Service"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "extensions", "v1beta1", "Deployments"})
	Resources = append(Resources, targetResource{*istioNamespaceFlag, "", "v1", "ConfigMap"})
	for _, targetResource := range Resources {
		err = checkCanCreateResources(kubeClient, targetResource)
		if err != nil {
			return err
		}
		fmt.Fprintf(writer, "Can create %v \n", targetResource.name)
	}

	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "SideCar-Injector\n")
	fmt.Fprintf(writer, "-----------------------\n")
	_, err = kubeClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().List(meta_v1.ListOptions{})
	if err != nil {
		fmt.Fprintf(writer, "The Cluster has not enabled admission webhook. Automatic sidecar injection is not supported.\n")
	} else {
		fmt.Fprintf(writer, "The Cluster has enabled admission webhook. Automatic sidecar injection is supported.\n")
	}
	fmt.Fprintf(writer, "\n")
	fmt.Fprintf(writer, "-----------------------\n")
	fmt.Fprintf(writer, "Install Pre-Check passed! The cluster is ready for Istio installation.\n")
	fmt.Fprintf(writer, "\n")
	return nil

}

func checkKubernetesVersion(versionInfo *version.Info) bool {

	v := strings.Join([]string{versionInfo.Major, versionInfo.Minor}, ".")
	if parseVersion(minK8SVersion, 4) < parseVersion(v, 4) {
		return true
	}
	return false
}
func parseVersion(s string, width int) int64 {
	strList := strings.Split(s, ".")
	format := fmt.Sprintf("%%s%%0%ds", width)
	v := ""
	for _, value := range strList {
		v = fmt.Sprintf(format, v, value)
	}
	var result int64
	var err error
	if result, err = strconv.ParseInt(v, 10, 64); err != nil {
		return 0
	}
	return result
}

func checkCanCreateResources(kubeClient *kubernetes.Clientset, r targetResource) error {
	auth := kubeClient.AuthorizationV1beta1()

	s := &authorizationapi.SelfSubjectAccessReview{
		Spec: authorizationapi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Namespace: r.namespace,
				Verb:      "create",
				Group:     r.group,
				Version:   r.version,
				Resource:  r.name,
			},
		},
	}

	response, err := auth.SelfSubjectAccessReviews().Create(s)
	if err != nil {
		return err
	}

	if !response.Status.Allowed {
		if len(response.Status.Reason) > 0 {
			return fmt.Errorf("missing permissions to create %s: %v", r.name, response.Status.Reason)
		}
		return fmt.Errorf("missing permissions to create %s", r.name)
	}
	return nil
}

func createKubeClient(restClientGetter resource.RESTClientGetter) (*kubernetes.Clientset, error) {
	restConfig, err := restClientGetter.ToRESTConfig()

	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restConfig)
}
