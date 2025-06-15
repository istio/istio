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

package bootstrap

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/autoregistration"
	configaggregate "istio.io/istio/pilot/pkg/config/aggregate"
	"istio.io/istio/pilot/pkg/config/kube/crdclient"
	"istio.io/istio/pilot/pkg/config/kube/file"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	ingress "istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	istioCredentials "istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/leaderelection"
	"istio.io/istio/pilot/pkg/leaderelection/k8sleaderelection/k8sresourcelock"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/activenotifier"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/config/analysis/incluster"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/validation/agent"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/util/sets"
)

// URL schemes supported by the config store
type ConfigSourceAddressScheme string

const (
	// fs:///PATH will load local files. This replaces --configDir.
	// example fs:///tmp/configroot
	// PATH can be mounted from a config map or volume
	File ConfigSourceAddressScheme = "fs"
	// xds://ADDRESS - load XDS-over-MCP sources
	// example xds://127.0.0.1:49133
	XDS ConfigSourceAddressScheme = "xds"
	// k8s:// - load in-cluster k8s controller
	// example k8s://
	Kubernetes ConfigSourceAddressScheme = "k8s"
)

// initConfigController creates the config controller in the pilotConfig.
func (s *Server) initConfigController(args *PilotArgs) error {
	meshConfig := s.environment.Mesh()
	if len(meshConfig.ConfigSources) > 0 {
		// Using MCP for config.
		if err := s.initConfigSources(args); err != nil {
			return err
		}
	} else if args.RegistryOptions.FileDir != "" {
		// Local files - should be added even if other options are specified
		configController, err := file.NewController(
			args.RegistryOptions.FileDir,
			args.RegistryOptions.KubeOptions.DomainSuffix,
			collections.Pilot,
			args.RegistryOptions.KubeOptions,
		)
		if err != nil {
			return err
		}
		s.ConfigStores = append(s.ConfigStores, configController)
	} else {
		err := s.initK8SConfigStore(args)
		if err != nil {
			return err
		}
	}

	// If running in ingress mode (requires k8s), wrap the config controller.
	if hasKubeRegistry(args.RegistryOptions.Registries) && meshConfig.IngressControllerMode != meshconfig.MeshConfig_OFF {
		// Wrap the config controller with a cache.
		ic := ingress.NewController(
			s.kubeClient,
			s.environment.Watcher,
			args.RegistryOptions.KubeOptions,
			s.XDSServer,
		)
		s.ConfigStores = append(s.ConfigStores, ic)

		s.addTerminatingStartFunc("ingress status", func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaderElection(args.Namespace, args.PodName, leaderelection.IngressController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("Starting ingress status writer")
					ic.SetStatusWrite(true, s.statusManager)

					<-leaderStop
					log.Infof("Stopping ingress status writer")
					ic.SetStatusWrite(false, nil)
				}).
				Run(stop)
			return nil
		})
	}

	// Wrap the config controller with a cache.
	aggregateConfigController, err := configaggregate.MakeCache(s.ConfigStores)
	if err != nil {
		return err
	}
	s.configController = aggregateConfigController

	// Create the config store.
	s.environment.ConfigStore = aggregateConfigController

	// Defer starting the controller until after the service is created.
	s.addStartFunc("config controller", func(stop <-chan struct{}) error {
		go s.configController.Run(stop)
		return nil
	})

	return nil
}

