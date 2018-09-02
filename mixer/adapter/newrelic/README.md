# gRPC adapter for New Relic
This adapter is used to upload telemetry data to NewRelic backend. It is different from compiled in adapters. You can run it as a separated process out of mixer process. The adapter communicates with Mixer, parse and send metric data to the real backend -- NewRelic.

## prerequisite
The data in New Relic will be presented as Insight Events. So you need to know the account id and get an insert key. You can follow this doc to [get your account id](https://docs.newrelic.com/docs/accounts/install-new-relic/account-setup/account-id). And follow this doc to [get an insert key](https://docs.newrelic.com/docs/insights/insights-data-sources/custom-data/insert-custom-events-insights-api#register).

## build
Assume the working directory is $ISTIO/mixer/adapter/newrelic.

```
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o nristioadapter
```

## running as a binary file
You can run the adapter outside the k8s. Just make sure the network policy allows your k8s cluster to talk with gRPC adapter. Suppose that the compiled executable file name is `nristioadapter`. you should start up the adapter like this.

```
./nristioadapter --maxworkers 1024 --port 12345
```

The adapter uses a job/worker pattern to handle the request from mixer. Parameter `--maxworkers` points how many workers in the pool. Parameter `--port` shows which port should the adapter listen on.

## apply config model in istio
You can reference sample_operator_cfg.yaml. Replace the `address` section below.

```
connection:
  address: "[::]:49951"
```

Replace the `[::]` with hostname or IP address of the host which adapter is running at. Replace `49951` with the real port of adapter.

## deploy as a POD inside k8s
The adapter can work as a POD in k8s. You can refer the yaml under [docker directory](./docker).
Firstly you need to create a [secret](./docker/secret.yaml) for account id and insert key. Account id and insert key should be base64 encoded. Then you can apply the secret.yaml, deployment.yaml and service.yaml.

Note: if you deploy the adapter as POD, you should config the handler using service name instead of hostname/IP address as binary file mode. It should follow [k8s dns pattern](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#services):

$servicename.$namespace.svc.cluster.local:$port

eg.
```
connection:
  address:nristio.default.svc.cluster.local:8888
```

## validate

Access any endpoint inside k8s with an envoy proxy side car. If the configuration is correct there should be some telemetry data generated. Login to your New Relic account and jump to insights view. Click `data explorer` and check the drop down list of events. If there is a new Custom Event called `IstioMetrics` then everything works.
