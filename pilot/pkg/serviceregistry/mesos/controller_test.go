package mesos

import (
	"encoding/json"
	"fmt"
	"github.com/gambol99/go-marathon"
	"istio.io/istio/pilot/pkg/model"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

var (
	productpageJSON = `{
  "environment": {
    "SERVICES_DOMAIN": "marathon.l4lb.thisdcos.directory",
    "INBOUND_PORTS": "9080",
    "SERVICE_NAME": "productpage-v1",
    "ZIPKIN_ADDRESS": "zipkin.istio.marathon.l4lb.thisdcos.directory:31767",
    "DISCOVERY_ADDRESSS": "pilot.istio.marathon.l4lb.thisdcos.directory:31510"
  },
  "labels": {
    "istio": "productpage",
    "version": "v1"
  },
  "id": "/productpage",
  "containers": [
    {
      "name": "productpage",
      "resources": {
        "cpus": 0.2,
        "mem": 1024,
        "disk": 0,
        "gpus": 0
      },
      "endpoints": [
        {
          "name": "http-9080",
          "containerPort": 9080,
          "hostPort": 31180,
          "protocol": [
            "tcp"
          ],
          "labels": {
            "VIP_0": "/productpage:9080"
          }
        }
      ],
      "image": {
        "kind": "DOCKER",
        "id": "istio/examples-bookinfo-productpage-v1:1.8.0"
      }
    },
    {
      "name": "proxy",
      "resources": {
        "cpus": 0.5,
        "mem": 1024,
        "disk": 0,
        "gpus": 0
      },
      "image": {
        "kind": "DOCKER",
        "forcePullImage": true,
        "id": "hyge/proxy_debug:mesos2"
      }
    }
  ],
  "networks": [
    {
      "name": "dcos",
      "mode": "container"
    }
  ],
  "scaling": {
    "instances": 1,
    "kind": "fixed"
  },
  "scheduling": {
    "backoff": {
      "backoff": 1,
      "backoffFactor": 1.15,
      "maxLaunchDelay": 3600
    },
    "upgrade": {
      "minimumHealthCapacity": 1,
      "maximumOverCapacity": 1
    },
    "killSelection": "YOUNGEST_FIRST",
    "unreachableStrategy": {
      "inactiveAfterSeconds": 0,
      "expungeAfterSeconds": 0
    },
    "placement": {
      "constraints": [
        
      ]
    }
  },
  "executorResources": {
    "cpus": 0.1,
    "mem": 32,
    "disk": 10
  },
  "volumes": [],
  "fetch": []
}`

	reviewsV1JSON = `{
  "environment": {
    "SERVICES_DOMAIN": "marathon.l4lb.thisdcos.directory",
    "INBOUND_PORTS": "9080",
    "SERVICE_NAME": "reviews-v1",
    "ZIPKIN_ADDRESS": "zipkin.istio.marathon.l4lb.thisdcos.directory:31767",
    "DISCOVERY_ADDRESSS": "pilot.istio.marathon.l4lb.thisdcos.directory:31510"
  },
  "labels": {
    "istio": "reviews",
    "version": "v1"
  },
  "id": "/reviews-v1",
  "containers": [
    {
      "name": "reviews",
      "resources": {
        "cpus": 0.2,
        "mem": 1024,
        "disk": 0,
        "gpus": 0
      },
      "endpoints": [
        {
          "name": "http-9080",
          "containerPort": 9080,
          "hostPort": 0,
          "protocol": [
            "tcp"
          ],
          "labels": {
            "VIP_0": "/reviews:9080"
          }
        }
      ],
      "image": {
        "kind": "DOCKER",
        "id": "istio/examples-bookinfo-reviews-v1:1.8.0"
      }
    },
    {
      "name": "proxy",
      "resources": {
        "cpus": 0.2,
        "mem": 512,
        "disk": 0,
        "gpus": 0
      },
      "image": {
        "kind": "DOCKER",
        "id": "hyge/proxy_debug:mesos2"
      }
    }
  ],
  "networks": [
    {
      "name": "dcos",
      "mode": "container"
    }
  ],
  "scaling": {
    "instances": 1,
    "kind": "fixed"
  },
  "scheduling": {
    "backoff": {
      "backoff": 1,
      "backoffFactor": 1.15,
      "maxLaunchDelay": 3600
    },
    "upgrade": {
      "minimumHealthCapacity": 1,
      "maximumOverCapacity": 1
    },
    "killSelection": "YOUNGEST_FIRST",
    "unreachableStrategy": {
      "inactiveAfterSeconds": 0,
      "expungeAfterSeconds": 0
    },
    "placement": {
      "constraints": [
        
      ]
    }
  },
  "executorResources": {
    "cpus": 0.1,
    "mem": 32,
    "disk": 10
  },
  "volumes": [],
  "fetch": []
}`

	reviewsV2JSON = `
{
  "environment": {
    "SERVICES_DOMAIN": "marathon.l4lb.thisdcos.directory",
    "INBOUND_PORTS": "9080",
    "SERVICE_NAME": "reviews-v2",
    "ZIPKIN_ADDRESS": "zipkin.istio.marathon.l4lb.thisdcos.directory:31767",
    "DISCOVERY_ADDRESSS": "pilot.istio.marathon.l4lb.thisdcos.directory:31510"
  },
  "labels": {
    "istio": "reviews",
    "version": "v2"
  },
  "id": "/reviews-v2",
  "containers": [
    {
      "name": "reviews",
      "resources": {
        "cpus": 0.2,
        "mem": 1024,
        "disk": 0,
        "gpus": 0
      },
      "endpoints": [
        {
          "name": "http-9080",
          "containerPort": 9080,
          "hostPort": 0,
          "protocol": [
            "tcp"
          ],
          "labels": {
            "VIP_0": "/reviews:9080"
          }
        }
      ],
      "image": {
        "kind": "DOCKER",
        "id": "istio/examples-bookinfo-reviews-v2:1.8.0"
      }
    },
    {
      "name": "proxy",
      "resources": {
        "cpus": 0.2,
        "mem": 512,
        "disk": 0,
        "gpus": 0
      },
      "image": {
        "kind": "DOCKER",
        "id": "hyge/proxy_debug:mesos2"
      }
    }
  ],
  "networks": [
    {
      "name": "dcos",
      "mode": "container"
    }
  ],
  "scaling": {
    "instances": 1,
    "kind": "fixed"
  },
  "scheduling": {
    "backoff": {
      "backoff": 1,
      "backoffFactor": 1.15,
      "maxLaunchDelay": 3600
    },
    "upgrade": {
      "minimumHealthCapacity": 1,
      "maximumOverCapacity": 1
    },
    "killSelection": "YOUNGEST_FIRST",
    "unreachableStrategy": {
      "inactiveAfterSeconds": 0,
      "expungeAfterSeconds": 0
    },
    "placement": {
      "constraints": [
        
      ]
    }
  },
  "executorResources": {
    "cpus": 0.1,
    "mem": 32,
    "disk": 10
  },
  "volumes": [],
  "fetch": []
}
`

	reviewsV3JSON = `
{
  "environment": {
    "SERVICES_DOMAIN": "marathon.l4lb.thisdcos.directory",
    "INBOUND_PORTS": "9080",
    "SERVICE_NAME": "reviews-v3",
    "ZIPKIN_ADDRESS": "zipkin.istio.marathon.l4lb.thisdcos.directory:31767",
    "DISCOVERY_ADDRESSS": "pilot.istio.marathon.l4lb.thisdcos.directory:31510"
  },
  "labels": {
    "istio": "reviews",
    "version": "v3"
  },
  "id": "/reviews-v3",
  "containers": [
    {
      "name": "reviews",
      "resources": {
        "cpus": 0.2,
        "mem": 1024,
        "disk": 0,
        "gpus": 0
      },
      "endpoints": [
        {
          "name": "http-9080",
          "containerPort": 9080,
          "hostPort": 0,
          "protocol": [
            "tcp"
          ],
          "labels": {
            "VIP_0": "/reviews:9080"
          }
        }
      ],
      "image": {
        "kind": "DOCKER",
        "id": "istio/examples-bookinfo-reviews-v3:1.8.0"
      }
    },
    {
      "name": "proxy",
      "resources": {
        "cpus": 0.2,
        "mem": 512,
        "disk": 0,
        "gpus": 0
      },
      "image": {
        "kind": "DOCKER",
        "id": "hyge/proxy_debug:mesos2"
      }
    }
  ],
  "networks": [
    {
      "name": "dcos",
      "mode": "container"
    }
  ],
  "scaling": {
    "instances": 1,
    "kind": "fixed"
  },
  "scheduling": {
    "backoff": {
      "backoff": 1,
      "backoffFactor": 1.15,
      "maxLaunchDelay": 3600
    },
    "upgrade": {
      "minimumHealthCapacity": 1,
      "maximumOverCapacity": 1
    },
    "killSelection": "YOUNGEST_FIRST",
    "unreachableStrategy": {
      "inactiveAfterSeconds": 0,
      "expungeAfterSeconds": 0
    },
    "placement": {
      "constraints": [
        
      ]
    }
  },
  "executorResources": {
    "cpus": 0.1,
    "mem": 32,
    "disk": 10
  },
  "volumes": [],
  "fetch": []
}
`

	ratingsJSON = `{
    "id":"/ratings",
    "labels":{
        "istio":"ratings",
        "version":"v1"
    },
    "environment":{
        "SERVICES_DOMAIN":"marathon.l4lb.thisdcos.directory",
        "INBOUND_PORTS":"9080",
        "SERVICE_NAME":"ratings-v1",
        "ZIPKIN_ADDRESS":"zipkin.istio.marathon.l4lb.thisdcos.directory:31767",
        "DISCOVERY_ADDRESSS":"pilot.istio.marathon.l4lb.thisdcos.directory:31510"
    },
    "containers":[
        {
            "name":"proxy",
            "resources":{
                "cpus":0.2,
                "mem":512,
                "disk":0,
                "gpus":0
            },
            "image":{
                "kind":"DOCKER",
                "id":"hyge/proxy_debug:mesos2"
            }
        },
        {
            "name":"ratings",
            "resources":{
                "cpus":0.1,
                "mem":512,
                "disk":0,
                "gpus":0
            },
            "endpoints":[
                {
                    "name":"http-9080",
                    "containerPort":9080,
                    "hostPort":0,
                    "protocol":[
                        "tcp"
                    ],
                    "labels":{
                        "VIP_0":"/ratings:9080"
                    }
                }
            ],
            "image":{
                "kind":"DOCKER",
                "id":"istio/examples-bookinfo-ratings-v1:1.8.0"
            }
        }
    ],
    "networks":[
        {
            "name":"dcos",
            "mode":"container"
        }
    ],
    "scaling":{
        "kind":"fixed",
        "instances":1
    },
    "scheduling":{
        "placement":{
            "constraints":[

            ]
        },
        "backoff":{
            "backoff":1,
            "backoffFactor":1.15,
            "maxLaunchDelay":3600
        },
        "upgrade":{
            "minimumHealthCapacity":1,
            "maximumOverCapacity":1
        },
        "killSelection":"YOUNGEST_FIRST",
        "unreachableStrategy":{
            "inactiveAfterSeconds":0,
            "expungeAfterSeconds":0
        }
    },
    "executorResources":{
        "cpus":0.1,
        "mem":32,
        "disk":10
    }
}`

	productpageStatusJSON = `{
  "id": "/productpage",
  "spec": {
    "id": "/productpage",
    "labels": {
      "istio": "productpage",
      "version": "v1"
    },
    "version": "2019-05-21T10:43:34.737Z",
    "environment": {
      "SERVICE_NAME": "productpage-v1",
      "DISCOVERY_ADDRESSS": "pilot.istio.marathon.slave.mesos:31510",
      "ZIPKIN_ADDRESS": "zipkin.istio.marathon.slave.mesos:31767",
      "SERVICES_DOMAIN": "marathon.slave.mesos",
      "INBOUND_PORTS": "9080"
    },
    "containers": [
      {
        "name": "productpage",
        "resources": {
          "cpus": 0.2,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "endpoints": [
          {
            "name": "http-9080",
            "containerPort": 9080,
            "hostPort": 31180,
            "protocol": [
              "tcp"
            ],
            "labels": {
              "VIP_0": "/productpage:9080"
            }
          }
        ],
        "image": {
          "kind": "DOCKER",
          "id": "istio/examples-bookinfo-productpage-v1:1.8.0"
        }
      },
      {
        "name": "proxy",
        "resources": {
          "cpus": 0.5,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "image": {
          "kind": "DOCKER",
          "id": "hyge/proxy_debug:mesos2"
        }
      }
    ],
    "networks": [
      {
        "name": "dcos",
        "mode": "container"
      }
    ],
    "scaling": {
      "kind": "fixed",
      "instances": 1
    },
    "scheduling": {
      "backoff": {
        "backoff": 1,
        "backoffFactor": 1.15,
        "maxLaunchDelay": 3600
      },
      "upgrade": {
        "minimumHealthCapacity": 1,
        "maximumOverCapacity": 1
      },
      "killSelection": "YOUNGEST_FIRST",
      "unreachableStrategy": {
        "inactiveAfterSeconds": 0,
        "expungeAfterSeconds": 0
      }
    },
    "executorResources": {
      "cpus": 0.1,
      "mem": 32,
      "disk": 10
    }
  },
  "status": "STABLE",
  "statusSince": "2019-05-21T10:43:37.318Z",
  "instances": [
    {
      "id": "productpage.instance-488b3441-7bb5-11e9-b931-8ae30e1b1bb7",
      "status": "STABLE",
      "statusSince": "2019-05-21T10:43:37.318Z",
      "conditions": [],
      "agentHostname": "10.0.0.165",
      "agentId": "e9b2b531-8352-4480-bc46-0b23f79b7cc0-S3",
      "agentRegion": "aws/us-west-2",
      "agentZone": "aws/us-west-2b",
      "resources": {
        "cpus": 0.8,
        "mem": 2080,
        "disk": 10,
        "gpus": 0
      },
      "networks": [
        {
          "name": "dcos",
          "addresses": [
            "9.0.6.3"
          ]
        }
      ],
      "containers": [
        {
          "name": "productpage",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "productpage.instance-488b3441-7bb5-11e9-b931-8ae30e1b1bb7.productpage",
          "endpoints": [
            {
              "name": "http-9080",
              "allocatedHostPort": 31180
            }
          ],
          "resources": {
            "cpus": 0.2,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        },
        {
          "name": "proxy",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "productpage.instance-488b3441-7bb5-11e9-b931-8ae30e1b1bb7.proxy",
          "endpoints": [],
          "resources": {
            "cpus": 0.5,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        }
      ],
      "specReference": "/v2/pods/productpage::versions/2019-05-21T10:43:34.737Z",
      "localVolumes": [],
      "lastUpdated": "2019-05-21T10:43:37.318Z",
      "lastChanged": "2019-05-21T10:43:37.318Z"
    }
  ],
  "terminationHistory": [],
  "lastUpdated": "2019-05-21T10:43:43.526Z",
  "lastChanged": "2019-05-21T10:43:37.318Z"
}`
	reviewsV1StatusJSON = `{
  "id": "/reviews",
  "spec": {
    "id": "/reviews",
    "labels": {
      "istio": "reviews",
      "version": "v1"
    },
    "version": "2019-05-21T10:43:34.737Z",
    "environment": {
      "SERVICE_NAME": "reviews-v1",
      "DISCOVERY_ADDRESSS": "pilot.istio.marathon.slave.mesos:31510",
      "ZIPKIN_ADDRESS": "zipkin.istio.marathon.slave.mesos:31767",
      "SERVICES_DOMAIN": "marathon.slave.mesos",
      "INBOUND_PORTS": "9080"
    },
    "containers": [
      {
        "name": "reviews",
        "resources": {
          "cpus": 0.2,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "endpoints": [
          {
            "name": "http-9080",
            "containerPort": 9080,
            "hostPort": 0,
            "protocol": [
              "tcp"
            ],
            "labels": {
              "VIP_0": "/reviews:9080"
            }
          }
        ],
        "image": {
          "kind": "DOCKER",
          "id": "istio/examples-bookinfo-reviews-v1:1.8.0"
        }
      },
      {
        "name": "proxy",
        "resources": {
          "cpus": 0.5,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "image": {
          "kind": "DOCKER",
          "id": "hyge/proxy_debug:mesos2"
        }
      }
    ],
    "networks": [
      {
        "name": "dcos",
        "mode": "container"
      }
    ],
    "scaling": {
      "kind": "fixed",
      "instances": 1
    },
    "scheduling": {
      "backoff": {
        "backoff": 1,
        "backoffFactor": 1.15,
        "maxLaunchDelay": 3600
      },
      "upgrade": {
        "minimumHealthCapacity": 1,
        "maximumOverCapacity": 1
      },
      "killSelection": "YOUNGEST_FIRST",
      "unreachableStrategy": {
        "inactiveAfterSeconds": 0,
        "expungeAfterSeconds": 0
      }
    },
    "executorResources": {
      "cpus": 0.1,
      "mem": 32,
      "disk": 10
    }
  },
  "status": "STABLE",
  "statusSince": "2019-05-21T10:43:37.318Z",
  "instances": [
    {
      "id": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18cc",
      "status": "STABLE",
      "statusSince": "2019-05-21T10:43:37.318Z",
      "conditions": [],
      "agentHostname": "10.0.0.165",
      "agentId": "e9b2b531-8352-4480-bc46-0b23f79b7cc0-S3",
      "agentRegion": "aws/us-west-2",
      "agentZone": "aws/us-west-2b",
      "resources": {
        "cpus": 0.8,
        "mem": 2080,
        "disk": 10,
        "gpus": 0
      },
      "networks": [
        {
          "name": "dcos",
          "addresses": [
            "9.0.6.3"
          ]
        }
      ],
      "containers": [
        {
          "name": "reviews",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18cc.reviews",
          "endpoints": [
            {
              "name": "http-9080",
              "allocatedHostPort": 31180
            }
          ],
          "resources": {
            "cpus": 0.2,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        },
        {
          "name": "proxy",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18cc.proxy",
          "endpoints": [],
          "resources": {
            "cpus": 0.5,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        }
      ],
      "specReference": "/v2/pods/reviews::versions/2019-05-21T10:43:34.737Z",
      "localVolumes": [],
      "lastUpdated": "2019-05-21T10:43:37.318Z",
      "lastChanged": "2019-05-21T10:43:37.318Z"
    }
  ],
  "terminationHistory": [],
  "lastUpdated": "2019-05-21T10:43:43.526Z",
  "lastChanged": "2019-05-21T10:43:37.318Z"
}`
	reviewsV2StatusJSON = `{
  "id": "/reviews",
  "spec": {
    "id": "/reviews",
    "labels": {
      "istio": "reviews",
      "version": "v2"
    },
    "version": "2019-05-21T10:43:34.737Z",
    "environment": {
      "SERVICE_NAME": "reviews-v2",
      "DISCOVERY_ADDRESSS": "pilot.istio.marathon.slave.mesos:31510",
      "ZIPKIN_ADDRESS": "zipkin.istio.marathon.slave.mesos:31767",
      "SERVICES_DOMAIN": "marathon.slave.mesos",
      "INBOUND_PORTS": "9080"
    },
    "containers": [
      {
        "name": "reviews",
        "resources": {
          "cpus": 0.2,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "endpoints": [
          {
            "name": "http-9080",
            "containerPort": 9080,
            "hostPort": 0,
            "protocol": [
              "tcp"
            ],
            "labels": {
              "VIP_0": "/reviews:9080"
            }
          }
        ],
        "image": {
          "kind": "DOCKER",
          "id": "istio/examples-bookinfo-reviews-v2:1.8.0"
        }
      },
      {
        "name": "proxy",
        "resources": {
          "cpus": 0.5,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "image": {
          "kind": "DOCKER",
          "id": "hyge/proxy_debug:mesos2"
        }
      }
    ],
    "networks": [
      {
        "name": "dcos",
        "mode": "container"
      }
    ],
    "scaling": {
      "kind": "fixed",
      "instances": 1
    },
    "scheduling": {
      "backoff": {
        "backoff": 1,
        "backoffFactor": 1.15,
        "maxLaunchDelay": 3600
      },
      "upgrade": {
        "minimumHealthCapacity": 1,
        "maximumOverCapacity": 1
      },
      "killSelection": "YOUNGEST_FIRST",
      "unreachableStrategy": {
        "inactiveAfterSeconds": 0,
        "expungeAfterSeconds": 0
      }
    },
    "executorResources": {
      "cpus": 0.1,
      "mem": 32,
      "disk": 10
    }
  },
  "status": "STABLE",
  "statusSince": "2019-05-21T10:43:37.318Z",
  "instances": [
    {
      "id": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18cd",
      "status": "STABLE",
      "statusSince": "2019-05-21T10:43:37.318Z",
      "conditions": [],
      "agentHostname": "10.0.0.165",
      "agentId": "e9b2b531-8352-4480-bc46-0b23f79b7cc0-S3",
      "agentRegion": "aws/us-west-2",
      "agentZone": "aws/us-west-2b",
      "resources": {
        "cpus": 0.8,
        "mem": 2080,
        "disk": 10,
        "gpus": 0
      },
      "networks": [
        {
          "name": "dcos",
          "addresses": [
            "9.0.6.3"
          ]
        }
      ],
      "containers": [
        {
          "name": "reviews",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18cd.reviews",
          "endpoints": [
            {
              "name": "http-9080",
              "allocatedHostPort": 31180
            }
          ],
          "resources": {
            "cpus": 0.2,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        },
        {
          "name": "proxy",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18cd.proxy",
          "endpoints": [],
          "resources": {
            "cpus": 0.5,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        }
      ],
      "specReference": "/v2/pods/reviews::versions/2019-05-21T10:43:34.737Z",
      "localVolumes": [],
      "lastUpdated": "2019-05-21T10:43:37.318Z",
      "lastChanged": "2019-05-21T10:43:37.318Z"
    }
  ],
  "terminationHistory": [],
  "lastUpdated": "2019-05-21T10:43:43.526Z",
  "lastChanged": "2019-05-21T10:43:37.318Z"
}`

	reviewsV3StatusJSON = `{
  "id": "/reviews",
  "spec": {
    "id": "/reviews",
    "labels": {
      "istio": "reviews",
      "version": "v3"
    },
    "version": "2019-05-21T10:43:34.737Z",
    "environment": {
      "SERVICE_NAME": "reviews-v3",
      "DISCOVERY_ADDRESSS": "pilot.istio.marathon.slave.mesos:31510",
      "ZIPKIN_ADDRESS": "zipkin.istio.marathon.slave.mesos:31767",
      "SERVICES_DOMAIN": "marathon.slave.mesos",
      "INBOUND_PORTS": "9080"
    },
    "containers": [
      {
        "name": "reviews",
        "resources": {
          "cpus": 0.2,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "endpoints": [
          {
            "name": "http-9080",
            "containerPort": 9080,
            "hostPort": 0,
            "protocol": [
              "tcp"
            ],
            "labels": {
              "VIP_0": "/reviews:9080"
            }
          }
        ],
        "image": {
          "kind": "DOCKER",
          "id": "istio/examples-bookinfo-reviews-v3:1.8.0"
        }
      },
      {
        "name": "proxy",
        "resources": {
          "cpus": 0.5,
          "mem": 1024,
          "disk": 0,
          "gpus": 0
        },
        "image": {
          "kind": "DOCKER",
          "id": "hyge/proxy_debug:mesos2"
        }
      }
    ],
    "networks": [
      {
        "name": "dcos",
        "mode": "container"
      }
    ],
    "scaling": {
      "kind": "fixed",
      "instances": 1
    },
    "scheduling": {
      "backoff": {
        "backoff": 1,
        "backoffFactor": 1.15,
        "maxLaunchDelay": 3600
      },
      "upgrade": {
        "minimumHealthCapacity": 1,
        "maximumOverCapacity": 1
      },
      "killSelection": "YOUNGEST_FIRST",
      "unreachableStrategy": {
        "inactiveAfterSeconds": 0,
        "expungeAfterSeconds": 0
      }
    },
    "executorResources": {
      "cpus": 0.1,
      "mem": 32,
      "disk": 10
    }
  },
  "status": "STABLE",
  "statusSince": "2019-05-21T10:43:37.318Z",
  "instances": [
    {
      "id": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18ce",
      "status": "STABLE",
      "statusSince": "2019-05-21T10:43:37.318Z",
      "conditions": [],
      "agentHostname": "10.0.0.165",
      "agentId": "e9b2b531-8352-4480-bc46-0b23f79b7cc0-S3",
      "agentRegion": "aws/us-west-2",
      "agentZone": "aws/us-west-2b",
      "resources": {
        "cpus": 0.8,
        "mem": 2080,
        "disk": 10,
        "gpus": 0
      },
      "networks": [
        {
          "name": "dcos",
          "addresses": [
            "9.0.6.3"
          ]
        }
      ],
      "containers": [
        {
          "name": "reviews",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18ce.reviews",
          "endpoints": [
            {
              "name": "http-9080",
              "allocatedHostPort": 31180
            }
          ],
          "resources": {
            "cpus": 0.2,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        },
        {
          "name": "proxy",
          "status": "TASK_RUNNING",
          "statusSince": "2019-05-21T10:43:34.801Z",
          "conditions": [],
          "containerId": "reviews.instance-488b3441-7bb5-11e9-b931-9bc41a1c18ce.proxy",
          "endpoints": [],
          "resources": {
            "cpus": 0.5,
            "mem": 1024,
            "disk": 0,
            "gpus": 0
          },
          "lastUpdated": "2019-05-21T10:43:34.801Z",
          "lastChanged": "2019-05-21T10:43:34.801Z"
        }
      ],
      "specReference": "/v2/pods/reviews::versions/2019-05-21T10:43:34.737Z",
      "localVolumes": [],
      "lastUpdated": "2019-05-21T10:43:37.318Z",
      "lastChanged": "2019-05-21T10:43:37.318Z"
    }
  ],
  "terminationHistory": [],
  "lastUpdated": "2019-05-21T10:43:43.526Z",
  "lastChanged": "2019-05-21T10:43:37.318Z"
}`
)

type mockServer struct {
	Server      *httptest.Server
	Pods        []*marathon.Pod
	Productpage []*marathon.Pod
	Reviews     []*marathon.Pod
	Ratings     []*marathon.Pod
	Lock        sync.Mutex
}

func newServer(t *testing.T) *mockServer {
	m := mockServer{}

	productpage := marathon.NewPod()
	err := productpage.UnmarshalJSON([]byte(productpageJSON))
	if err != nil {
		t.Errorf("Failed to unmarshal json: %v", err)
	}

	reviewsv1 := marathon.NewPod()
	err = reviewsv1.UnmarshalJSON([]byte(reviewsV1JSON))
	if err != nil {
		t.Errorf("Failed to unmarshal json: %v", err)
	}

	reviewsv2 := marathon.NewPod()
	err = reviewsv2.UnmarshalJSON([]byte(reviewsV2JSON))
	if err != nil {
		t.Errorf("Failed to unmarshal json: %v", err)
	}

	reviewsv3 := marathon.NewPod()
	err = reviewsv3.UnmarshalJSON([]byte(reviewsV3JSON))
	if err != nil {
		t.Errorf("Failed to unmarshal json: %v", err)
	}

	rating := marathon.NewPod()
	err = rating.UnmarshalJSON([]byte(ratingsJSON))
	if err != nil {
		t.Errorf("Failed to unmarshal json: %v", err)
	}

	m.Productpage = []*marathon.Pod{productpage}
	m.Reviews = []*marathon.Pod{reviewsv1, reviewsv2, reviewsv3}
	m.Ratings = []*marathon.Pod{rating}
	m.Pods = []*marathon.Pod{productpage, reviewsv1, reviewsv2, reviewsv3, rating}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/pods" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.Pods)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v2/pods/reviews-v1" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, reviewsV1JSON)
		} else if r.URL.Path == "/v2/pods/reviews-v1::status" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, reviewsV1StatusJSON)
		} else if r.URL.Path == "/v2/pods/reviews-v2" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, reviewsV2JSON)
		} else if r.URL.Path == "/v2/pods/reviews-v2::status" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, reviewsV2StatusJSON)
		} else if r.URL.Path == "/v2/pods/reviews-v3" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, reviewsV3JSON)
		} else if r.URL.Path == "/v2/pods/reviews-v3::status" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, reviewsV3StatusJSON)
		} else if r.URL.Path == "/v2/pods/productpage" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.Productpage)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v2/pods/ratings" {
			m.Lock.Lock()
			data, _ := json.Marshal(&m.Ratings)
			m.Lock.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		} else if r.URL.Path == "/v2/pods/productpage::status" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(productpageStatusJSON))
		} else if r.URL.Path == "/v2/pods/details::status" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, "{}")
		} else {
			data, _ := json.Marshal(&[]*marathon.Pod{})
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, string(data))
		}
	}))

	m.Server = server
	return &m
}