func (s *Server) initK8SConfigStore(args *PilotArgs) error {
	if s.kubeClient == nil {
		return nil
	}
	configController := s.makeKubeConfigController(args)
	s.ConfigStores = append(s.ConfigStores, configController)
	tw := revisions.NewTagWatcher(s.kubeClient, args.Revision)
	s.addStartFunc("tag-watcher", func(stop <-chan struct{}) error {
		go tw.Run(stop)
		return nil
	})
	tw.AddHandler(func(sets.String) {
		s.XDSServer.ConfigUpdate(&model.PushRequest{
			Full:   true,
			Reason: model.NewReasonStats(model.TagUpdate),
			Forced: true,
		})
	})
	if features.EnableGatewayAPI {
		if s.statusManager == nil && features.EnableGatewayAPIStatus {
			s.initStatusManager(args)
		}
		args.RegistryOptions.KubeOptions.KrtDebugger = args.KrtDebugger
		gwc := gateway.NewController(s.kubeClient, s.kubeClient.CrdWatcher().WaitForCRD, args.RegistryOptions.KubeOptions, s.XDSServer)
		s.environment.GatewayAPIController = gwc
		s.ConfigStores = append(s.ConfigStores, s.environment.GatewayAPIController)

		// Use a channel to signal activation of per-revision status writer
		activatePerRevisionStatusWriterCh := make(chan struct{})
		// This check is for backwards compatibility with older(non per-revision) gateway status leader election.
		// By backward compatibility, we mean that during upgrade, new istiod deployment should not start writing status
		// until the old istiod deployment has acquired the leader election lock.
		// If the ConfigMap for leader election exists, the non-revision leader election will be joined by this deployment.
		// Once the leader election is acquired, it will signal the channel to activate the per-revision status writer.
		// So this function is helping in handing over the leader election from older non-revision leader election to
		// per-revision leader election status writer.
		// If the leader election ConfigMap does not exist, means there is no existing leader,
		// this func will close the channel to activate the per-revision status writer immediately.
		s.checkAndRunNonRevisionLeaderElectionIfRequired(args, activatePerRevisionStatusWriterCh)

		s.addTerminatingStartFunc("gateway status", func(stop <-chan struct{}) error {
			leaderelection.
				NewPerRevisionLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayStatusController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("waiting for gateway status writer activation")
					<-activatePerRevisionStatusWriterCh
					log.Infof("Starting gateway status writer for revision: %s", args.Revision)
					gwc.SetStatusWrite(true, s.statusManager)

					// Trigger a push so we can recompute status
					s.XDSServer.ConfigUpdate(&model.PushRequest{
						Full:   true,
						Reason: model.NewReasonStats(model.GlobalUpdate),
						Forced: true,
					})
					<-leaderStop
					log.Infof("Stopping gateway status writer")
					gwc.SetStatusWrite(false, nil)
				}).
				Run(stop)
			return nil
		})
		if features.EnableGatewayAPIDeploymentController {
			s.addTerminatingStartFunc("gateway deployment controller", func(stop <-chan struct{}) error {
				leaderelection.
					NewPerRevisionLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayDeploymentController, args.Revision, s.kubeClient).
					AddRunFunction(func(leaderStop <-chan struct{}) {
						// We can only run this if the Gateway CRD is created
						if s.kubeClient.CrdWatcher().WaitForCRD(gvr.KubernetesGateway, leaderStop) {
							tagWatcher := revisions.NewTagWatcher(s.kubeClient, args.Revision)
							controller := gateway.NewDeploymentController(s.kubeClient, s.clusterID, s.environment,
								s.webhookInfo.getWebhookConfig, s.webhookInfo.addHandler, tagWatcher, args.Revision, args.Namespace)
							// Start informers again. This fixes the case where informers for namespace do not start,
							// as we create them only after acquiring the leader lock
							// Note: stop here should be the overall pilot stop, NOT the leader election stop. We are
							// basically lazy loading the informer, if we stop it when we lose the lock we will never
							// recreate it again.
							s.kubeClient.RunAndWait(stop)
							go tagWatcher.Run(leaderStop)
							controller.Run(leaderStop)
						}
					}).
					Run(stop)
				return nil
			})
		}
	}
	if features.EnableAmbientStatus {
		statusWritingEnabled := activenotifier.New(false)
		args.RegistryOptions.KubeOptions.StatusWritingEnabled = statusWritingEnabled
		s.addTerminatingStartFunc("ambient status", func(stop <-chan struct{}) error {
			leaderelection.
				NewLeaseLeaderElection(args.Namespace, args.PodName, leaderelection.StatusController, args.Revision, s.kubeClient).
				AddRunFunction(func(leaderStop <-chan struct{}) {
					log.Infof("Starting ambient status writer")
					statusWritingEnabled.StoreAndNotify(true)
					<-leaderStop
					statusWritingEnabled.StoreAndNotify(false)
					log.Infof("Stopping ambient status writer")
				}).
				Run(stop)
			return nil
		})
	}
	if features.EnableAnalysis {
		if err := s.initInprocessAnalysisController(args); err != nil {
			return err
		}
	}
	var err error
	s.RWConfigStore, err = configaggregate.MakeWriteableCache(s.ConfigStores, configController)
	if err != nil {
		return err
	}
	s.XDSServer.WorkloadEntryController = autoregistration.NewController(configController, args.PodName, args.KeepaliveOptions.MaxServerConnectionAge)
	return nil
}

