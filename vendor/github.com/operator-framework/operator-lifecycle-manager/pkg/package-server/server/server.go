package server

import (
	"fmt"
	"io"
	"net"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/operator-framework/operator-lifecycle-manager/pkg/api/client"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/lib/queueinformer"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apiserver"
	genericpackagemanifests "github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/apiserver/generic"
	"github.com/operator-framework/operator-lifecycle-manager/pkg/package-server/provider"
)

// NewCommandStartPackageServer provides a CLI handler for 'start master' command
// with a default PackageServerOptions.
func NewCommandStartPackageServer(defaults *PackageServerOptions, stopCh <-chan struct{}) *cobra.Command {
	cmd := &cobra.Command{
		Short: "Launch a package API server",
		Long:  "Launch a package API server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := defaults.Run(stopCh); err != nil {
				return err
			}
			return nil
		},
	}

	flags := cmd.Flags()

	// flags.BoolVar(&defaults.InsecureKubeletTLS, "kubelet-insecure-tls", defaults.InsecureKubeletTLS, "Do not verify CA of serving certificates presented by Kubelets.  For testing purposes only.")
	flags.DurationVar(&defaults.WakeupInterval, "interval", defaults.WakeupInterval, "interval at which to re-sync CatalogSources")
	flags.StringSliceVar(&defaults.WatchedNamespaces, "watched-namespaces", defaults.WatchedNamespaces, "list of namespaces the package-server will watch watch for CatalogSources")
	flags.StringVar(&defaults.Kubeconfig, "kubeconfig", defaults.Kubeconfig, "path to the kubeconfig used to connect to the Kubernetes API server and the Kubelets (defaults to in-cluster config)")
	flags.BoolVar(&defaults.Debug, "debug", defaults.Debug, "use debug log level")

	defaults.SecureServing.AddFlags(flags)
	defaults.Authentication.AddFlags(flags)
	defaults.Authorization.AddFlags(flags)
	defaults.Features.AddFlags(flags)

	return cmd
}

type PackageServerOptions struct {
	// RecommendedOptions *genericoptions.RecommendedOptions
	SecureServing  *genericoptions.SecureServingOptionsWithLoopback
	Authentication *genericoptions.DelegatingAuthenticationOptions
	Authorization  *genericoptions.DelegatingAuthorizationOptions
	Features       *genericoptions.FeatureOptions

	GlobalNamespace   string
	WatchedNamespaces []string
	WakeupInterval    time.Duration

	Kubeconfig   string
	RegistryAddr string

	// Only to be used to for testing
	DisableAuthForTesting bool

	// Enable debug log level
	Debug bool

	SharedInformerFactory informers.SharedInformerFactory
	StdOut                io.Writer
	StdErr                io.Writer
}

func NewPackageServerOptions(out, errOut io.Writer) *PackageServerOptions {
	o := &PackageServerOptions{

		SecureServing:  genericoptions.WithLoopback(genericoptions.NewSecureServingOptions()),
		Authentication: genericoptions.NewDelegatingAuthenticationOptions(),
		Authorization:  genericoptions.NewDelegatingAuthorizationOptions(),
		Features:       genericoptions.NewFeatureOptions(),

		WatchedNamespaces: []string{v1.NamespaceAll},
		WakeupInterval:    5 * time.Minute,

		DisableAuthForTesting: false,
		Debug:                 false,

		StdOut: out,
		StdErr: errOut,
	}

	return o
}

func (o *PackageServerOptions) Complete() error {
	return nil
}

func (o *PackageServerOptions) Config() (*apiserver.Config, error) {
	if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	serverConfig := genericapiserver.NewConfig(genericpackagemanifests.Codecs)
	if err := o.SecureServing.ApplyTo(serverConfig); err != nil {
		return nil, err
	}

	if !o.DisableAuthForTesting {
		if err := o.Authentication.ApplyTo(&serverConfig.Authentication, serverConfig.SecureServing, nil); err != nil {
			return nil, err
		}
		if err := o.Authorization.ApplyTo(&serverConfig.Authorization); err != nil {
			return nil, err
		}
	}

	return &apiserver.Config{
		GenericConfig:  serverConfig,
		ProviderConfig: genericpackagemanifests.ProviderConfig{},
	}, nil
}

func (o *PackageServerOptions) Run(stopCh <-chan struct{}) error {
	if o.Debug {
		log.SetLevel(log.DebugLevel)
	}

	// grab the config for the API server
	config, err := o.Config()
	if err != nil {
		return err
	}
	config.GenericConfig.EnableMetrics = true

	// set up the client config
	var clientConfig *rest.Config
	if len(o.Kubeconfig) > 0 {
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: o.Kubeconfig}
		loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

		clientConfig, err = loader.ClientConfig()
	} else {
		clientConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("unable to construct lister client config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("unable to construct lister client: %v", err)
	}

	crClient, err := client.NewClient(o.Kubeconfig)
	if err != nil {
		return err
	}

	queueOperator, err := queueinformer.NewOperator(o.Kubeconfig, log.New())
	if err != nil {
		return err
	}

	sourceProvider := provider.NewRegistryProvider(crClient, queueOperator, o.WakeupInterval, o.WatchedNamespaces, o.GlobalNamespace)
	config.ProviderConfig.Provider = sourceProvider

	// we should never need to resync, since we're not worried about missing events,
	// and resync is actually for regular interval-based reconciliation these days,
	// so set the default resync interval to 0
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	server, err := config.Complete(informerFactory).New()
	if err != nil {
		return err
	}

	ready, _ := sourceProvider.Run(stopCh)
	<-ready

	return server.GenericAPIServer.PrepareRun().Run(stopCh)
}
