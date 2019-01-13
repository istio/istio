{{ define "settings.yaml.tpl" }}
# Istio Galley settings file. This yaml file contains all component-level settings for starting and running
# Galley.
#
# The file is comprised of multiple top-level sections: general, processing and validation. The general
# section contains settings that are common for various Galley sub-components, or process-wide settings. The
# processing section contains settings that pertain to the configuration ingrestion, processing and
# distribution. The validation section contains settings for the admission controller webhook that Galley
# implements for integrating with the Kubernetes API Server for integration purposes.
#

# General settings that apply to the whole process or to multiple sub-components.
#
general:
  # The path to the Mesh config file. If the file is not found, default Mesh config values are used.
  # Defaults to "/etc/mesh-config/mesh"
  #
  # meshConfigFile: "/etc/mesh-config/mesh"

  # Path to the kube config file to use. If not specified, then in-cluster configuration will be used.
  # Defaults to in-cluster configuration.
  #
  # kubeConfig: "/home/.kube/config"

  # Port for exposing self-monitoring information.
  # Defaults to 9093
  #
  monitoringPort: {{ .Values.global.monitoringPort }}

  # Enable profiling for Galley
  # Defaults to false
  #
  # bool enable_profiling = 3;

  # Port for exposing profiling
  # Defaults to 9094
  #
  # pprofPort: 9094

  # Introspection (CtrlZ) related settings
  #
  introspection:
    # Indicates that introspection should be disabled.
    # Defaults to false
    #
    # disable: false

    # The IP port to use for ctrlz.
    # Defaults to 9876
    #
    # port: 9876

    # The IP address to listen on for ctrlz.
    # Defaults to 127.0.0.1
    #
    # address: "127.0.0.1"

  # Liveness probe related settings
  liveness:
    # Disable the probe
    # Defaults to false
    #
    # disable: false

    # The path to the file used for the existence.
    # Defaults to /healthLiveness
    #
    # path: "/healthLiveness"

    # The interval for updating the file's last modified time.
    # Defaults to 1s (1 second)
    #
    # interval: 1s

  readiness:
    # Disable the probe
    # Defaults to false
    #
    # disable: false

    # The path to the file used for the existence.
    # Defaults to /healthReadiness
    #
    # path: "/healthReadiness"

    # The interval for updating the file's last modified time.
    # Defaults to 1s (1 second)
    #
    # interval: 1s

# Config processing and distribution related settings.
#
processing:
  # DNS Domain suffix to use while constructing Ingress based resources.
  # Defaults to "cluster.local"
  #
  # domainSuffix: "cluster.local"

  # Settings for the MCP Service
  #
  server:
    # Disable the server.
    # Defaults to false
    #
{{- if not $.Values.global.useMCP }}
    disable: true
{{- end }}

    # Address to use for Galley's gRPC API, (e.g. tcp://127.0.0.1:9092 or unix:///path/to/file).
    # Defaults to tcp://0.0.0.0:9901
    #
    # address: "tcp://0.0.0.0:9901"

    # Maximum size of individual gRPC messages.
    # Defaults to 1024 * 1024
    #
    # maxReceivedMessageSize: 1048576

    # Maximum number of outstanding RPCs per connection.
    # Defaults to 1024
    #
    # maxConcurrentStreams: 1024

    # Enable gRPC tracing.
    # Defaults to false
    #
    # grpcTracing: false

    # Disable resource readiness checks. This allows Galley to start if not all resource types are supported
    # Defaults to false
    #
    disableResourceReadyCheck: false

    # Auth settings for the main MCP Server. The currently supported settings are mtls and insecure.
    # Defaults to mTLS
    #
    auth:
{{- if $.Values.global.controlPlaneSecurityEnabled}}
      # Use mutual TLS for Authn.
      mtls:
        # The path to the x509 certificate.
        # Defaults to /etc/certs/cert-chain.pem
        #
        # clientCertificate: "/etc/certs/cert-chain.pem"

        # The path to the x509 private key matching clientCertificate.
        # Defaults to "/etc/certs/key.pem"
        #
        # privateKey: "/etc/certs/key.pem"

        # The path to the x509 CA bundle file.
        # Defaults to "/etc/certs/root-cert.pem"
        #
        # caCertificates: "/etc/certs/root-cert.pem"

        # Optional whitelisting file for SPIFFE IDs. If the file is not specified, then all incoming connections
        # are allowed.
        # WARNING: This is an experimental feature, and both the setting and the file format can change in the
        # future.
        # Defaults to "".
        #
        # accessListFile: "/etc/config/accesslist.yaml"
{{- else }}
      insecure: {}
{{- end }}

  # Source section contains settings for configuration input sources. The source section can contain either
  # a kubernetes stanza, or a filesystem stanza. If neither is specific, then Kubernetes will be used as
  # the config source.
  #
  source:

    # Kubernetes stanza indicates that the configuration should be read from a Kubernetes API server.
    # The kubeconfig file that was specified in the general section will be used.
    #
    kubernetes:
      # resyncPeriod is the resync period used for Kubernetes wathers. A value of 0 indicates that on resync
      # should be done.
      # Defaults to 0s
      #
      # resyncPeriod: 0s

    # Filesystem stanza indicates that the configuration should be read from a directory in the local
    # filesystem.
    #
    # filesystem:
      # path indicates the folder from which yaml formatted config resource should be read from.
      # Required.
      #
      # path: "/etc/istio/config"

# validation section contains settings for the Kubernetes Admission controller that Galley uses to integrate
# and validate configuration.
#
validation:
  # Disables validation web hook.
  # Defaults to false
  #
  # disable: false

  # The path to the template file that is used to generate the validating webhook configuration for
  # self-registration.
  # Defaults to /etc/config/validatingwebhookconfiguration.yaml
  #
  # webhookConfigFile: /etc/config/validatingwebhookconfiguration.yaml

  # Port where the webhook is served. Per k8s admission registration requirements this should be 443 unless
  # there is only a single port for the service.
  # Defaults to 443
  #
  # webhookPort: 443

  # Name of the k8s validatingwebhookconfiguration resource to create, during self-registration of the
  # admission web hook.
  # Defaults to "istio-galley"
  #
  # webhookName: istio-galley

  # The name of the deployment resource for Galley itself. This is used when Galley self-registers its own
  # webhook configuration.
  # Defaults to "istio-galley"
  #
  # deploymentName: istio-galley

  # The namespace in which the Kubernetes deployment and service resources for Galley resides.
  # Defaults to  "istio-system"
  #
  # deploymentNamespace: istio-system

  # The name of the k8s service of the validation webhook. This is used to verify endpoint readiness before
  # registering the validatingwebhookconfiguration.
  # Defaults to "istio-galley"
  #
  # serviceName: istio-galley

  # TLS settings to use for the validation webhook.
  #
  tls:
    # The path to the x509 certificate.
    # Defaults to "/etc/certs/cert-chain.pem"
    #
    # clientCertificate: /etc/certs/cert-chain.pem

    # The path to the x509 private key matching clientCertificate.
    # Defaults to "/etc/certs/key.pem"
    #
    # privateKey: /etc/certs/key.pem

    # The path to the x509 CA bundle file.
    # Defaults to "/etc/certs/root-cert.pem"
    #
    # caCertificates: /etc/certs/root-cert.pem

{{ end }}
