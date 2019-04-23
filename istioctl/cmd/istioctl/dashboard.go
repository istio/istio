// Copyright 2019 Istio Authors
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

package main

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"istio.io/istio/istioctl/pkg/kubernetes"

	"istio.io/istio/pkg/log"
)

var (
	dashboardCmd = &cobra.Command{
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

	controlZport = 0
)

func init() {
	dashboardCmd.AddCommand(kialiDashCmd())
	dashboardCmd.AddCommand(promDashCmd())
	dashboardCmd.AddCommand(grafanaDashCmd())
	dashboardCmd.AddCommand(jaegerDashCmd())
	dashboardCmd.AddCommand(zipkinDashCmd())

	dashboardCmd.AddCommand(envoyDashCmd())
	controlz := controlZDashCmd()
	controlz.PersistentFlags().IntVar(&controlZport, "ctrlz_port", 9876, "ControlZ port")
	dashboardCmd.AddCommand(controlz)
	experimentalCmd.AddCommand(dashboardCmd)
}

// port-forward to Istio System Prometheus; open browser
func promDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "prometheus",
		Short:   "Open Prometheus web UI",
		Long:    `Open Istio's Prometheus dashboard`,
		Example: `istioctl experimental dashboard prometheus`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(istioNamespace, "app=prometheus")
			if err != nil {
				return fmt.Errorf("not able to locate Prometheus pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Prometheus pods found")
			}

			// only use the first pod in the list
			promPod := pl.Items[0]
			fw, err := client.BuildPortForwarder(promPod.Name, istioNamespace, 0, 9090)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for Prometheus: %v", err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to Prometheus pod ready")
				openBrowser(fmt.Sprintf("http://localhost:%d", fw.LocalPort), cmd.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			return nil
		},
	}

	return cmd
}

// port-forward to Istio System Grafana; open browser
func grafanaDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "grafana",
		Short:   "Open Grafana web UI",
		Long:    `Open Istio's Grafana dashboard`,
		Example: `istioctl experimental dashboard grafana`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(istioNamespace, "app=grafana")
			if err != nil {
				return fmt.Errorf("not able to locate Grafana pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Grafana pods found")
			}

			// only use the first pod in the list
			promPod := pl.Items[0]

			fw, err := client.BuildPortForwarder(promPod.Name, istioNamespace, 0, 3000)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for Grafana: %v", err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to Grafana pod ready")
				openBrowser(fmt.Sprintf("http://localhost:%d", fw.LocalPort), cmd.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			return nil
		},
	}

	return cmd
}

// port-forward to Istio System Kiali; open browser
func kialiDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "kiali",
		Short:   "Open Kiali web UI",
		Long:    `Open Istio's Kiali dashboard`,
		Example: `istioctl experimental dashboard kiali`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(istioNamespace, "app=kiali")
			if err != nil {
				return fmt.Errorf("not able to locate Kiali pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Kiali pods found")
			}

			// only use the first pod in the list
			promPod := pl.Items[0]
			fw, err := client.BuildPortForwarder(promPod.Name, istioNamespace, 0, 20001)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for Kiali: %v", err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to Kiali pod ready")
				openBrowser(fmt.Sprintf("http://localhost:%d/kiali", fw.LocalPort), cmd.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			return nil
		},
	}

	return cmd
}

// port-forward to Istio System Jaeger; open browser
func jaegerDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jaeger",
		Short:   "Open Jaeger web UI",
		Long:    `Open Istio's Jaeger dashboard`,
		Example: `istioctl experimental dashboard jaeger`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(istioNamespace, "app=jaeger")
			if err != nil {
				return fmt.Errorf("not able to locate Jaeger pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Jaeger pods found")
			}

			// only use the first pod in the list
			promPod := pl.Items[0]
			fw, err := client.BuildPortForwarder(promPod.Name, istioNamespace, 0, 16686)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for Jaeger: %v", err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to Jaeger pod ready")
				openBrowser(fmt.Sprintf("http://localhost:%d", fw.LocalPort), cmd.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			return nil
		},
	}

	return cmd
}

// port-forward to Istio System Zipkin; open browser
func zipkinDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "zipkin",
		Short:   "Open Zipkin web UI",
		Long:    `Open Istio's Zipkin dashboard`,
		Example: `istioctl experimental dashboard zipkin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			pl, err := client.PodsForSelector(istioNamespace, "app=zipkin")
			if err != nil {
				return fmt.Errorf("not able to locate Zipkin pod: %v", err)
			}

			if len(pl.Items) < 1 {
				return errors.New("no Zipkin pods found")
			}

			// only use the first pod in the list
			promPod := pl.Items[0]
			fw, err := client.BuildPortForwarder(promPod.Name, istioNamespace, 0, 9411)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for Zipkin: %v", err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to Jaeger pod ready")
				openBrowser(fmt.Sprintf("http://localhost:%d", fw.LocalPort), cmd.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}
			return nil
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
		Example: `istioctl experimental dashboard envoy productpage-123-456.default`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify a pod")
			}

			podName, ns := inferPodInfo(args[0], handleNamespace())
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			fw, err := client.BuildPortForwarder(podName, ns, 0, 15000)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for %s: %v", podName, err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to Envoy sidecar ready")
				openBrowser(fmt.Sprintf("http://localhost:%d", fw.LocalPort), c.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			return nil
		},
	}

	return cmd
}

// port-forward to sidecar ControlZ port; open browser
func controlZDashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "controlz <pod-name[.namespace]>",
		Short:   "Open ControlZ web UI",
		Long:    `Open the ControlZ web UI for a pod in the Istio control plane`,
		Example: `istioctl experimental dashboard controlz pilot-123-456.istio-system`,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) < 1 {
				c.Println(c.UsageString())
				return fmt.Errorf("specify a pod")
			}

			podName, ns := inferPodInfo(args[0], handleNamespace())
			client, err := clientExecFactory(kubeconfig, configContext)
			if err != nil {
				return fmt.Errorf("failed to create k8s client: %v", err)
			}

			fw, err := client.BuildPortForwarder(podName, ns, 0, controlZport)
			if err != nil {
				return fmt.Errorf("could not build port forwarder for %s: %v", podName, err)
			}

			if err = kubernetes.RunPortForwarder(fw, func(fw *kubernetes.PortForward) error {
				log.Debugf("port-forward to ControlZ port ready")
				openBrowser(fmt.Sprintf("http://localhost:%d", fw.LocalPort), c.OutOrStdout())
				return nil
			}); err != nil {
				return fmt.Errorf("failure running port forward process: %v", err)
			}

			return nil
		},
	}

	return cmd
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
