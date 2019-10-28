## Controller local test
### Run inside the cluster

Step 1 and Step 2 are required only when you make changes locally
1. Run make docker.all to push image with your env $HUB and $TAG set.

1. Update the `image:` field in deploy/operator.yaml to point to your image.

1. Run `kubectl apply -k deploy/`.

### Run outside the cluster
1. Set env $WATCH_NAMESPACE and $LEADER_ELECTION_NAMESPACE, default value is istio-operator

1. From operator root directory, run `go run ./cmd/manager/*.go server `

To use Remote debugging with IntelliJ, replace above step 2 with following:

1. From ./cmd/manager path run
`
dlv debug --headless --listen=:2345 --api-version=2 -- server
`.

1. In IntelliJ, create a new Go Remote debug configuration with default settings.

1. Start debugging process and verify it is working. For example, try adding a breakpoint at Reconcile logic and apply a new CR.