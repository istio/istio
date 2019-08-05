
// GENERATED FILE -- DO NOT EDIT

package annotation

// Instance describes a single resource annotation
type Instance struct {
	// The name of the annotation.
	Name string

	// Description of the annotation.
	Description string

	// Hide the existence of this annotation when outputting usage information.
	Hidden bool

	// Mark this annotation as deprecated when generating usage information.
	Deprecated bool
}

var (
	
		AlphaCanonicalServiceAccounts = Instance {
          Name: "alpha.istio.io/canonical-serviceaccounts",
          Description: "Specifies the non-Kubernetes service accounts that are "+
                        "allowed to run this service. NOTE This API is Alpha and "+
                        "has no stability guarantees.",
          Hidden: true,
          Deprecated: false,
        }
	
		AlphaIdentity = Instance {
          Name: "alpha.istio.io/identity",
          Description: "Identity for the workload. NOTE This API is Alpha and has "+
                        "no stability guarantees.",
          Hidden: true,
          Deprecated: false,
        }
	
		AlphaKubernetesServiceAccounts = Instance {
          Name: "alpha.istio.io/kubernetes-serviceaccounts",
          Description: "Specifies the Kubernetes service accounts that are "+
                        "allowed to run this service on the VMs. NOTE This API is "+
                        "Alpha and has no stability guarantees.",
          Hidden: true,
          Deprecated: false,
        }
	
		IoKubernetesIngressClass = Instance {
          Name: "kubernetes.io/ingress.class",
          Description: "Annotation on an Ingress resources denoting the class of "+
                        "controllers responsible for it.",
          Hidden: false,
          Deprecated: false,
        }
	
		AlphaNetworkingEndpointsVersion = Instance {
          Name: "networking.alpha.istio.io/endpointsVersion",
          Description: "Added to synthetic ServiceEntry resources to provide the "+
                        "raw resource version from the most recent k8s Endpoints "+
                        "update (if available). NOTE This API is Alpha and has no "+
                        "stability guarantees.",
          Hidden: true,
          Deprecated: false,
        }
	
		AlphaNetworkingNotReadyEndpoints = Instance {
          Name: "networking.alpha.istio.io/notReadyEndpoints",
          Description: "Added to synthetic ServiceEntry resources to provide the "+
                        "'NotReadyAddresses' from the Kubernetes Endpoints "+
                        "resource. The value is a comma-separated list of IP:port. "+
                        "NOTE This API is Alpha and has no stability guarantees.",
          Hidden: true,
          Deprecated: false,
        }
	
		AlphaNetworkingServiceVersion = Instance {
          Name: "networking.alpha.istio.io/serviceVersion",
          Description: "Added to synthetic ServiceEntry resources to provide the "+
                        "raw resource version from the most recent k8s Service "+
                        "update. This will always be available for synthetic "+
                        "service entries. NOTE This API is Alpha and has no "+
                        "stability guarantees.",
          Hidden: true,
          Deprecated: false,
        }
	
		NetworkingExportTo = Instance {
          Name: "networking.istio.io/exportTo",
          Description: "Specifies the namespaces to which this service should be "+
                        "exported to. A value of '*' indicates it is reachable "+
                        "within the mesh '.' indicates it is reachable within its "+
                        "namespace.",
          Hidden: false,
          Deprecated: false,
        }
	
		PolicyCheck = Instance {
          Name: "policy.istio.io/check",
          Description: "Determines the policy for behavior when unable to connect "+
                        "to Mixer. If not set, FAIL_CLOSE is set, rejecting "+
                        "requests.",
          Hidden: false,
          Deprecated: false,
        }
	
		PolicyCheckBaseRetryWaitTime = Instance {
          Name: "policy.istio.io/checkBaseRetryWaitTime",
          Description: "Base time to wait between retries, will be adjusted by "+
                        "backoff and jitter. In duration format. If not set, this "+
                        "will be 80ms.",
          Hidden: false,
          Deprecated: false,
        }
	
		PolicyCheckMaxRetryWaitTime = Instance {
          Name: "policy.istio.io/checkMaxRetryWaitTime",
          Description: "Maximum time to wait between retries to Mixer. In "+
                        "duration format. If not set, this will be 1000ms.",
          Hidden: false,
          Deprecated: false,
        }
	
		PolicyCheckRetries = Instance {
          Name: "policy.istio.io/checkRetries",
          Description: "The maximum number of retries on transport errors to "+
                        "Mixer. If not set, this will be 0, indicating no retries.",
          Hidden: false,
          Deprecated: false,
        }
	
		PolicyLang = Instance {
          Name: "policy.istio.io/lang",
          Description: "Selects the attribute expression langauge runtime for "+
                        "Mixer..",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatusReadinessApplicationPorts = Instance {
          Name: "readiness.status.sidecar.istio.io/applicationPorts",
          Description: "Specifies the list of ports exposed by the application "+
                        "container. Used by the istio-proxy readiness probe to "+
                        "determine that Envoy is configured and ready to receive "+
                        "traffic.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatusReadinessFailureThreshold = Instance {
          Name: "readiness.status.sidecar.istio.io/failureThreshold",
          Description: "Specifies the failure threshold for the istio-proxy "+
                        "readiness probe.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatusReadinessInitialDelaySeconds = Instance {
          Name: "readiness.status.sidecar.istio.io/initialDelaySeconds",
          Description: "Specifies the initial delay (in seconds) for the "+
                        "istio-proxy readiness probe.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatusReadinessPeriodSeconds = Instance {
          Name: "readiness.status.sidecar.istio.io/periodSeconds",
          Description: "Specifies the period (in seconds) for the istio-proxy "+
                        "readiness probe.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarBootstrapOverride = Instance {
          Name: "sidecar.istio.io/bootstrapOverride",
          Description: "Specifies an alternative Envoy bootstrap configuration "+
                        "file.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarComponentLogLevel = Instance {
          Name: "sidecar.istio.io/componentLogLevel",
          Description: "Specifies the component log level for Envoy.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarControlPlaneAuthPolicy = Instance {
          Name: "sidecar.istio.io/controlPlaneAuthPolicy",
          Description: "Specifies the auth policy used by the Istio control "+
                        "plane. If NONE, traffic will not be encrypted. If "+
                        "MUTUAL_TLS, traffic between istio-proxy sidecars will be "+
                        "wrapped into mutual TLS connections.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarDiscoveryAddress = Instance {
          Name: "sidecar.istio.io/discoveryAddress",
          Description: "Specifies the XDS discovery address to be used by the "+
                        "istio-proxy sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarInject = Instance {
          Name: "sidecar.istio.io/inject",
          Description: "Specifies whether or not an istio-proxy sidecar should be "+
                        "automatically injected into the workload.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarInterceptionMode = Instance {
          Name: "sidecar.istio.io/interceptionMode",
          Description: "Specifies the mode used to redirect inbound connections "+
                        "to Envoy (REDIRECT or TPROXY).",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarLogLevel = Instance {
          Name: "sidecar.istio.io/logLevel",
          Description: "Specifies the log level for Envoy.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarProxyCPU = Instance {
          Name: "sidecar.istio.io/proxyCPU",
          Description: "Specifies the requested CPU setting for the istio-proxy "+
                        "sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarProxyImage = Instance {
          Name: "sidecar.istio.io/proxyImage",
          Description: "Specifies the Docker image to be used by the istio-proxy "+
                        "sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarProxyMemory = Instance {
          Name: "sidecar.istio.io/proxyMemory",
          Description: "Specifies the requested memory setting for the "+
                        "istio-proxy sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarRewriteAppHTTPProbers = Instance {
          Name: "sidecar.istio.io/rewriteAppHTTPProbers",
          Description: "Rewrite HTTP readiness and liveness probes to be "+
                        "redirected to istio-proxy sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatsInclusionPrefixes = Instance {
          Name: "sidecar.istio.io/statsInclusionPrefixes",
          Description: "Specifies the comma separated list of prefixes of the "+
                        "stats to be emitted by Envoy.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatsInclusionRegexps = Instance {
          Name: "sidecar.istio.io/statsInclusionRegexps",
          Description: "Specifies the comma separated list of regexes the stats "+
                        "should match to be emitted by Envoy.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatsInclusionSuffixes = Instance {
          Name: "sidecar.istio.io/statsInclusionSuffixes",
          Description: "Specifies the comma separated list of suffixes of the "+
                        "stats to be emitted by Envoy.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatus = Instance {
          Name: "sidecar.istio.io/status",
          Description: "Generated by istio-proxy sidecar injection that indicates "+
                        "the status of the operation. Includes a version hash of "+
                        "the executed template, as well as names of injected "+
                        "resources.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarUserVolume = Instance {
          Name: "sidecar.istio.io/userVolume",
          Description: "Specifies one or more user volumes (as a JSON array) to "+
                        "be added to the istio-proxy sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarUserVolumeMount = Instance {
          Name: "sidecar.istio.io/userVolumeMount",
          Description: "Specifies one or more user volume mounts (as a JSON "+
                        "array) to be added to the istio-proxy sidecar.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarStatusPort = Instance {
          Name: "status.sidecar.istio.io/port",
          Description: "Specifies the HTTP status Port for the istio-proxy "+
                        "sidecar. If zero, the istio-proxy will not provide "+
                        "status.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarTrafficExcludeInboundPorts = Instance {
          Name: "traffic.sidecar.istio.io/excludeInboundPorts",
          Description: "A comma separated list of inbound ports to be excluded "+
                        "from redirection to Envoy. Only applies when all inbound "+
                        "traffic (i.e. '*') is being redirected.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarTrafficExcludeOutboundIPRanges = Instance {
          Name: "traffic.sidecar.istio.io/excludeOutboundIPRanges",
          Description: "A comma separated list of IP ranges in CIDR form to be "+
                        "excluded from redirection. Only applies when all outbound "+
                        "traffic (i.e. '*') is being redirected.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarTrafficExcludeOutboundPorts = Instance {
          Name: "traffic.sidecar.istio.io/excludeOutboundPorts",
          Description: "A comma separated list of outbound ports to be excluded "+
                        "from redirection to Envoy.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarTrafficIncludeInboundPorts = Instance {
          Name: "traffic.sidecar.istio.io/includeInboundPorts",
          Description: "A comma separated list of inbound ports for which traffic "+
                        "is to be redirected to Envoy. The wildcard character '*' "+
                        "can be used to configure redirection for all ports. An "+
                        "empty list will disable all inbound redirection.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarTrafficIncludeOutboundIPRanges = Instance {
          Name: "traffic.sidecar.istio.io/includeOutboundIPRanges",
          Description: "A comma separated list of IP ranges in CIDR form to "+
                        "redirect to envoy (optional). The wildcard character '*' "+
                        "can be used to redirect all outbound traffic. An empty "+
                        "list will disable all outbound redirection.",
          Hidden: false,
          Deprecated: false,
        }
	
		SidecarTrafficKubevirtInterfaces = Instance {
          Name: "traffic.sidecar.istio.io/kubevirtInterfaces",
          Description: "A comma separated list of virtual interfaces whose "+
                        "inbound traffic (from VM) will be treated as outbound.",
          Hidden: false,
          Deprecated: false,
        }
	
)