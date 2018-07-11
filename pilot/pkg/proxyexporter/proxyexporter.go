// Copyright 2018 Aspen Mesh Authors
// No part of this software may be reproduced or transmitted in any
// form or by any means, electronic or mechanical, for any purpose,
// without express written permission of F5 Networks, Inc.

package proxyexporter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// PodNameEnvVar is the name of the environment variable containing the pod name
	PodNameEnvVar = "POD_NAME"
	// PodNamespaceEnvVar is the name of environment variable containing the pod namespace
	PodNamespaceEnvVar = "POD_NAMESPACE"
	// PodUIDEnvVar is the name of the environment variable containing the pod UID
	PodUIDEnvVar = "POD_UID"
	// PodIPEnvVar is the name of the environment variable containing the pod IP
	PodIPEnvVar = "INSTANCE_IP"
)

// A Config holds all config values for CertExporter
type Config struct {
	StatsdUDPAddress string
	AgentAddress     string
	InstanceName     string
	InstanceIP       string
	PollInterval     time.Duration
}

// DefaultOptions creates a new config populated with default values
func DefaultOptions() *Config {
	cfg := &Config{}
	cfg.SetDefaults()
	return cfg
}

// SetDefaults sets all the default config values
func (cfg *Config) SetDefaults() {
	values := model.DefaultProxyConfig()
	cfg.StatsdUDPAddress = values.StatsdUdpAddress
	cfg.AgentAddress = "aspen-mesh-agent.istio-system.svc.cluster.local:12001"

	name := os.Getenv(PodNameEnvVar)
	namespace := os.Getenv(PodNamespaceEnvVar)
	if len(name) == 0 || len(namespace) == 0 {
		cfg.InstanceName = "UNKNOWN"
	} else {
		cfg.InstanceName = name + "." + namespace
	}

	cfg.InstanceIP = os.Getenv(PodIPEnvVar)
	if len(cfg.InstanceIP) == 0 {
		cfg.InstanceIP = "UNKNOWN"
	}

	cfg.PollInterval = 15 * time.Second
}

// A CertExporter exports status of certificates used by envoy
type CertExporter struct {
	statsc    *statsd.Client
	cfg       Config
	exporters []Exporter
}

// An Exporter can export information to statsd or other collectors
type Exporter interface {
	// Called periodically to allow the exporter to export info
	Export(ctx context.Context, statsc *statsd.Client) error
	// Called once before exit to allow and cleanup.
	Finalize(ctx context.Context, statsc *statsd.Client) error
}

// New returns a new CertExporter
func New(cfg Config) (*CertExporter, error) {
	statsc, err := statsd.New(cfg.StatsdUDPAddress)
	if err != nil {
		return nil, err
	}

	statsc.Namespace = "istio.proxy.certs."

	// Tags for all metrics
	addTag(statsc, "pod_name", cfg.InstanceName)
	addTag(statsc, "pod_ip", cfg.InstanceIP)

	uid := os.Getenv(PodUIDEnvVar)
	if len(uid) != 0 {
		addTag(statsc, "pod_uid", uid)
	}

	certExporter := NewProxyCertInfo(cfg.AgentAddress)
	return &CertExporter{
		statsc:    statsc,
		cfg:       cfg,
		exporters: []Exporter{certExporter},
	}, nil
}

func addTag(statsc *statsd.Client, name, val string) {
	statsc.Tags = append(statsc.Tags, fmt.Sprintf("%s:%s", name, val))
}

// Run will poll and export status information until the context is done
func (e *CertExporter) Run(ctx context.Context) {
	defer func() {
		_ = e.statsc.Flush()
	}()

	ticker := time.NewTicker(e.cfg.PollInterval)
	defer ticker.Stop()

	log.Info("Running proxy export")
	log.Infof("  statsd: %s", e.cfg.StatsdUDPAddress)
	log.Infof("  agent: %s", e.cfg.AgentAddress)

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping proxy export")
			err := e.finalize()
			if err != nil {
				log.Warnf("Failed to cleanup: %v", err)
			}
			return
		case <-ticker.C:
			err := e.export(ctx)
			if err != nil {
				log.Errorf("Error exporting proxy status: %v", err)
			}
		}
	}
}

func (e *CertExporter) export(ctx context.Context) error {
	for i := range e.exporters {
		err := e.exporters[i].Export(ctx, e.statsc)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *CertExporter) finalize() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan []string, 1)
	go func() {
		errs := []string{}
		for i := range e.exporters {
			err := e.exporters[i].Finalize(ctx, e.statsc)
			if err != nil {
				errs = append(errs, err.Error())
			}
		}
		ch <- errs
		close(ch)
	}()

	select {
	case errs := <-ch:
		if len(errs) != 0 {
			return fmt.Errorf(strings.Join(errs, "; "))
		}
	case <-time.After(3 * time.Second):
		log.Warnf("Timeout during finalize")
	}

	return nil
}
