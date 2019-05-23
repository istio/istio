# Testing Citadel Agent

## Getting Started for the Test

Starting the Citadel Agent first

```bash
go build  -o agent \
  ${GOPATH}/src/istio.io/istio/security/cmd/node_agent_k8s/main.go

export AGENT_UDS_PATH=$(mktemp /tmp/citadel-agent.XXXX)
echo "Citadel Agent UDS Path ${AGENT_UDS_PATH}"
CA_PROVIDER='VaultCA' CA_ADDR="https://34.83.129.211:8200" ./agent \
  --workloadUDSPath=${AGENT_UDS_PATH}
```

Run the test in a separate terminal window

```bash
go test -v -istio.testing.citadelagent.skip=false \
  -istio.testing.citadelagent.uds=${AGENT_UDS_PATH} \
  ${GOPATH}/src/istio.io/istio/security/testing/agent
```

## Deploy in Kuberentes

```bash
make clean && make docker.sdsclient && docker push ${HUB}/sdsclient:${TAG}
kubectl apply -f sdsc.yaml

# Key/Cert are printed in the log.
kubectl logs <pod-id>
```

## Future Work

- Refactor `node_agent_k8s` binary to be able to start the server from the test.
- Build Docker image for the testing binary and add to istio/tools for release qualification.
- More certificate validation options in the sdsclient.
- More Envoy version/nonce/resource_name feature implemented in the sdsclient.