//  Copyright 2019 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package settings

import (
	"time"

	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/probe"
)

// ToProbeOptions converts the given Probe stanza to Probe options.
func ToProbeOptions(p *Probe) (*probe.Options, error) {
	if p.Disable {
		return &probe.Options{}, nil
	}

	var interval time.Duration
	if p.Interval != nil {
		var err error
		interval, err = ptypes.Duration(p.Interval)
		if err != nil {
			return nil, err
		}
	}

	return &probe.Options{
		Path:           p.Path,
		UpdateInterval: interval,
	}, nil
}

// ToServerArgs converts the given Galley settings to server.Args.
func ToServerArgs(s *Galley) (*server.Args, error) {
	var err error
	var resyncPeriod time.Duration
	var enableServer bool
	var configPath string
	var insecure bool
	var credentialOptions *creds.Options
	var accessListFile string

	k := s.Processing.Source.GetKubernetes()
	if k != nil && k.ResyncPeriod != nil {
		if resyncPeriod, err = ptypes.Duration(k.ResyncPeriod); err != nil {
			return nil, err
		}
	}

	f := s.Processing.Source.GetFilesystem()
	if f != nil {
		configPath = f.Path
	}

	enableServer = !s.Processing.Server.Disable

	introspectionOptions := toIntrospectionOptions(&s.General.Introspection)

	i := s.Processing.Server.Auth.GetInsecure()
	if i != nil {
		insecure = true
	}
	m := s.Processing.Server.Auth.GetMtls()
	if m != nil {
		credentialOptions = &creds.Options{
			KeyFile:           m.PrivateKey,
			CertificateFile:   m.ClientCertificate,
			CACertificateFile: m.CaCertificates,
		}
		accessListFile = m.AccessListFile
	}

	a := server.DefaultArgs()
	a.APIAddress = s.Processing.Server.Address
	a.Insecure = insecure
	a.CredentialOptions = credentialOptions
	a.MaxReceivedMessageSize = uint(s.Processing.Server.MaxReceivedMessageSize)
	a.MaxConcurrentStreams = uint(s.Processing.Server.MaxConcurrentStreams)
	a.ResyncPeriod = resyncPeriod
	a.DomainSuffix = s.Processing.DomainSuffix
	a.AccessListFile = accessListFile
	a.ConfigPath = configPath
	a.DisableResourceReadyCheck = s.Processing.Server.DisableResourceReadyCheck
	a.EnableServer = enableServer
	a.EnableGRPCTracing = s.Processing.Server.GrpcTracing
	a.IntrospectionOptions = introspectionOptions
	a.KubeConfig = s.General.KubeConfig
	a.MeshConfigFile = s.General.MeshConfigFile

	return a, nil
}

// ToValidationArgs converts the given Galley settings to validation.WebhookParameters.
func ToValidationArgs(s *Galley) (*validation.WebhookParameters, error) {
	v := validation.DefaultArgs()
	v.CertFile = s.Validation.Tls.ClientCertificate
	v.KeyFile = s.Validation.Tls.PrivateKey
	v.CACertFile = s.Validation.Tls.CaCertificates

	v.EnableValidation = !s.Validation.Disable
	v.WebhookConfigFile = s.Validation.WebhookConfigFile
	v.Port = uint(s.Validation.WebhookPort)
	v.DeploymentAndServiceNamespace = s.Validation.DeploymentNamespace
	v.DeploymentName = s.Validation.DeploymentName
	v.ServiceName = s.Validation.ServiceName
	v.WebhookName = s.Validation.WebhookName

	if err := v.Validate(); err != nil {
		return nil, err
	}

	return v, nil
}

func toIntrospectionOptions(i *Introspection) *ctrlz.Options {
	if i.Disable {
		return &ctrlz.Options{
			Port: 0,
		}
	}

	return &ctrlz.Options{
		Port:    uint16(i.Port),
		Address: i.Address,
	}
}