// initConfigSources will process mesh config 'configSources' and initialize
// associated configs.
func (s *Server) initConfigSources(args *PilotArgs) (err error) {
	for _, configSource := range s.environment.Mesh().ConfigSources {
		srcAddress, err := url.Parse(configSource.Address)
		if err != nil {
			return fmt.Errorf("invalid config URL %s %v", configSource.Address, err)
		}
		scheme := ConfigSourceAddressScheme(srcAddress.Scheme)
		switch scheme {
		case File:
			if srcAddress.Path == "" {
				return fmt.Errorf("invalid fs config URL %s, contains no file path", configSource.Address)
			}

			configController, err := file.NewController(
				srcAddress.Path,
				args.RegistryOptions.KubeOptions.DomainSuffix,
				collections.Pilot,
				args.RegistryOptions.KubeOptions,
			)
			if err != nil {
				return err
			}
			s.ConfigStores = append(s.ConfigStores, configController)
			log.Infof("Started File configSource %s", configSource.Address)
		case XDS:
			transportCredentials, err := s.getTransportCredentials(args, configSource.TlsSettings)
			if err != nil {
				return fmt.Errorf("failed to read transport credentials from config: %v", err)
			}
			xdsMCP, err := adsc.New(srcAddress.Host, &adsc.ADSConfig{
				InitialDiscoveryRequests: adsc.ConfigInitialRequests(),
				Config: adsc.Config{
					Namespace: args.Namespace,
					Workload:  args.PodName,
					Revision:  args.Revision,
					Meta: model.NodeMetadata{
						Generator: "api",
						// To reduce transported data if upstream server supports. Especially for custom servers.
						IstioRevision: args.Revision,
					}.ToStruct(),
					GrpcOpts: []grpc.DialOption{
						args.KeepaliveOptions.ConvertToClientOption(),
						grpc.WithTransportCredentials(transportCredentials),
					},
				},
			})
			if err != nil {
				return fmt.Errorf("failed to dial XDS %s %v", configSource.Address, err)
			}
			store := memory.Make(collections.Pilot)
			// TODO: enable namespace filter for memory controller
			configController := memory.NewController(store)
			configController.RegisterHasSyncedHandler(xdsMCP.HasSynced)
			xdsMCP.Store = configController
			err = xdsMCP.Run()
			if err != nil {
				return fmt.Errorf("MCP: failed running %v", err)
			}
			s.ConfigStores = append(s.ConfigStores, configController)
			log.Infof("Started XDS configSource %s", configSource.Address)
		case Kubernetes:
			if srcAddress.Path == "" || srcAddress.Path == "/" {
				err2 := s.initK8SConfigStore(args)
				if err2 != nil {
					log.Warnf("Error loading k8s: %v", err2)
					return err2
				}
				log.Infof("Started Kubernetes configSource %s", configSource.Address)
			} else {
				log.Warnf("Not implemented, ignore: %v", configSource.Address)
				// TODO: handle k8s:// scheme for remote cluster. Use same mechanism as service registry,
				// using the cluster name as key to match a secret.
			}
		default:
			log.Warnf("Ignoring unsupported config source: %v", configSource.Address)
		}
	}
	return nil
}

// initInprocessAnalysisController spins up an instance of Galley which serves no purpose other than
// running Analyzers for status updates.  The Status Updater will eventually need to allow input from istiod
// to support config distribution status as well.
func (s *Server) initInprocessAnalysisController(args *PilotArgs) error {
	if s.statusManager == nil {
		s.initStatusManager(args)
	}
	s.addStartFunc("analysis controller", func(stop <-chan struct{}) error {
		go leaderelection.
			NewLeaderElection(args.Namespace, args.PodName, leaderelection.AnalyzeController, args.Revision, s.kubeClient).
			AddRunFunction(func(leaderStop <-chan struct{}) {
				cont, err := incluster.NewController(leaderStop, s.RWConfigStore,
					s.kubeClient, args.Revision, args.Namespace, s.statusManager, args.RegistryOptions.KubeOptions.DomainSuffix)
				if err != nil {
					return
				}
				cont.Run(leaderStop)
			}).Run(stop)
		return nil
	})
	return nil
}

func (s *Server) makeKubeConfigController(args *PilotArgs) *crdclient.Client {
	opts := crdclient.Option{
		Revision:     args.Revision,
		DomainSuffix: args.RegistryOptions.KubeOptions.DomainSuffix,
		Identifier:   "crd-controller",
		KrtDebugger:  args.KrtDebugger,
	}

	schemas := collections.Pilot
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI()
	}
	schemas = schemas.Add(collections.Ingress)

	return crdclient.NewForSchemas(s.kubeClient, opts, schemas)
}

