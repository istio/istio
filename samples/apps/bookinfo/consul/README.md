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

To build all images for the bookinfo sample for the consul adapter, run:

  ```
  ./build-docker-services.sh
  ```

To bring up all containers directly, from the `samples/apps/bookinfo/consul` directory run

  ```
  docker-compose up -d
  ```

This will pull images from docker hub to your local computing space.

Now you can see all the containers in the mesh by running `docker ps -a`.

NOTE: If Mac users experience an error starting the consul service in the `docker-compose up -d` command, 
open your `docker-compose.yaml` and overwrite the `consul` service with the following and re-run the `up -d` command :
```
  consul:
    image: gliderlabs/consul-server
    networks:
      envoymesh:
        aliases:
          - consul
    ports:
      - "8500:8500"
      - "53:8600/udp"
      - "8400:8400"
    environment:
      - SERVICE_IGNORE=1
    command: ["-bootstrap"]
```

To view the productpage webpage, open a web browser and enter `localhost:9081/productpage`.  

If you refresh the page several times, you should see different versions of reviews shown in productpage presented in a round robin style (red stars, black stars, no stars). If the webpage is not displaying properly, you may need to run `docker-compose restart discovery` to resolve a timing issue during start up.

NOTE: Mac users will have to run the following commands first prior to creating a rule:

```
kubectl config set-cluster mac --server=http://localhost:8080
kubectl config set-context mac --cluster=mac
kubectl config use-context mac
```

You can create basic routing rules using istioctl from the `samples/apps/bookinfo/consul` directory:

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
