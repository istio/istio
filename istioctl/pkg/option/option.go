package option

import (
	"github.com/spf13/pflag"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/pointer"
)

const (
	FlagKubeConfig     = "kubeconfig"
	FlagContext        = "context"
	FlagNamespace      = "namespace"
	FlagIstioNamespace = "istioNamespace"
)

type RootFlags struct {
	kubeconfig     *string
	configContext  *string
	namespace      *string
	istioNamespace *string

	defaultNamespace string
}

func NewRootFlags() *RootFlags {
	return &RootFlags{
		kubeconfig:     pointer.String(""),
		configContext:  pointer.String(""),
		namespace:      pointer.String(""),
		istioNamespace: pointer.String(""),
	}
}

func (r *RootFlags) AddFlags(flags *pflag.FlagSet) {
	if r.kubeconfig != nil {
		flags.StringVar(r.kubeconfig, FlagKubeConfig, "",
			"Kubernetes configuration file")
	}
	if r.configContext != nil {
		flags.StringVar(r.configContext, FlagContext, "",
			"Kubernetes configuration context")
	}
	if r.namespace != nil {
		flags.StringVar(r.namespace, FlagNamespace, "",
			"Kubernetes namespace")
	}
	if r.istioNamespace != nil {
		flags.StringVar(r.istioNamespace, FlagIstioNamespace, "",
			"Istio system namespace")
	}
}

func (r *RootFlags) KubeConfig() string {
	return *r.kubeconfig
}

func (r *RootFlags) KubeContext() string {
	return *r.configContext
}

func (r *RootFlags) Namespace() string {
	return *r.namespace
}

func (r *RootFlags) IstioNamespace() string {
	return *r.istioNamespace
}

func (r *RootFlags) DefaultNamespace() string {
	return r.defaultNamespace
}

func (r *RootFlags) ConfigureDefaultNamespace() {
	configAccess := clientcmd.NewDefaultPathOptions()

	kubeconfig := *r.kubeconfig
	if kubeconfig != "" {
		// use specified kubeconfig file for the location of the
		// config to read
		configAccess.GlobalFile = kubeconfig
	}

	// gets existing kubeconfig or returns new empty config
	config, err := configAccess.GetStartingConfig()
	if err != nil {
		r.defaultNamespace = v1.NamespaceDefault
		return
	}

	// If a specific context was specified, use that. Otherwise, just use the current context from the kube config.
	selectedContext := config.CurrentContext
	if *r.configContext != "" {
		selectedContext = *r.configContext
	}

	// Use the namespace associated with the selected context as default, if the context has one
	context, ok := config.Contexts[selectedContext]
	if !ok {
		r.defaultNamespace = v1.NamespaceDefault
		return
	}
	if context.Namespace == "" {
		r.defaultNamespace = v1.NamespaceDefault
		return
	}
	r.defaultNamespace = context.Namespace
}
