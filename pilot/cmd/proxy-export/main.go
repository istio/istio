// Copyright 2018 Aspen Mesh Authors
// No part of this software may be reproduced or transmitted in any
// form or by any means, electronic or mechanical, for any purpose,
// without express written permission of F5 Networks, Inc.

package main

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"istio.io/istio/pilot/pkg/proxyexporter"
	"istio.io/istio/pkg/cmd"
	"istio.io/istio/pkg/collateral"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/version"
)

var (
	config         proxyexporter.Config
	loggingOptions = log.DefaultOptions()

	exportCmd = &cobra.Command{
		Use:   "proxy-exporter",
		Short: "Istio Proxy Exporter",
		Long:  "Istio Proxy Exporter exports auxiliary runtime proxy status",
		RunE:  cmdProxyExport,
	}
)

func init() {
	config.SetDefaults()
	exportCmd.PersistentFlags().StringVar(&config.StatsdUDPAddress, "statsdUdpAddress", config.StatsdUDPAddress,
		"IP Address and Port of a statsd UDP listener (e.g. 10.75.241.127:9125)")
	exportCmd.PersistentFlags().StringVar(&config.InstanceName, "name", config.InstanceName,
		fmt.Sprintf("Name of the proxy instance. Overrides value from %s env variable.",
			proxyexporter.PodNameEnvVar))
	exportCmd.PersistentFlags().StringVar(&config.InstanceIP, "ip", config.InstanceIP,
		fmt.Sprintf("IP of the proxy instance. Overrides value from %s env variable.",
			proxyexporter.PodIPEnvVar))
	exportCmd.Flags().DurationVar(&config.PollInterval, "poll-interval", config.PollInterval,
		"Number of seconds between updates.")

	loggingOptions.AttachCobraFlags(exportCmd)

	cmd.AddFlags(exportCmd)

	exportCmd.AddCommand(version.CobraCommand())

	exportCmd.AddCommand(collateral.CobraCommand(exportCmd, &doc.GenManHeader{
		Title:   exportCmd.Short,
		Section: "istio-proxy-export CLI",
		Manual:  exportCmd.Short,
	}))
}

func main() {
	if err := exportCmd.Execute(); err != nil {
		log.Errora(err)
		os.Exit(-1)
	}
}

func cmdProxyExport(_ *cobra.Command, _ []string) error {
	if err := log.Configure(loggingOptions); err != nil {
		return err
	}
	exporter, err := proxyexporter.New(config)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		exporter.Run(ctx)
	}()

	stop := make(chan struct{})
	cmd.WaitSignal(stop)
	<-stop
	cancel()
	wg.Wait()
	_ = log.Sync()
	return nil
}
