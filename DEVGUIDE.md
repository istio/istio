# Developers Guide to Istio

# Respositories

The Istio project is divided across multiple github repositories:

### [api](https://github.com/istio/api)

This repository defines component-level APIs and common configuration 
formats for the Istio platform.

### [istio](https://github.com/istio/istio)

The main Istio repo which is used to host the high-level documentation
for the project.

### [manager](https://github.com/istio/manager)

The Istio Manager is used to configure Istio and propagate configuration to 
the other components of the system, including the Istio mixer and the Istio 
proxy mesh.

### [mixer](https://github.com/istio/mixer)

The Istio mixer provides the foundation of the Istio service mesh design. 
It is responsible for insulating the Istio proxy and Istio-based services 
from details of the current execution environment, as well as to implement 
the control policies that Istio supports.

### [mixerclient](https://github.com/istio/mixerclient)

C++ client for the mixer API.

### [proxy](https://github.com/istio/proxy)

The Istio Proxy is a microservice proxy that can be used on the client and 
server side, and forms a microservice mesh. 


# Building & Testing

See each specific repository for information about how to build that
component.
