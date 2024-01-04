package v1alpha3

import (
	"strconv"

	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"istio.io/istio/pkg/log"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/config"
	gatewaytool "istio.io/istio/pkg/config/gateway"
)

const enableH2 = "mse.ingress.alibabacloud.com/enable-h2"

type buildListenerFilterChainExtraOpts struct {
	gatewayConfig *config.Config
	meshConfig    *meshconfig.MeshConfig
	proxyConfig   *meshconfig.ProxyConfig
}

type TLSProtocolVersion string

const (
	tlsV10 TLSProtocolVersion = "TLSv1.0"
	tlsV11 TLSProtocolVersion = "TLSv1.1"
	tlsV12 TLSProtocolVersion = "TLSv1.2"
	tlsV13 TLSProtocolVersion = "TLSv1.3"
)

var (
	tlsProtocol = map[TLSProtocolVersion]networking.ServerTLSSettings_TLSProtocol{
		tlsV10: networking.ServerTLSSettings_TLSV1_0,
		tlsV11: networking.ServerTLSSettings_TLSV1_1,
		tlsV12: networking.ServerTLSSettings_TLSV1_2,
		tlsV13: networking.ServerTLSSettings_TLSV1_3,
	}
)

func Convert(protocol string) networking.ServerTLSSettings_TLSProtocol {
	return tlsProtocol[TLSProtocolVersion(protocol)]
}

func shouldDisableH2(extraOpts *buildListenerFilterChainExtraOpts) bool {
	if extraOpts == nil {
		return false
	}

	// explicitly enable/disable h2 in individual gateway crd.
	if extraOpts.gatewayConfig != nil && extraOpts.gatewayConfig.Annotations != nil {
		gateway := extraOpts.gatewayConfig.Spec.(*networking.Gateway)
		var hosts []string
		for _, server := range gateway.Servers {
			if gatewaytool.IsHTTPSServerWithTLSTermination(server) {
				hosts = append(hosts, server.Hosts...)
			}
		}
		if value, exist := extraOpts.gatewayConfig.Annotations[enableH2]; exist {
			if enable, err := strconv.ParseBool(value); err == nil && enable {
				log.Infof("Enable h2 for hosts %v", hosts)
				return false
			} else {
				log.Infof("Disable h2 for hosts %v", hosts)
				return true
			}
		}
	}

	// explicitly disable h2 in wide proxy config.
	if extraOpts.proxyConfig != nil && extraOpts.proxyConfig.DisableAlpnH2 {
		return true
	}

	return false
}

func applyTls(tlsSettings *networking.ServerTLSSettings, extraOpts *buildListenerFilterChainExtraOpts) *tls.TlsParameters {
	var tlsMinProtocolVersion tls.TlsParameters_TlsProtocol
	var tlsMaxProtocolVersion tls.TlsParameters_TlsProtocol
	var cipherSuite []string

	// First apply global config
	if extraOpts != nil && extraOpts.meshConfig != nil {
		globalConfig := extraOpts.meshConfig.MseIngressGlobalConfig
		if globalConfig != nil {
			if globalConfig.TlsMinProtocolVersion != "" {
				tlsMinProtocolVersion = convertTLSProtocol(convertServerTLSSettings(globalConfig.TlsMinProtocolVersion))
			}

			if globalConfig.TlsMaxProtocolVersion != "" {
				tlsMaxProtocolVersion = convertTLSProtocol(convertServerTLSSettings(globalConfig.TlsMaxProtocolVersion))
			}

			if len(globalConfig.TlsCipherSuites) > 0 {
				cipherSuite = globalConfig.TlsCipherSuites
			}
		}
	}

	// Then individual gateway
	if tlsSettings.MinProtocolVersion != 0 {
		tlsMinProtocolVersion = convertTLSProtocol(tlsSettings.MinProtocolVersion)
	}
	if tlsSettings.MaxProtocolVersion != 0 {
		tlsMaxProtocolVersion = convertTLSProtocol(tlsSettings.MaxProtocolVersion)
	}
	if len(tlsSettings.CipherSuites) > 0 {
		cipherSuite = tlsSettings.CipherSuites
	}

	if tlsMinProtocolVersion != tls.TlsParameters_TLS_AUTO ||
		tlsMaxProtocolVersion != tls.TlsParameters_TLS_AUTO ||
		len(cipherSuite) > 0 {
		return &tls.TlsParameters{
			TlsMinimumProtocolVersion: tlsMinProtocolVersion,
			TlsMaximumProtocolVersion: tlsMaxProtocolVersion,
			CipherSuites:              cipherSuite,
		}
	}

	return nil
}

func convertServerTLSSettings(protocol string) networking.ServerTLSSettings_TLSProtocol {
	return tlsProtocol[TLSProtocolVersion(protocol)]
}
