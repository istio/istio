# Integrating SPIRE as a CA through Envoy's SDS API

This sample deploys a setup of [SPIRE](https://github.com/spiffe/spire) (the SPIFFE Runtime Environment) as an example of integrating with [Envoy's SDS](https://www.envoyproxy.io/docs/envoy/latest/configuration/security/secret) API. For more information
on the SPIFFE specs, refer to the [SPIFFE Overview](https://spiffe.io/docs/latest/spiffe-about/overview/).

Once SPIRE is deployed and integrated with Istio, this sample deploys a modified version of the [sleep](/samples/sleep/README.md) service and validates that its [identity](https://spiffe.io/docs/latest/spiffe-about/spiffe-concepts/#spiffe-verifiable-identity-document-svid) was issued by SPIRE. Workload registration is handled by the [SPIRE Controller Manager](https://github.com/spiffe/spire-controller-manager).

See [Istio CA Integration with SPIRE](https://istio.io/latest/docs/ops/integrations/spire) for further details about this integration.

## Deploy the integration

1. Deploy SPIRE. For proper socket injection, this **must** be done prior to installing Istio in your cluster:

   ```bash
   helm upgrade --install spire-crds spire-crds --repo https://spiffe.github.io/helm-charts-hardened/ --version 0.5.0
   helm upgrade --install spire spire --repo https://spiffe.github.io/helm-charts-hardened/  --version 0.24.0 \
    -n spire-server --create-namespace \
    --set global.spire.trustDomain="example.org" \
    --set spiffe-oidc-discovery-provider.enabled=false \
    --wait
   ```

1. Ensure that the deployment is completed before moving to the next step. This can be verified by waiting on the `spire-agent` pod to become ready:

   ```bash
   kubectl wait pod --for=condition=ready -l app.kubernetes.io/instance=spire -l app.kubernetes.io/name=agent -n spire-server
   ```

1. Use the configuration profile provided to install Istio (requires istioctl v1.14+):

    > [!IMPORTANT]
    > If you are using Kubernetes 1.33+ and have not disabled [native sidecars](https://istio.io/latest/blog/2023/native-sidecars/),
    > you must modify the injection template to use `initContainers`.

    ```bash
    sed -i 's/containers:/initContainers:/' istio-spire-config.yaml
    ```

    ```bash
    istioctl install -f istio-spire-config.yaml
    ```

1. Create a ClusterSPIFFEID to create a registration entry for all workloads with the `spiffe.io/spire-managed-identity: true` label:

   ```bash
   kubectl apply -f clusterspiffeid.yaml
   ```

1. Add the `spiffe.io/spire-managed-identity: true` label to the Ingress-gateway Deployment:

   ```bash
   kubectl patch deployment istio-ingressgateway -n istio-system -p '{"spec":{"template":{"metadata":{"labels":{"spiffe.io/spire-managed-identity": "true"}}}}}'
   ```

1. Deploy the `sleep-spire.yaml` version of the [sleep](/samples/sleep/README.md) service, which injects the custom istio-agent template defined in `istio-spire-config.yaml` and has the `spiffe.io/spire-managed-identity: true` label.

  If you have [automatic sidecar injection](https://istio.io/docs/setup/additional-setup/sidecar-injection/#automatic-sidecar-injection) enabled:

   ```bash
   kubectl apply -f sleep-spire.yaml
   ```

  Otherwise, manually inject the sidecar before applying:

   ```bash
   kubectl apply -f <(istioctl kube-inject -f sleep-spire.yaml)
   ```

1. Retrieve sleep's SVID identity document using the `istioctl proxy-config secret` command:

   ```bash
   export SLEEP_POD=$(kubectl get pod -l app=sleep -o jsonpath="{.items[0].metadata.name}")
   istioctl pc secret $SLEEP_POD -o json | jq -r \
   '.dynamicActiveSecrets[0].secret.tlsCertificate.certificateChain.inlineBytes' | base64 --decode > chain.pem
   ```

1. Inspect the certificate content and verify that SPIRE was the issuer:

   ```bash
   openssl x509 -in chain.pem -text | grep SPIRE
      Subject: C = US, O = SPIRE, CN = sleep-5d6df95bbf-kt2tt
   ```

## Tear down

1.  Delete all deployments and configurations for the SPIRE Agent, Server, and namespace:

    ```bash
    kubectl delete namespace spire
    ```

1.  Delete the ClusterRole, ClusterRoleBinding, Role, RoleBindings, ValidatingWebhookConfiguration, CSIDriver, and CustomResourceDefinition:

    ```bash
    kubectl delete clusterrole spire-server-cluster-role spire-agent-cluster-role manager-role
    kubectl delete clusterrolebinding spire-server-cluster-role-binding spire-agent-cluster-role-binding manager-role-binding
    kubectl delete role spire-server-role leader-election-role
    kubectl delete rolebinding spire-server-role-binding leader-election-role-binding
    kubectl delete ValidatingWebhookConfiguration spire-controller-manager-webhook
    kubectl delete csidriver csi.spiffe.io
    kubectl delete CustomResourceDefinition clusterspiffeids.spire.spiffe.io
    kubectl delete CustomResourceDefinition clusterfederatedtrustdomains.spire.spiffe.io
    ```
