---
layout: page
title: Hello World!
permalink: /docs/demo-helloworld.html
redirect_from: /docs/demo/helloworld/
category: Demo
order: 2
---

This demo starts two versions of a helloworld microservice each with two
instances, in order to demonstrate how Istio can be used to split
incoming traffic between the two versions. You can define the proportion of
traffic to each microservice as a percentage.

## Deploy and Scale the App

The commands to deploy the helloworld demo application are as follows:

1. Bring up the containers:

   ```bash
   kubectl create -f examples/helloworld.yaml
   ```

   The above command automatically launches two instances of each version of
   `helloworld`.

1. Set the gateway environment variable:

   ```bash
   export GATEWAY_URL=$(minikube ip):32000
   ```

### List the Services in the App

You can view the microservices that are running using the following command:

```bash
TODO: command to list running istio services
```
    
The expected output is the following:

```bash
TODO: command output showing 2 v1 and 2 v2 instances of helloworld service
```

There are 4 instances of the helloworld service. Two are instances of
version "v1" and the other two belong to version "v2".

## Version-based routing

1. Lets send all traffic to the v1 version of helloworld. Run the following command:

   ```bash
   TODO: command to route all traffic to v1 version of helloworld
   ```

1. We can confirm the routes are set by running the following command:

   ```bash
   TODO: command to show the current routes
   ```

   You should see the following output:

   ```bash
   TODO: command output showing all helloworld traffic is to v1
   ```

1. Confirm that all traffic is being directed to the v1 instance, by running the following cURL command multiple times:

   ```bash
   curl http://$GATEWAY_URL/helloworld/hello
   ```

   You can see that the traffic is continually routed between the v1 instances only, in a random fashion:

   ```bash
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-p8909
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-qwpex
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-p8909
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-qwpex
   ...
   ```

1. Next, we will split traffic between helloworld v1 and v2

   Run the following command to send 25% of the traffic to helloworld v2, leaving the rest (75%) on v1:
    
   ```bash
   TODO: command to split traffic 25% to v2 and 75% to v1
   ```

1. Run this cURL command several times:

   ```bash
   curl http://$GATEWAY_URL/helloworld/hello
   ```

   You will see alternating responses from all 4 helloworld instances, where approximately 1 out of every 4 (25%) responses
   will be from a "v2" instances, and the other responses from the "v1" instances:

   ```bash
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-p8909
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-qwpex
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v2, container: helloworld-v2-ggkvd
   $ curl http://$GATEWAY_URL/helloworld/hello
   Hello version: v1, container: helloworld-v1-p8909
   ...
   ```

   Note: if you use a browser instead of cURL to access the service and continually refresh the page, 
   it will always return the same version (v1 or v2), because a cookie is set to maintain version affinity.
   However, the browser still alternates in a random manner between instances of the specific version.

## Cleanup

To remove the `helloworld` application,

```bash
kubectl delete -f examples/k8s-helloworld.yaml
```
