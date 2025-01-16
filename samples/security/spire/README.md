# Integrating SPIRE as a CA through Envoy's SDS API

This sample deploys a setup of [SPIRE](https://github.com/spiffe/spire) (the SPIFFE Runtime Environment) as an example of integrating with [Envoy's SDS](https://www.envoyproxy.io/docs/envoy/latest/configuration/security/secret) API. For more information
on the SPIFFE specs, refer to the [SPIFFE Overview](https://spiffe.io/docs/latest/spiffe-about/overview/).

Once SPIRE is deployed and integrated with Istio, this sample deploys a modified version of the [sleep](/samples/sleep/README.md) service and validates that its [identity](https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/#spiffe-verifiable-identity-document-svid) was issued by SPIRE. Workload registration is handled by the [SPIRE Controller Manager](https://github.com/spiffe/spire-controller-manager).

See [Istio CA Integration with SPIRE](https://istio.io/latest/docs/ops/integrations/spire) for further details about this integration.

## Deploy the integration

1. Deploy SPIRE. For proper socket injection, this **must** be done prior to installing Istio in your cluster:

  ```bash
  $ kubectl apply -f spire-quickstart.yaml
  ```

1. Ensure that the deployment is completed before moving to the next step. This can be verified by waiting on the `spire-agent` pod to become ready:

  ```bash
  $ kubectl wait pod --for=condition=ready -n spire -l app=spire-agent
  ```

1. Use the configuration profile provided to install Istio (requires istioctl v1.14+):

  ```bash
  $ istioctl install -f istio-spire-config.yaml
  ```

1. Create a ClusterSPIFFEID to create a registration entry for all workloads with the `spiffe.io/spire-managed-identity: true` label:

  ```bash
  $ kubectl apply -f clusterspiffeid.yaml
  ```

1. Deploy the `sleep-spire.yaml` version of the [sleep](/samples/sleep/README.md) service, which injects the custom istio-agent template defined in `istio-spire-config.yaml` and has the `spiffe.io/spire-managed-identity: true` label.

  ```bash
  $ kubectl apply -f sleep-spire.yaml
  ```

1. Retrieve sleep's SVID identity document using the `istioctl proxy-config secret` command:

  ```bash
  $ istioctl pc secret deploy/sleep -o json | jq -r \
  '.dynamicActiveSecrets[0].secret.tlsCertificate.certificateChain.inlineBytes' | base64 --decode > chain.pem
  ```

1. Inspect the certificate content and verify that SPIRE was the issuer:

  ```bash
  $ openssl x509 -in chain.pem -text | grep SPIRE
      Subject: C = US, O = SPIRE, CN = sleep-5d6df95bbf-kt2tt
  ```

## Tear down

1.  Delete the spire installation:

  ```bash
  $ kubectl delete -f clusterspiffeid.yaml
  $ kubectl delete -f spire-quickstart.yaml
  ```
