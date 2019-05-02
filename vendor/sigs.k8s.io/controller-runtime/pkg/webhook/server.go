/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	atypes "sigs.k8s.io/controller-runtime/pkg/webhook/admission/types"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert"
	"sigs.k8s.io/controller-runtime/pkg/webhook/internal/cert/writer"
	"sigs.k8s.io/controller-runtime/pkg/webhook/types"
)

// default interval for checking cert is 90 days (~3 months)
var defaultCertRefreshInterval = 3 * 30 * 24 * time.Hour

// ServerOptions are options for configuring an admission webhook server.
type ServerOptions struct {
	// Port is the port number that the server will serve.
	// It will be defaulted to 443 if unspecified.
	Port int32

	// CertDir is the directory that contains the server key and certificate.
	// If using FSCertWriter in Provisioner, the server itself will provision the certificate and
	// store it in this directory.
	// If using SecretCertWriter in Provisioner, the server will provision the certificate in a secret,
	// the user is responsible to mount the secret to the this location for the server to consume.
	CertDir string

	// Client is a client defined in controller-runtime instead of a client-go client.
	// It knows how to talk to a kubernetes cluster.
	// Client will be injected by the manager if not set.
	Client client.Client

	// DisableWebhookConfigInstaller controls if the server will automatically create webhook related objects
	// during bootstrapping. e.g. webhookConfiguration, service and secret.
	// If false, the server will install the webhook config objects. It is defaulted to false.
	DisableWebhookConfigInstaller *bool

	// BootstrapOptions contains the options for bootstrapping the admission server.
	*BootstrapOptions
}

// BootstrapOptions are options for bootstrapping an admission webhook server.
type BootstrapOptions struct {
	// MutatingWebhookConfigName is the name that used for creating the MutatingWebhookConfiguration object.
	MutatingWebhookConfigName string
	// ValidatingWebhookConfigName is the name that used for creating the ValidatingWebhookConfiguration object.
	ValidatingWebhookConfigName string

	// Secret is the location for storing the certificate for the admission server.
	// The server should have permission to create a secret in the namespace.
	// This is optional. If unspecified, it will write to the filesystem.
	// It the secret already exists and is different from the desired, it will be replaced.
	Secret *apitypes.NamespacedName

	// Deprecated: Writer will not be used anywhere.
	Writer io.Writer

	// Service is k8s service fronting the webhook server pod(s).
	// This field is optional. But one and only one of Service and Host need to be set.
	// This maps to field .webhooks.getClientConfig.service
	// https://github.com/kubernetes/api/blob/183f3326a9353bd6d41430fc80f96259331d029c/admissionregistration/v1beta1/types.go#L260
	Service *Service
	// Host is the host name of .webhooks.clientConfig.url
	// https://github.com/kubernetes/api/blob/183f3326a9353bd6d41430fc80f96259331d029c/admissionregistration/v1beta1/types.go#L250
	// This field is optional. But one and only one of Service and Host need to be set.
	// If neither Service nor Host is unspecified, Host will be defaulted to "localhost".
	Host *string

	// certProvisioner is constructed using certGenerator and certWriter
	certProvisioner *cert.Provisioner // nolint: structcheck

	// err will be non-nil if there is an error occur during initialization.
	err error // nolint: structcheck
}

// Service contains information for creating a service
type Service struct {
	// Name of the service
	Name string
	// Namespace of the service
	Namespace string
	// Selectors is the selector of the service.
	// This must select the pods that runs this webhook server.
	Selectors map[string]string
}

// Server is an admission webhook server that can serve traffic and
// generates related k8s resources for deploying.
type Server struct {
	// Name is the name of server
	Name string

	// ServerOptions contains options for configuring the admission server.
	ServerOptions

	sMux *http.ServeMux
	// registry maps a path to a http.Handler.
	registry map[string]Webhook

	// mutatingWebhookConfiguration and validatingWebhookConfiguration are populated during server bootstrapping.
	// They can be nil, if there is no webhook registered under it.
	webhookConfigurations []runtime.Object

	// manager is the manager that this webhook server will be registered.
	manager manager.Manager

	// httpServer is the actual server that serves the traffic.
	httpServer *http.Server

	once sync.Once
}

// Webhook defines the basics that a webhook should support.
type Webhook interface {
	// GetName returns the name of the webhook.
	GetName() string
	// GetPath returns the path that the webhook registered.
	GetPath() string
	// GetType returns the Type of the webhook.
	// e.g. mutating or validating
	GetType() types.WebhookType
	// Handler returns a http.Handler for the webhook.
	Handler() http.Handler
	// Validate validates if the webhook itself is valid.
	// If invalid, a non-nil error will be returned.
	Validate() error
}

