# Integrating SPIRE as a CA through Envoy's SDS API

This sample deploys a setup of [SPIRE](https://github.com/spiffe/spire) (the SPIFFE Runtime Environment) as an example of integrating with [Envoy's SDS](https://www.envoyproxy.io/docs/envoy/latest/configuration/security/secret) API. For more information
on the SPIFFE specs, refer to the [SPIFFE Overview](https://spiffe.io/docs/latest/spiffe-about/overview/).

Once SPIRE is deployed and integrated with Istio, this sample deploys a modified version of the [sleep](/samples/sleep/README.md) service and validates that its [identity](https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/#spiffe-verifiable-identity-document-svid) was issued by SPIRE. Workload registration is automatically handled by the [k8s-workload-registrar](https://github.com/spiffe/spire/blob/main/support/k8s/k8s-workload-registrar/README.md).

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

1. Deploy the `sleep-spire.yaml` version of the [sleep](/samples/sleep/README.md) service, which injects the custom istio-agent template defined in `istio-spire-config.yaml`.

  If you have [automatic sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection) enabled:

  ```bash
  $ kubectl apply -f sleep-spire.yaml
  ```

  Otherwise, manually inject the sidecar before applying:

  ```bash
  $ kubectl apply -f <(istioctl kube-inject -f sleep-spire.yaml)
  ```

1. Retrieve sleep's SVID identity document using the `istioctl proxy-config secret` command:

  ```bash
  $ export SLEEP_POD=$(kubectl get pod -l app=sleep -o jsonpath="{.items[0].metadata.name}")
  $ istioctl pc secret $SLEEP_POD -o json | jq -r \
  '.dynamicActiveSecrets[0].secret.tlsCertificate.certificateChain.inlineBytes' | base64 --decode > chain.pem
  ```

1. Inspect the certificate content and verify that SPIRE was the issuer:

  ```bash
  $ openssl x509 -in chain.pem -text | grep SPIRE
      Subject: C = US, O = SPIRE, CN = sleep-5d6df95bbf-kt2tt
  ```

## Tear down

1.  Delete all deployments and configurations for the SPIRE Agent, Server, and namespace:

  ```bash
  $ kubectl delete namespace spire
  ```

1.  Delete the ClusterRole and ClusterRoleBinding:

  ```bash
  $ kubectl delete clusterrole spire-server-trust-role spire-agent-cluster-role
  $ kubectl delete clusterrolebinding spire-server-trust-role-binding spire-agent-cluster-role-binding
  ```
