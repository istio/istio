# Testing Citadel Agent

## Getting Started

Starting the Citadel Agent first

```bash
go build  -o agent \
  ${GOPATH}/src/istio.io/istio/security/cmd/node_agent_k8s/main.go

export AGENT_UDS_PATH=$(mktemp /tmp/citadel-agent.XXXX)
CA_PROVIDER='VaultCA' CA_ADDR="https://34.83.129.211:8200" ./agent \
  --workloadUDSPath=${AGENT_UDS_PATH}
```

Start the ADSC client

```bash
export CITADEL_SERVER_UDS_PATH=${AGENT_UDS_PATH}
export CITADEL_SKIP_AGENT_TEST="false"
go test -v ${GOPATH}/src/istio.io/istio/security/testing/agent
```