func TestInstances(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	hostname := serviceHostname("reviews")
	filterPort := 9080
	instances, err := controller.InstancesByPort(hostname, filterPort, model.LabelsCollection{})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
	if len(instances) != 3 {
		t.Errorf("Instances() returned wrong # of service instances => %q, want 3", len(instances))
	}
	for _, inst := range instances {
		if inst.Service.Hostname != hostname {
			t.Errorf("Instances() returned wrong service instance => %v, want %q",
				inst.Service.Hostname, hostname)
		}
	}

	filterTagKey := "version"
	filterTagVal := "v3"
	instances, err = controller.InstancesByPort(hostname, filterPort, model.LabelsCollection{
		model.Labels{filterTagKey: filterTagVal},
	})
	if err != nil {
		t.Errorf("client encountered error during Instances(): %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("Instances() did not filter by tags => %q, want 1", len(instances))
	}
	for _, inst := range instances {
		found := false
		for key, val := range inst.Labels {
			if key == filterTagKey && val == filterTagVal {
				found = true
			}
		}
		if !found {
			t.Errorf("Instances() did not match by tag {%q:%q}", filterTagKey, filterTagVal)
		}
	}
}

func TestInstancesBadHostname(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Consul Controller: %v", err)
	}

	instances, err := controller.InstancesByPort("", 0, model.LabelsCollection{})
	if err == nil {
		t.Error("Instances() should return error when provided bad hostname")
	}
	if len(instances) != 0 {
		t.Errorf("Instances() returned wrong # of service instances => %q, want 0", len(instances))
	}
}

