# Debug interface

The debug handlers are configured on the monitoring port (default 15014) as well
as on the http port (8080).


```bash

PILOT=istio-pilot.istio-system:15014

# What is sent to envoy
# Listeners and routes
curl $PILOT/debug/adsz

# Endpoints
curl $PILOT/debug/edsz

# Clusters
curl $PILOT/debug/cdsz


```

Each handler takes an extra parameter, "debug=0|1" which flips the verbosity of the
messages for that component (similar with envoy).

An extra parameter "push=1" triggers a config push to all connected endpoints.

Handlers should list, in json format:
- one entry for each connected envoy
- the timestamp of the connection

In addition, Pilot debug interface can show pilot's internal view of the config:

```bash


# General metrics
curl $PILOT/metrics

# All services/external services from all registries
curl $PILOT/debug/registryz

# All endpoints
curl $PILOT/debug/endpointz[?brief=1]

# All configs.
curl $PILOT/debug/configz

```

Example for EDS:

```json
{

   // Cluster
   "echosrv.istio-system.svc.cluster.local|grpc-ping": {
    "EdsClients": {
      // One for each connected envoy.
      "sidecar~172.17.0.8~echosrv-deployment-5b7878cc9-dlm8j.istio-system~istio-system.svc.cluster.local-116": {
        // Should match the info in the node (this is the real remote address)
        "PeerAddr": "172.17.0.8:42044",
        "Clusters": [
          // Should match the cluster, this is what is monitored
          "echosrv.istio-system.svc.cluster.local|grpc-ping"
        ],
        // Time the sidecar connected to pilot
        "Connect": "2018-03-22T15:01:07.527304202Z"
      },
      "sidecar~172.17.0.9~echosrv-deployment-5b7878cc9-wb9b7.istio-system~istio-system.svc.cluster.local-75": {
        "PeerAddr": "172.17.0.9:47182",
        "Clusters": [
          "echosrv.istio-system.svc.cluster.local|grpc-ping"
        ],
        "Connect": "2018-03-22T15:01:00.465066249Z"
      }
    },
    // The info pushed to each connected sidecar watching the cluster.
    "LoadAssignment": {
      "cluster_name": "echosrv.istio-system.svc.cluster.local|grpc-ping",
      "endpoints": [
        {
          "locality": {},
          "lb_endpoints": [
           {
              // Should match the endpoint and port from 'kubectl get ep'
              "endpoint": {
                "address": {
                  "Address": {
                    "SocketAddress": {
                      "address": "172.17.0.8",
                      "PortSpecifier": {
                        "PortValue": 8079
                      },
                      "ipv4_compat": true
                    }
                  }
                }
              }
            },
          ]
          }
      ]
      }
    }


```

# Log messages

Verbose messages for v2 is controlled by env variables PILOT_DEBUG_{EDS,CDS,LDS}.
Setting it to "0" disables debug, setting it to "1" enables - debug is currently
enabled by default, since it is not very verbose.

Messages are prefixed with EDS/LDS/CDS.

What we log and how to use it:
- sidecar connecting to pilot: "EDS/CSD/LDS: REQ ...". This includes the node, IP and the discovery
request proto. Should show up when the sidecar starts up.
- sidecar disconnecting from pilot: xDS: close. This happens when a pod is stopped.
- push events - whenever we push a config to the sidecar.
- "XDS: Registry event..." - indicates a registry event, should be followed by PUSH messages for
each endpoint.
- "EDS: no instances": pay close attention to this event, it indicates that Envoy asked for
a cluster but pilot doesn't have any valid instance. At some point after, when the instance eventually
shows up you should see an EDS PUSH message.

In addition, the registry has slightly more verbose messages about the events, so it is
possible to map an event in the registry to config pushes.


# Example requests and responses


EDS:

```bash

    node:<id:"ingress~~istio-ingress-6796c456f4-7zqtm.istio-system~istio-system.svc.cluster.local"
    cluster:"istio-ingress"
    build_version:"0/1.6.0-dev//RELEASE" >
    resource_names:"echosrv.istio-system.svc.cluster.local|http-echo"
    type_url:"type.googleapis.com/envoy.api.v2.ClusterLoadAssignment"

```
