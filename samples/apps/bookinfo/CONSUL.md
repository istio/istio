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

 * Download and install Kubernetes CLI [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version
  1.7.3 or higher.

 * Download istioctl from Istio's [releases page](https://github.com/istio/istio/releases) or build from
 source in Istio Pilot repository

## Bookinfo Demo

The ingress controller is still under construction, routing functionalities can be tested by curling a service container directly.

First step is to configure kubectl to use the apiserver created in the steps below:

```
kubectl config set-cluster local --server=http://172.28.0.13:8080
kubectl config set-context local --cluster=local
kubectl config use-context local
```

To build all images for the bookinfo sample for the consul adapter, navigate to `samples/apps/bookinfo/src` directory and run:

  ```
  ./build-docker-services.sh
  ```

To bring up all containers directly, from the `samples/apps/bookinfo/src` directory run

  ```
  docker-compose up -d
  ```

This will pull images from docker hub to your local computing space.

Now you can see all the containers in the mesh by running `docker ps -a`.

To view the productpage webpage, retrieve the productpage container IP using the `docker inspect`command:

```
$ docker inspect -f '{{.NetworkSettings.Networks.src_envoymesh.IPAddress}}' src_productpage-v1_1
172.28.0.6
```

Open a web browser and enter the productpage IP as follows: `<IP>:9080/productpage`

If you refresh the page several times, you should see different versions of reviews shown in productpage presented in a round robin style (red stars, black stars, no stars). If the webpage is not displaying properly, you may need to run `docker-compose restart discovery` to resolve a timing issue during start up.

You can create basic routing rules using istioctl from the `/bookinfo` directory:

```
istioctl create -f consul-reviews-v1.yaml
```

This will set a rule to display no stars by default.

```
istioctl replace -f consul-reviews-v3.yaml
```

This will set a rule to display red stars by default.

```
istioctl create -f consul-content-rule.yaml
```

This will display black stars - but only if you login as user `jason` (no password), otherwise only red stars will be shown.

If you are an advanced consul and docker network user, you may choose to configure your own envoymesh network dns and consul port mapping and istio-apiserver ipv4_address in the `docker-compose.yaml` file.