func TestInstancesError(t *testing.T) {
	ts := newServer(t)
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Consul Controller: %v", err)
	}

	ts.Server.Close()
	instances, err := controller.InstancesByPort(serviceHostname("reviews"), 9080, model.LabelsCollection{})
	if err == nil {
		t.Error("Instances() should return error when client experiences connection problem")
	}
	if len(instances) != 0 {
		t.Errorf("Instances() returned wrong # of instances: %q, want 0", len(instances))
	}
}

func TestServices(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	services, err := controller.Services()
	if err != nil {
		t.Errorf("client encountered error during Services(): %v", err)
	}
	serviceMap := make(map[string]*model.Service)
	for _, svc := range services {
		name, err := parseHostname(svc.Hostname)
		if err != nil {
			t.Errorf("Services() error parsing hostname: %v", err)
		}
		serviceMap[name] = svc
	}

	for _, name := range []string{"productpage", "reviews", "ratings"} {
		if _, exists := serviceMap[name]; !exists {
			t.Errorf("Services() missing: %q", name)
		}
	}
	if len(services) != 3 {
		t.Errorf("Services() returned wrong # of services: %q, want 3", len(services))
	}
}

func TestServicesError(t *testing.T) {
	ts := newServer(t)
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	ts.Server.Close()
	services, err := controller.Services()
	if err == nil {
		t.Error("Services() should return error when client experiences connection problem")
	}
	if len(services) != 0 {
		t.Errorf("Services() returned wrong # of services: %q, want 0", len(services))
	}
}

