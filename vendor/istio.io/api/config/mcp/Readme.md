# Mesh Configuration Protocol Protos

This folder contains the proto buffers used by the Mesh Configuration Protocol, 
an [XDS](https://github.com/envoyproxy/data-plane-api/blob/master/XDS_PROTOCOL.md#streaming-grpc-subscriptions)
inspired protocol for transferring configuration among Istio components 
during runtime.

The protocol buffers in this folder are not used for configuring Istio. 
Instead, they define an internal protocol through which the configuration proto
instances can be delivered to components, such as Mixer and Pilot.
   
The server side of the protocol is implemented in Galley, Istio's config 
aggregation and distribution component.
