package webhook

import (
	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type Analyzer struct{}

var _ analysis.Analyzer = &Analyzer{}

func (a *Analyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "webhook.Analyzer",
		Description: "Checks the validity of Istio webhooks",
		Inputs: collection.Names{
			collections.K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations.Name(),
		},
	}
}

func (a *Analyzer) Analyze(context analysis.Context) {
	context.ForEach(collections.K8SAdmissionregistrationK8SIoV1Mutatingwebhookconfigurations.Name(), func(resource *resource.Instance) bool {
		log.Errorf("howardjohn: analyzed %v", resource.Metadata.FullName)
		return true
	})
}
//
//func (a *Analyzer) analyzeProtocolAddresses(resource *resource.Instance, context analysis.Context) {
//	se := resource.Message.(*v1alpha3.ServiceEntry)
//
//	if se.Addresses == nil {
//		for index, port := range se.Ports {
//			if port.Protocol == "" || port.Protocol == "TCP" {
//				message := msg.NewServiceEntryAddressesRequired(resource)
//
//				if line, ok := util.ErrorLine(resource, fmt.Sprintf(util.ServiceEntryPort, index)); ok {
//					message.Line = line
//				}
//
//				context.Report(collections.IstioNetworkingV1Alpha3Serviceentries.Name(), message)
//			}
//		}
//	}
//}
//