// getTransportCredentials attempts to create credentials.TransportCredentials from ClientTLSSettings in mesh config
// Implemented only for SIMPLE_TLS mode
// TODO:
//
//	Implement for MUTUAL_TLS/ISTIO_MUTUAL_TLS modes
func (s *Server) getTransportCredentials(args *PilotArgs, tlsSettings *v1alpha3.ClientTLSSettings) (credentials.TransportCredentials, error) {
	if err := agent.ValidateTLS(args.Namespace, tlsSettings); err != nil && tlsSettings.GetMode() == v1alpha3.ClientTLSSettings_SIMPLE {
		return nil, err
	}
	switch tlsSettings.GetMode() {
	case v1alpha3.ClientTLSSettings_SIMPLE:
		if len(tlsSettings.GetCredentialName()) > 0 {
			rootCert, err := s.getRootCertFromSecret(tlsSettings.GetCredentialName(), args.Namespace)
			if err != nil {
				return nil, err
			}
			tlsSettings.CaCertificates = string(rootCert.Cert)
			tlsSettings.CaCrl = string(rootCert.CRL)
		}
		if tlsSettings.GetInsecureSkipVerify().GetValue() || len(tlsSettings.GetCaCertificates()) == 0 {
			return credentials.NewTLS(&tls.Config{
				ServerName:         tlsSettings.GetSni(),
				InsecureSkipVerify: tlsSettings.GetInsecureSkipVerify().GetValue(), //nolint
			}), nil
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM([]byte(tlsSettings.GetCaCertificates())) {
			return nil, fmt.Errorf("failed to add ca certificate from configSource.tlsSettings to pool")
		}
		return credentials.NewTLS(&tls.Config{
			ServerName:         tlsSettings.GetSni(),
			InsecureSkipVerify: tlsSettings.GetInsecureSkipVerify().GetValue(), //nolint
			RootCAs:            certPool,
			VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
				return s.verifyCert(rawCerts, tlsSettings)
			},
		}), nil
	default:
		return insecure.NewCredentials(), nil
	}
}

// verifyCert verifies given cert against TLS settings like SANs and CRL.
func (s *Server) verifyCert(certs [][]byte, tlsSettings *v1alpha3.ClientTLSSettings) error {
	if len(certs) == 0 {
		return fmt.Errorf("no certificates provided")
	}
	cert, err := x509.ParseCertificate(certs[0])
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	if len(tlsSettings.SubjectAltNames) > 0 {
		sanMatchFound := false
		for _, san := range cert.DNSNames {
			if sanMatchFound {
				break
			}
			for _, name := range tlsSettings.SubjectAltNames {
				if san == name {
					sanMatchFound = true
					break
				}
			}
		}
		if !sanMatchFound {
			return fmt.Errorf("no matching SAN found")
		}
	}

	if len(tlsSettings.CaCrl) > 0 {
		crlData := []byte(strings.TrimSpace(tlsSettings.CaCrl))
		block, _ := pem.Decode(crlData)
		if block != nil {
			crlData = block.Bytes
		}
		crl, err := x509.ParseRevocationList(crlData)
		if err != nil {
			return fmt.Errorf("failed to parse CRL: %w", err)
		}
		for _, revokedCert := range crl.RevokedCertificateEntries {
			if cert.SerialNumber.Cmp(revokedCert.SerialNumber) == 0 {
				return fmt.Errorf("certificate is revoked")
			}
		}
	}

	return nil
}

// getRootCertFromSecret fetches a map of keys and values from a secret with name in namespace
func (s *Server) getRootCertFromSecret(name, namespace string) (*istioCredentials.CertInfo, error) {
	secret, err := s.kubeClient.Kube().CoreV1().Secrets(namespace).Get(context.Background(), name, v1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get credential with name %v: %v", name, err)
	}
	return kube.ExtractRoot(secret.Data)
}

// checkAndRunNonRevisionLeaderElectionIfRequired checks the ConfigMap for leader election and runs non-revision leader election
// to figure out when the older leader has released the lock.
// Once the lock is acquired, it will close the activateCh channel to signal the per-revision leader to start writing status.
func (s *Server) checkAndRunNonRevisionLeaderElectionIfRequired(args *PilotArgs, activateCh chan struct{}) {
	cm, err := s.kubeClient.Kube().CoreV1().ConfigMaps(args.Namespace).Get(context.Background(), leaderelection.GatewayStatusController, v1.GetOptions{})
	if err != nil {
		// ConfigMap does not exist, so per-revision leader election should be active
		close(activateCh)
		return
	}
	leaderAnn, ok := cm.Annotations[k8sresourcelock.LeaderElectionRecordAnnotationKey]
	if ok {
		var leaderInfo struct {
			HolderIdentity string `json:"holderIdentity"`
		}
		if err := json.Unmarshal([]byte(leaderAnn), &leaderInfo); err == nil {
			if leaderInfo.HolderIdentity != "" {
				// Non-revision leader election should run, per-revision should be waiting for activation
				s.addTerminatingStartFunc("gateway status", func(stop <-chan struct{}) error {
					leaderelection.
						NewLeaderElection(args.Namespace, args.PodName, leaderelection.GatewayStatusController, args.Revision, s.kubeClient).
						AddRunFunction(func(leaderStop <-chan struct{}) {
							// now that we have the leader lock, we can activate the per-revision status writer
							close(activateCh)
							log.Infof("Activating gateway status writer")
							<-leaderStop
						}).
						Run(stop)
					return nil
				})
				return
			}
		}
	}
	// If annotation missing or holderIdentity is blank, per-revision leader election should be active
	close(activateCh)
}
