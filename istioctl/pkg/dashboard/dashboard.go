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

package dashboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/cli"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
)

var (
	listenPort     = 0
	controlZport   = 0
	promPort       = 0
	grafanaPort    = 0
	kialiPort      = 0
	jaegerPort     = 0
	zipkinPort     = 0
	skywalkingPort = 0

	bindAddress = ""

	// open browser or not, default is true
	browser = true

	// label selector
	labelSelector = ""

	addonNamespace = ""

	envoyDashNs = ""

	proxyAdminPort int
)

const (
	defaultPrometheusPort = 9090
	defaultGrafanaPort    = 3000
	defaultKialiPort      = 20001
	defaultJaegerPort     = 16686
	defaultZipkinPort     = 9411
	defaultSkywalkingPort = 8080
)

// port-forward to Istio System Prometheus; open browser
func promDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "prometheus",
		Short: "Open Prometheus web UI",
		Long:  `Open Istio's Prometheus dashboard`,
		Example: `  istioctl dashboard prometheus

  # with short syntax
  istioctl dash prometheus
  istioctl d prometheus`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=prometheus")
			if err != nil {
				return fmt.Errorf("not able to locate Prometheus pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Prometheus pods found")
			}

			// only use the first pod in the list
			return portForward(pl.Items[0].Name, addonNamespace, "Prometheus",
				"http://%s", bindAddress, promPort, client, cmd.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to Istio System Grafana; open browser
func grafanaDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "grafana",
		Short: "Open Grafana web UI",
		Long:  `Open Istio's Grafana dashboard`,
		Example: `  istioctl dashboard grafana

  # with short syntax
  istioctl dash grafana
  istioctl d grafana`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=grafana")
			if err != nil {
				return fmt.Errorf("not able to locate Grafana pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Grafana pods found")
			}

			// only use the first pod in the list
			return portForward(pl.Items[0].Name, addonNamespace, "Grafana",
				"http://%s", bindAddress, grafanaPort, client, cmd.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to Istio System Kiali; open browser
func kialiDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "kiali",
		Short: "Open Kiali web UI",
		Long:  `Open Istio's Kiali dashboard`,
		Example: `  istioctl dashboard kiali

  # with short syntax
  istioctl dash kiali
  istioctl d kiali`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=kiali")
			if err != nil {
				return fmt.Errorf("not able to locate Kiali pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Kiali pods found")
			}

			// only use the first pod in the list
			return portForward(pl.Items[0].Name, addonNamespace, "Kiali",
				"http://%s/kiali", bindAddress, kialiPort, client, cmd.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to Istio System Jaeger; open browser
func jaegerDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "jaeger",
		Short: "Open Jaeger web UI",
		Long:  `Open Istio's Jaeger dashboard`,
		Example: `  istioctl dashboard jaeger

  # with short syntax
  istioctl dash jaeger
  istioctl d jaeger`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=jaeger")
			if err != nil {
				return fmt.Errorf("not able to locate Jaeger pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Jaeger pods found")
			}

			// only use the first pod in the list
			return portForward(pl.Items[0].Name, addonNamespace, "Jaeger",
				"http://%s", bindAddress, jaegerPort, client, cmd.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to Istio System Zipkin; open browser
func zipkinDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "zipkin",
		Short: "Open Zipkin web UI",
		Long:  `Open Istio's Zipkin dashboard`,
		Example: `  istioctl dashboard zipkin

  # with short syntax
  istioctl dash zipkin
  istioctl d zipkin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=zipkin")
			if err != nil {
				return fmt.Errorf("not able to locate Zipkin pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Zipkin pods found")
			}

			// only use the first pod in the list
			return portForward(pl.Items[0].Name, addonNamespace, "Zipkin",
				"http://%s", bindAddress, zipkinPort, client, cmd.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to sidecar Envoy admin port; open browser
func envoyDashCmd(ctx cli.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "envoy [<type>/]<name>[.<namespace>]",
		Short: "Open Envoy admin web UI",
		Long:  `Open the Envoy admin dashboard for a sidecar`,
		Example: `  # Open Envoy dashboard for the productpage-123-456.default pod
  istioctl dashboard envoy productpage-123-456.default

  # Open Envoy dashboard for one pod under a deployment
  istioctl dashboard envoy deployment/productpage-v1

  # with short syntax
  istioctl dash envoy productpage-123-456.default
  istioctl d envoy productpage-123-456.default
`,
		RunE: func(c *cobra.Command, args []string) error {
			kubeClient, err := ctx.CLIClient()
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}
			if labelSelector == "" && len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify a pod or --selector")
			}

			if labelSelector != "" && len(args) > 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("name cannot be provided when a selector is specified")
			}

			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			var podName, ns string
			if labelSelector != "" {
				pl, err := kubeClient.PodsForSelector(context.TODO(), ctx.NamespaceOrDefault(envoyDashNs), labelSelector)
				if err != nil {
					return fmt.Errorf("not able to locate pod with selector %s: %v", labelSelector, err)
				}

				if len(pl.Items) < 1 {
					return errors.New("no pods found")
				}

				if len(pl.Items) > 1 {
					log.Warnf("more than 1 pods fits selector: %s; will use pod: %s", labelSelector, pl.Items[0].Name)
				}

				// only use the first pod in the list
				podName = pl.Items[0].Name
				ns = pl.Items[0].Namespace
			} else {
				podName, ns, err = ctx.InferPodInfoFromTypedResource(args[0], envoyDashNs)
				if err != nil {
					return err
				}
			}

			return portForward(podName, ns, fmt.Sprintf("Envoy sidecar %s", podName),
				"http://%s", bindAddress, proxyAdminPort, kubeClient, c.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to sidecar ControlZ port; open browser
func controlZDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "controlz [<type>/]<name>[.<namespace>]",
		Short: "Open ControlZ web UI",
		Long:  `Open the ControlZ web UI for a pod in the Istio control plane`,
		Example: `  # Open ControlZ web UI for the istiod-123-456.istio-system pod
  istioctl dashboard controlz istiod-123-456.istio-system

  # Open ControlZ web UI for the istiod-56dd66799-jfdvs pod in a custom namespace
  istioctl dashboard controlz istiod-123-456 -n custom-ns

  # Open ControlZ web UI for any Istiod pod
  istioctl dashboard controlz deployment/istiod.istio-system

  # with short syntax
  istioctl dash controlz pilot-123-456.istio-system
  istioctl d controlz pilot-123-456.istio-system
`,
		RunE: func(c *cobra.Command, args []string) error {
			if labelSelector == "" && len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify a pod or --selector")
			}

			if labelSelector != "" && len(args) > 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("name cannot be provided when a selector is specified")
			}

			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			var podName, ns string
			if labelSelector != "" {
				pl, err := client.PodsForSelector(context.TODO(), ctx.NamespaceOrDefault(addonNamespace), labelSelector)
				if err != nil {
					return fmt.Errorf("not able to locate pod with selector %s: %v", labelSelector, err)
				}

				if len(pl.Items) < 1 {
					return errors.New("no pods found")
				}

				if len(pl.Items) > 1 {
					log.Warnf("more than 1 pods fits selector: %s; will use pod: %s", labelSelector, pl.Items[0].Name)
				}

				// only use the first pod in the list
				podName = pl.Items[0].Name
				ns = pl.Items[0].Namespace
			} else {
				podName, ns, err = ctx.InferPodInfoFromTypedResource(args[0], addonNamespace)
				if err != nil {
					return err
				}
			}

			return portForward(podName, ns, fmt.Sprintf("ControlZ %s", podName),
				"http://%s", bindAddress, controlZport, client, c.OutOrStdout(), browser)
		},
	}

	return cmd
}

// port-forward to SkyWalking UI on istio-system
func skywalkingDashCmd(ctx cli.Context) *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:   "skywalking",
		Short: "Open SkyWalking UI",
		Long:  "Open the Istio dashboard in the SkyWalking UI",
		Example: `  istioctl dashboard skywalking

  # with short syntax
  istioctl dash skywalking
  istioctl d skywalking`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := ctx.CLIClientWithRevision(opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(context.TODO(), addonNamespace, "app=skywalking-ui")
			if err != nil {
				return fmt.Errorf("not able to locate SkyWalking UI pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no SkyWalking UI pods found")
			}

			// only use the first pod in the list
			return portForward(pl.Items[0].Name, addonNamespace, "SkyWalking",
				"http://%s", bindAddress, skywalkingPort, client, cmd.OutOrStdout(), browser)
		},
	}

	return cmd
}

// portForward first tries to forward localhost:remotePort to podName:remotePort, falls back to dynamic local port
func portForward(podName, namespace, flavor, urlFormat, localAddress string, remotePort int,
	client kube.CLIClient, writer io.Writer, browser bool,
) error {
	// port preference:
	// - If --listenPort is specified, use it
	// - without --listenPort, prefer the remotePort but fall back to a random port
	var portPrefs []int
	if listenPort != 0 {
		portPrefs = []int{listenPort}
	} else {
		portPrefs = []int{remotePort, 0}
	}

	var err error
	for _, localPort := range portPrefs {
		var fw kube.PortForwarder
		fw, err = client.NewPortForwarder(podName, namespace, localAddress, localPort, remotePort)
		if err != nil {
			return fmt.Errorf("could not build port forwarder for %s: %v", flavor, err)
		}

		if err = fw.Start(); err != nil {
			fw.Close()
			// Try the next port
			continue
		}

		// Close the port forwarder when the command is terminated.
		ClosePortForwarderOnInterrupt(fw)

		log.Debugf(fmt.Sprintf("port-forward to %s pod ready", flavor))
		openBrowser(fmt.Sprintf(urlFormat, fw.Address()), writer, browser)

		// Wait for stop
		fw.WaitForStop()

		return nil
	}

	return fmt.Errorf("failure running port forward process: %v", err)
}

func ClosePortForwarderOnInterrupt(fw kube.PortForwarder) {
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		defer signal.Stop(signals)
		<-signals
		fw.Close()
	}()
}

func openBrowser(url string, writer io.Writer, browser bool) {
	var err error

	fmt.Fprintf(writer, "%s\n", url)

	if !browser {
		fmt.Fprint(writer, "skipping opening a browser")
		return
	}

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		fmt.Fprintf(writer, "Unsupported platform %q; open %s in your browser.\n", runtime.GOOS, url)
	}

	if err != nil {
		fmt.Fprintf(writer, "Failed to open browser; open %s in your browser.\n", url)
	}
}

func Dashboard(cliContext cli.Context) *cobra.Command {
	dashboardCmd := &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"dash", "d"},
		Short:   "Access to Istio web UIs",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("unknown dashboard %q", args[0])
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			return nil
		},
	}

	dashboardCmd.PersistentFlags().IntVarP(&listenPort, "port", "p", 0, "Local port to listen to")
	dashboardCmd.PersistentFlags().StringVar(&bindAddress, "address", "localhost",
		"Address to listen on. Only accepts IP address or localhost as a value. "+
			"When localhost is supplied, istioctl will try to bind on both 127.0.0.1 and ::1 "+
			"and will fail if neither of these address are available to bind.")
	dashboardCmd.PersistentFlags().BoolVar(&browser, "browser", true,
		"When --browser is supplied as false, istioctl dashboard will not open the browser. "+
			"Default is true which means istioctl dashboard will always open a browser to view the dashboard.")
	dashboardCmd.PersistentFlags().StringVarP(&addonNamespace, "namespace", "n", "istio-system",
		"Namespace where the addon is running, if not specified, istio-system would be used")

	kiali := kialiDashCmd(cliContext)
	kiali.PersistentFlags().IntVar(&kialiPort, "ui-port", defaultKialiPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(kiali)

	prom := promDashCmd(cliContext)
	prom.PersistentFlags().IntVar(&promPort, "ui-port", defaultPrometheusPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(prom)

	graf := grafanaDashCmd(cliContext)
	graf.PersistentFlags().IntVar(&grafanaPort, "ui-port", defaultGrafanaPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(graf)

	jaeger := jaegerDashCmd(cliContext)
	jaeger.PersistentFlags().IntVar(&jaegerPort, "ui-port", defaultJaegerPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(jaeger)

	zipkin := zipkinDashCmd(cliContext)
	zipkin.PersistentFlags().IntVar(&zipkinPort, "ui-port", defaultZipkinPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(zipkin)

	skywalking := skywalkingDashCmd(cliContext)
	skywalking.PersistentFlags().IntVar(&skywalkingPort, "ui-port", defaultSkywalkingPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(skywalking)

	envoy := envoyDashCmd(cliContext)
	envoy.PersistentFlags().StringVarP(&labelSelector, "selector", "l", "", "Label selector")
	envoy.PersistentFlags().StringVarP(&envoyDashNs, "namespace", "n", cliContext.NamespaceOrDefault(""),
		"Namespace where the addon is running, if not specified, istio-system would be used")
	envoy.PersistentFlags().IntVar(&proxyAdminPort, "ui-port", util.DefaultProxyAdminPort, "The component dashboard UI port.")
	dashboardCmd.AddCommand(envoy)

	controlz := controlZDashCmd(cliContext)
	controlz.PersistentFlags().IntVar(&controlZport, "ctrlz_port", 9876, "ControlZ port")
	controlz.PersistentFlags().StringVarP(&labelSelector, "selector", "l", "", "Label selector")
	controlz.PersistentFlags().StringVarP(&addonNamespace, "namespace", "n", "istio-system",
		"Namespace where the addon is running, if not specified, istio-system would be used")
	dashboardCmd.AddCommand(controlz)

	return dashboardCmd
}
