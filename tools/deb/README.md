# Implementation notes

## Pilot protocol

To debug the behavior of the VM, it helps to understand the pilot protocol and expectations.

Envoy requests to pilot are controlled by 2 parameters:
- "--service-cluster" - typically istio-proxy, from mesh config
- "--service-node" - identifies the node. This was the IP address in Istio 0.1

The first format used "|" separator, with 3 components:
- IPAddress
- ID
- Domain

In 0.2, the service name is a 4-tuple, with ~ separator. A proxy using 0.2 is not compatible with 
a 0.1 pilot nor the reverse.

- TYPE: sidecar | ingress | egress - determines the generated config. Defaults to sidecar, set as first CLI param for the agent.
- IPAdress - same as in 0.1, the IP of the endpoint/pod. This determines which local listeners to enable.
 Populated from $INSTANCE_IP or --ip 
- ID: the full target.uid, will be added as attribute to the server. Set as "--id" or $POD_NAME.$POD_NAMESPACE
- Domain: mesh domain, from $POD_NAMESPACE

The parameters are set by pilot-agent, based on environment variables.

UID is typically kubernetes://$PODNAME.$NAMESPACE

# LDS - or listeners

There are few types.
 
1. The main bound listener on 15001 - this is where iptables redirects
 
2. For each HTTP port in the cluster, there is one listener on tcp://0.0.0.0/port - defining mixer attributes
and asking for route config. This is for outbound requests.

3. For services running on the local machine. Only returned if pilot knows that the IP of the endpoint belongs 
to a service. The address is the ClusterIP of the service. 
 
 ```json
 
  {
    "address": "tcp://10.138.0.6:80", // the service IP, in cluster range 
  } 
  ...
 "route_config": {
        "virtual_hosts": [
         {
          "name": "inbound|80",
          "domains": [
           "*"
          ],
          "routes": [
           {
            "prefix": "/",
            "cluster": "in.80",
            "opaque_config": {
             "mixer_control": "on",
             "mixer_forward": "off"
            }
           }
          ]
         }
        ]
       },

 ```
4. For every TCP service, there is a listener on SERVICE_IP:port address, with a destionatio_ip_list.


# RDS - or routes

For each OUTBOUND port will define the route  to the right cluster. 

curl -v http://istio-pilot:8080/v1/routes/$PORT/istio-proxy/sidecar~$[VM_IP]~$[SVC].default~cluster.local

Note that the route generated includes an 'out' cluster - even if the IP is an endpoint for the machine.
That's because the RDS is applied on the 0.0.0.0 port - which is used for outbound. The more specific
service port will capture inbound traffic, and that has an explicit route to the inbound cluster.

```json

{
  "virtual_hosts": [
   {
    "name": "istio-ingress.default.svc.cluster.local|http",
    "domains": [
     "istio-ingress.default.svc:80",
     "istio-ingress.default.svc",
     "istio-ingress.default.svc.cluster:80",
     "istio-ingress.default.svc.cluster",
     "istio-ingress.default.svc.cluster.local:80",
     "istio-ingress.default.svc.cluster.local",
     "10.51.255.104:80",
     "10.51.255.104"
    ],
    "routes": [
     {
      "prefix": "/",
      "cluster": "out.fd518f1d0ba070c47739cbf6b191f85eb1cdda3d"
     }
    ]
   },
   {
    "name": "rawvm.default.svc.cluster.local|http",
    "domains": [
     "rawvm.default.svc:80",
     "rawvm.default.svc",
     "rawvm.default.svc.cluster:80",
     "rawvm.default.svc.cluster",
     "rawvm.default.svc.cluster.local:80",
     "rawvm.default.svc.cluster.local",
     "10.51.244.162:80",
     "10.51.244.162"
    ],
    "routes": [
     {
      "prefix": "/",
      "cluster": "out.a94f05e263fb8c0693d9ec4d8d61c9e3b15e2c05"
     }
    ]
   }
  ]
}

```

# LDS - or services (clusters in envoy)

curl -v http://istio-pilot:8080/v1/clusters/istio-proxy/sidecar~$[VM_IP]~$[SVC].default~cluster.local

If services are provided on the IP, expect to see an in cluster:

```json
"clusters": [
   {
    "name": "in.80",
    "connect_timeout_ms": 1000,
    "type": "static",
    "lb_type": "round_robin",
    "hosts": [
     {
      "url": "tcp://127.0.0.1:80"
     }
    ]
   },

```

In addition, for each name that shows up in RDS, there is one here:
The service name is the only useful info - should match the one in routes.

```json
 {
    "name": "out.01b3502fc7b29750c3b185358aec68fcbb4b9cf6",
    "service_name": "istio-pilot.default.svc.cluster.local|http-discovery",
    "connect_timeout_ms": 1000,
    "type": "sds",
    "lb_type": "round_robin"
   },

```


# SDS - endpoints

For each service, this resolves to the endpoint list. The parameter it the service name, 
result should show each endpoint IP. 

This is the same as K8S endpoint API, except in different format.

 curl -v 'http://istio-pilot:8080/v1/registration/zipkin.default.svc.cluster.local|http'
 
 