// NewServer creates a new admission webhook server.
func NewServer(name string, mgr manager.Manager, options ServerOptions) (*Server, error) {
	as := &Server{
		Name:          name,
		sMux:          http.NewServeMux(),
		registry:      map[string]Webhook{},
		ServerOptions: options,
		manager:       mgr,
	}

	return as, nil
}

// Register validates and registers webhook(s) in the server
func (s *Server) Register(webhooks ...Webhook) error {
	for i, webhook := range webhooks {
		// validate the webhook before registering it.
		err := webhook.Validate()
		if err != nil {
			return err
		}
		_, found := s.registry[webhook.GetPath()]
		if found {
			return fmt.Errorf("can't register duplicate path: %v", webhook.GetPath())
		}
		s.registry[webhook.GetPath()] = webhooks[i]
		s.sMux.Handle(webhook.GetPath(), webhook.Handler())
	}

	// Lazily add Server to manager.
	// Because the all webhook handlers to be in place, so we can inject the things they need.
	return s.manager.Add(s)
}

// Handle registers a http.Handler for the given pattern.
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.sMux.Handle(pattern, handler)
}

var _ manager.Runnable = &Server{}

// Start runs the server.
// It will install the webhook related resources depend on the server configuration.
func (s *Server) Start(stop <-chan struct{}) error {
	s.once.Do(s.setDefault)
	if s.err != nil {
		return s.err
	}

	if s.DisableWebhookConfigInstaller != nil && !*s.DisableWebhookConfigInstaller {
		log.Info("installing webhook configuration in cluster")
		err := s.InstallWebhookManifests()
		if err != nil {
			return err
		}
	} else {
		log.Info("webhook installer is disabled")
	}

	return s.run(stop)
}

func (s *Server) run(stop <-chan struct{}) error { // nolint: gocyclo
	errCh := make(chan error)
	serveFn := func() {
		s.httpServer = &http.Server{
			Addr:    fmt.Sprintf(":%v", s.Port),
			Handler: s.sMux,
		}
		log.Info("starting the webhook server.")
		errCh <- s.httpServer.ListenAndServeTLS(path.Join(s.CertDir, writer.ServerCertName), path.Join(s.CertDir, writer.ServerKeyName))
	}

	shutdownHappend := false
	timer := time.Tick(wait.Jitter(defaultCertRefreshInterval, 0.1))
	go serveFn()
	for {
		select {
		case <-timer:
			changed, err := s.RefreshCert()
			if err != nil {
				log.Error(err, "encountering error when refreshing the certificate")
				return err
			}
			if !changed {
				log.Info("no need to reload the certificates.")
				continue
			}
			log.Info("server is shutting down to reload the certificates.")
			shutdownHappend = true
			err = s.httpServer.Shutdown(context.Background())
			if err != nil {
				log.Error(err, "encountering error when shutting down")
				return err
			}
			timer = time.Tick(wait.Jitter(defaultCertRefreshInterval, 0.1))
			go serveFn()
		case <-stop:
			return s.httpServer.Shutdown(context.Background())
		case e := <-errCh:
			// Don't exit when getting an http.ErrServerClosed error due to restarting the server.
			if shutdownHappend && e == http.ErrServerClosed {
				shutdownHappend = false
			} else if e != nil {
				log.Error(e, "server returns an unexpected error")
				return e
			}
		}
	}
}

// RefreshCert refreshes the certificate using Server's Provisioner if the certificate is expiring.
func (s *Server) RefreshCert() (bool, error) {
	cc, err := s.getClientConfig()
	if err != nil {
		return false, err
	}
	changed, err := s.certProvisioner.Provision(cert.Options{
		ClientConfig: cc,
		Objects:      s.webhookConfigurations,
	})
	if err != nil {
		return false, err
	}

	return changed, batchCreateOrReplace(s.Client, s.webhookConfigurations...)
}

var _ inject.Client = &Server{}

// InjectClient injects the client into the server
func (s *Server) InjectClient(c client.Client) error {
	s.Client = c
	for _, wh := range s.registry {
		if _, err := inject.ClientInto(c, wh.Handler()); err != nil {
			return err
		}
	}
	return nil
}

var _ inject.Decoder = &Server{}

// InjectDecoder injects the client into the server
func (s *Server) InjectDecoder(d atypes.Decoder) error {
	for _, wh := range s.registry {
		if _, err := inject.DecoderInto(d, wh.Handler()); err != nil {
			return err
		}
	}
	return nil
}
