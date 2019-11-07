# Consul Adapter for Istio on Docker

Make Istio run in docker environment by integrating Consul as a service registry.

## Design Principle

The key issue is how to implement the ServiceDiscovery interface functions in Istio.
This platform adapter uses Consul Server to help Istio monitor service instances running in the underlying platform.
When a service instance is brought up in docker, the [Registrator](http://gliderlabs.github.io/registrator/latest/)
automatically registers the service in Consul.

Note that Istio pilot is running inside each app container so as to coordinate Envoy and the service mesh.

## Prerequisites

* Clone Istio Pilot [repo](https://github.com/istio/pilot) (required only if building images locally)

* Download istioctl from Istio's [releases page](https://github.com/istio/istio/releases) or build from
source in Istio Pilot repository

## Bookinfo Demo

The ingress controller is still under construction, routing functionalities can be tested by curling a service container directly.

To build all images for the bookinfo sample for the consul adapter, run:

```bash
samples/bookinfo/src/build-docker-services.sh
```

For Linux users, configure the `DOCKER_GATEWAY` environment variable

```bash
export DOCKER_GATEWAY=172.28.0.1:
```

To bring up the control plane containers directly, from the root repository directory run

```bash
docker-compose -f install/consul/istio.yaml up -d
```

This will pull images from docker hub to your local computing space.

Now you can see all the containers in the mesh by running `docker ps -a`.

If the webpage is not displaying properly, you may need to run the previous command once more to resolve a timing issue during start up.

To bring up the app containers, from the `samples/bookinfo/consul` directory run

```bash
docker-compose -f bookinfo.yaml up -d
```

To view the productpage webpage, open a web browser and enter `localhost:9081/productpage`.

If you refresh the page several times, you should see different versions of reviews shown in productpage presented in a round robin style (red stars, black stars, no stars).

Configure `kubectl` to use the locally mapped port for the Istio api server

```bash
kubectl config set-context istio --cluster=istio
kubectl config set-cluster istio --server=http://localhost:8080
kubectl config use-context istio
```

If you are an advanced consul and docker network user, you may choose to configure your own envoymesh network dns and consul port mapping and istio-apiserver ipv4_address in the `istio.yaml` file.