func TestGetService(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	service, err := controller.GetService("productpage.marathon.l4lb.thisdcos.directory")
	if err != nil {
		t.Errorf("client encountered error during GetService(): %v", err)
	}
	if service == nil {
		t.Error("service should exist")
	}

	if service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetService() incorrect service returned => %q, want %q",
			service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetServiceError(t *testing.T) {
	ts := newServer(t)
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	ts.Server.Close()
	service, err := controller.GetService("productpage.marathon.l4lb.thisdcos.directory")
	if err == nil {
		t.Error("GetService() should return error when client experiences connection problem")
	}
	if service != nil {
		t.Error("GetService() should return nil when client experiences connection problem")
	}
}

func TestGetServiceBadHostname(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	service, err := controller.GetService("")
	if err == nil {
		t.Error("GetService() should thow error for bad hostnames")
	}
	if service != nil {
		t.Error("service should not exist")
	}
}

func TestGetServiceNoInstances(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	ts.Pods = []*marathon.Pod{}

	service, err := controller.GetService("productpage.marathon.l4lb.thisdcos.directory")
	if err != nil {
		t.Errorf("GetService() encountered unexpected error: %v", err)
	}
	if service != nil {
		t.Errorf("service should not exist: %v", service)
	}
}

func TestGetProxyServiceInstances(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	services, err := controller.GetProxyServiceInstances(&model.Proxy{ID: "productpage.instance-488b3441-7bb5-11e9-b931-8ae30e1b1bb7"})
	if err != nil {
		t.Errorf("client encountered error during GetProxyServiceInstances(): %v", err)
	}
	if len(services) != 1 {
		t.Errorf("GetProxyServiceInstances() returned wrong # of endpoints => %q, want 1", len(services))
	}

	if services[0].Service.Hostname != serviceHostname("productpage") {
		t.Errorf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
			services[0].Service.Hostname, serviceHostname("productpage"))
	}
}

