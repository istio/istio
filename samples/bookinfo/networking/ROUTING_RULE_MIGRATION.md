## Routing Config Model Changes

The routing configuration resources in `v1alpha3` have changed as follows:

1. `RouteRule` -> `VirtualService`
2. `DestinationPolicy` -> `DestinationRule`
3. `EgressRule` -> `ServiceEntry`
4. `Ingress` -> `Gateway` (recommended to use)

A `VirtualService` configures the set of routes to a particular traffic destination host.
A `DestinationRule` configures the set of policies to be applied at a destination after routing has occurred.

Note that the `apiVersion` of these resources is also changed:

`apiVersion: config.istio.io/v1alpha2` -> `apiVersion: networking.istio.io/v1alpha3`

### Creating and deleting Route Rules

In the previous config model there could be many `RouteRule` resources for the same destination, where a `precedence` field was used
to control the order of evaluation. In `v1alpha3`, all rules for a given destination are stored together as an ordered
list in a single `VirtualService` resource. Therefore, adding a second and subsequent rules for a particular destination
is no longer done by creating a new `RouteRule` resource, but instead by updating the one-and-only `VirtualService` resource
for the destination.

old routing rules:
```
istioctl create -f my-second-rule-for-destination-abc.yaml
```
v1alpha3 routing rules:
```
istioctl replace -f my-updated-rules-for-destination-abc.yaml
```

>>> Proposal: we should add an `istioctl patch` command, to allow users to only provide the second rule

Deleting route rules other than the last one for a particular destination is also done using `istioctl replace`.
