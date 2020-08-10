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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"

	"github.com/spf13/cobra"

	"istio.io/pkg/log"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/util/handlers"
	"istio.io/istio/pkg/kube"
)

var (
	listenPort   = 0
	controlZport = 0

	bindAddress = ""

	// label selector
	labelSelector = ""

	addonNamespace = ""
)

// port-forward to Istio System Prometheus; open browser
func promDashCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "prometheus",
		Short:   "Open Prometheus web UI",
		Long:    `Open Istio's Prometheus dashboard`,
		Example: `istioctl dashboard prometheus`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
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
				"http://%s", bindAddress, 9090, client, cmd.OutOrStdout())
		},
	}

	return cmd
}

// port-forward to Istio System Grafana; open browser
func grafanaDashCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "grafana",
		Short:   "Open Grafana web UI",
		Long:    `Open Istio's Grafana dashboard`,
		Example: `istioctl dashboard grafana`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
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
				"http://%s", bindAddress, 3000, client, cmd.OutOrStdout())
		},
	}

	return cmd
}

// port-forward to Istio System Kiali; open browser
func kialiDashCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "kiali",
		Short:   "Open Kiali web UI",
		Long:    `Open Istio's Kiali dashboard`,
		Example: `istioctl dashboard kiali`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
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
				"http://%s/kiali", bindAddress, 20001, client, cmd.OutOrStdout())
		},
	}

	return cmd
}

// port-forward to Istio System Jaeger; open browser
func jaegerDashCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "jaeger",
		Short:   "Open Jaeger web UI",
		Long:    `Open Istio's Jaeger dashboard`,
		Example: `istioctl dashboard jaeger`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
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
				"http://%s", bindAddress, 16686, client, cmd.OutOrStdout())
		},
	}

	return cmd
}

// port-forward to Istio System Zipkin; open browser
func zipkinDashCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "zipkin",
		Short:   "Open Zipkin web UI",
		Long:    `Open Istio's Zipkin dashboard`,
		Example: `istioctl dashboard zipkin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
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
				"http://%s", bindAddress, 9411, client, cmd.OutOrStdout())
		},
	}

	return cmd
}

// port-forward to sidecar Envoy admin port; open browser
func envoyDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "envoy <pod-name[.namespace]>",
		Short:   "Open Envoy admin web UI",
		Long:    `Open the Envoy admin dashboard for a sidecar`,
		Example: `istioctl dashboard envoy productpage-123-456.default`,
		RunE: func(c *cobra.Command, args []string) error {
			if labelSelector == "" && len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify a pod or --selector")
			}

			if labelSelector != "" && len(args) > 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("name cannot be provided when a selector is specified")
			}

			client, err := kubeClient(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			var podName, ns string
			if labelSelector != "" {
				pl, err := client.PodsForSelector(context.TODO(), handlers.HandleNamespace(namespace, defaultNamespace), labelSelector)
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
				podName, ns = handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
			}

			return portForward(podName, ns, fmt.Sprintf("Envoy sidecar %s", podName),
				"http://%s", bindAddress, 15000, client, c.OutOrStdout())
		},
	}

	return cmd
}

// port-forward to sidecar ControlZ port; open browser
func controlZDashCmd() *cobra.Command {
	var opts clioptions.ControlPlaneOptions
	cmd := &cobra.Command{
		Use:     "controlz <pod-name[.namespace]>",
		Short:   "Open ControlZ web UI",
		Long:    `Open the ControlZ web UI for a pod in the Istio control plane`,
		Example: `istioctl dashboard controlz pilot-123-456.istio-system`,
		RunE: func(c *cobra.Command, args []string) error {
			if labelSelector == "" && len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify a pod or --selector")
			}

			if labelSelector != "" && len(args) > 0 {
				c.Println(c.UsageString())
				return fmt.Errorf("name cannot be provided when a selector is specified")
			}

			client, err := kubeClientWithRevision(kubeconfig, configContext, opts.Revision)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			var podName, ns string
			if labelSelector != "" {
				pl, err := client.PodsForSelector(context.TODO(), handlers.HandleNamespace(namespace, defaultNamespace), labelSelector)
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
				podName, ns = handlers.InferPodInfo(args[0], handlers.HandleNamespace(namespace, defaultNamespace))
			}

			return portForward(podName, ns, fmt.Sprintf("ControlZ %s", podName),
				"http://%s", bindAddress, controlZport, client, c.OutOrStdout())
		},
	}

	return cmd
}

// portForward first tries to forward localhost:remotePort to podName:remotePort, falls back to dynamic local port
func portForward(podName, namespace, flavor, urlFormat, localAddress string, remotePort int,
	client kube.ExtendedClient, writer io.Writer) error {

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
		fw, err := client.NewPortForwarder(podName, namespace, localAddress, localPort, remotePort)
		if err != nil {
			return fmt.Errorf("could not build port forwarder for %s: %v", flavor, err)
		}

		if err = fw.Start(); err != nil {
			// Try the next port
			continue
		}

		// Close the port forwarder when the command is terminated.
		closePortForwarderOnInterrupt(fw)

		log.Debugf(fmt.Sprintf("port-forward to %s pod ready", flavor))
		openBrowser(fmt.Sprintf(urlFormat, fw.Address()), writer)

		// Wait for stop
		fw.WaitForStop()

		return nil
	}

	return fmt.Errorf("failure running port forward process: %v", err)
}

func closePortForwarderOnInterrupt(fw kube.PortForwarder) {
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)
		defer signal.Stop(signals)
		<-signals
		fw.Close()
	}()
}
func openBrowser(url string, writer io.Writer) {
	var err error

	fmt.Fprintf(writer, "%s\n", url)

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

func dashboard() *cobra.Command {
	dashboardCmd := &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"dash", "d"},
		Short:   "Access to Istio web UIs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown dashboard %q", args[0])
			}

			return nil
		},
	}

	dashboardCmd.PersistentFlags().IntVarP(&listenPort, "port", "p", 0, "Local port to listen to")
	dashboardCmd.PersistentFlags().StringVar(&bindAddress, "address", "localhost",
		"Address to listen on. Only accepts IP address or localhost as a value. "+
			"When localhost is supplied, istioctl will try to bind on both 127.0.0.1 and ::1 "+
			"and will fail if neither of these address are available to bind.")
	dashboardCmd.PersistentFlags().StringVar(&addonNamespace, "namespace", istioNamespace,
		"Namespace where the addon is running, if not specified, istio-system would be used")

	dashboardCmd.AddCommand(kialiDashCmd())
	dashboardCmd.AddCommand(promDashCmd())
	dashboardCmd.AddCommand(grafanaDashCmd())
	dashboardCmd.AddCommand(jaegerDashCmd())
	dashboardCmd.AddCommand(zipkinDashCmd())

	envoy := envoyDashCmd()
	envoy.PersistentFlags().StringVarP(&labelSelector, "selector", "l", "", "label selector")
	dashboardCmd.AddCommand(envoy)

	controlz := controlZDashCmd()
	controlz.PersistentFlags().IntVar(&controlZport, "ctrlz_port", 9876, "ControlZ port")
	controlz.PersistentFlags().StringVarP(&labelSelector, "selector", "l", "", "label selector")
	dashboardCmd.AddCommand(controlz)

	return dashboardCmd
}