func TestGetProxyServiceInstancesError(t *testing.T) {
	ts := newServer(t)
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		ts.Server.Close()
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	ts.Server.Close()
	instances, err := controller.GetProxyServiceInstances(&model.Proxy{ID: "productpage.instance-488b3441-7bb5-11e9-b931-8ae30e1b1bb7"})
	if err == nil {
		t.Error("GetProxyServiceInstances() should return error when client experiences connection problem")
	}
	if len(instances) != 0 {
		t.Errorf("GetProxyServiceInstances() returned wrong # of instances: %q, want 0", len(instances))
	}
}

func TestGetProxyWorkloadLabels(t *testing.T) {
	ts := newServer(t)
	defer ts.Server.Close()
	option := ControllerOptions{
		ServerURL: ts.Server.URL,
		VIPDomain: "marathon.l4lb.thisdcos.directory",
		Interval:  2 * time.Second,
	}
	controller, err := NewController(option)
	if err != nil {
		t.Errorf("could not create Mesos Controller: %v", err)
	}

	tests := []struct {
		name     string
		id       string
		expected model.LabelsCollection
	}{
		{
			name:     "Productpage",
			id:       "productpage.instance-488b3441-7bb5-11e9-b931-8ae30e1b1bb7",
			expected: model.LabelsCollection{{"istio": "productpage", "version": "v1"}},
		},

		{
			name:     "No match",
			id:       "details.instance-xxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxx",
			expected: model.LabelsCollection{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			labels, err := controller.GetProxyWorkloadLabels(&model.Proxy{ID: test.id})

			if err != nil {
				t.Errorf("client encountered error during GetProxyWorkloadLabels(): %v", err)
			}

			if !reflect.DeepEqual(labels, test.expected) {
				t.Errorf("GetProxyWorkloadLabels() wrong labels => returned %#v, want %#v", labels, test.expected)
			}
		})
	}
}
