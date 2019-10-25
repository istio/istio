## Controller local test
### Run inside the cluster

Step 1 and Step 2 are required only when you make changes locally
1. Run make docker.all to push image with your env $HUB and $TAG set.

1. Update the `image:` field in deploy/operator.yaml to point to your image.

1. Run `kubectl apply -k deploy/`.

### Run outside the cluster

1. Install [Operator SDK CLI](#https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md)

1. Then run
```
operator-sdk up local --namespace=istio-operator --operator-flags "server"
```

