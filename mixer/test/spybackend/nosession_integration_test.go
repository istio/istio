// +build !race

// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package spybackend

import (
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/mixer/adapter/model/v1beta1"
	istio_mixer_v1 "istio.io/api/mixer/v1"
	policy_v1beta1 "istio.io/api/policy/v1beta1"
	adapter_integration "istio.io/istio/mixer/pkg/adapter/test"
	"istio.io/istio/mixer/pkg/status"
	sampleapa "istio.io/istio/mixer/test/spyAdapter/template/apa"
	checkproducer "istio.io/istio/mixer/test/spyAdapter/template/checkoutput"
)

const (
	h1 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: h1
  namespace: istio-system
spec:
  adapter: spybackend-nosession
  connection:
    address: "%s"
---
`
	i1Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i1metric
  namespace: istio-system
spec:
  template: metric
  params:
    value: request.size | 123
    dimensions:
      destination_service: "\"unknown\""
      response_code: "200"
---
`

	r1H1I1Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r1
  namespace: istio-system
spec:
  actions:
  - handler: h1.istio-system
    instances:
    - i1metric
---
`

	h2 = `
apiVersion: "config.istio.io/v1alpha2"
kind: handler
metadata:
  name: h2
  namespace: istio-system
spec:
  adapter: spybackend-nosession
  connection:
    address: "%s"
---
`

	i2Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i2metric
  namespace: istio-system
spec:
  template: metric
  params:
    value: request.size | 456
    dimensions:
      destination_service: "\"unknown\""
      response_code: "400"
---
`

	r2H2I2Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r2
  namespace: istio-system
spec:
  actions:
  - handler: h2.istio-system
    instances:
    - i2metric
---
`
	i3List = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: i3list
  namespace: istio-system
spec:
  template: listentry
  params:
    value: source.name | "defaultstr"
---
`

	r3H1I3List = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r3
  namespace: istio-system
spec:
  actions:
  - handler: h1.istio-system
    instances:
    - i3list
---
`

	i4Quota = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: requestQuota
  namespace: istio-system
spec:
  template: quota
  params:
    dimensions:
      source: source.labels["app"] | source.name | "unknown"
      sourceVersion: source.labels["version"] | "unknown"
      destination: destination.labels["app"] | destination.service.host | "unknown"
      destinationVersion: destination.labels["version"] | "unknown"
---
`

	r4h1i4Quota = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r4
  namespace: istio-system
spec:
  actions:
  - handler: h1
    instances:
    - requestQuota
---
`

	r6MatchIfReqIDH1i4Metric = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r5
  namespace: istio-system
spec:
  match: request.id | "unknown" != "unknown"
  actions:
  - handler: h1
    instances:
    - i1metric
---
`
	i5Apa = `
apiVersion: "config.istio.io/v1alpha2"
kind: instance
metadata:
  name: genattrs
  namespace: istio-system
spec:
  template: apa
  params:
    int64Primitive: request.size | 456
  attribute_bindings:
    request.size: output.int64Primitive
---
`

	r7TriggerAPA = `
apiVersion: "config.istio.io/v1alpha2"
kind: rule
metadata:
  name: r7
  namespace: istio-system
spec:
  match: (destination.namespace | "") == "trigger_apa"
  actions:
  - handler: h1
    instances:
    - genattrs
---
`

	i6Checkoutput = `
apiVersion: config.istio.io/v1alpha2
kind: instance
metadata:
  name: i6
  namespace: istio-system
spec:
  template: checkoutput
  params:
    stringPrimitive: destination.namespace | "unknown"
`

	r8Checkoutput = `
apiVersion: config.istio.io/v1alpha2
kind: rule
metadata:
  name: r7
  namespace: istio-system
spec:
  actions:
  - handler: h1
    instances: ["i6"]
    name: h1action
  requestHeaderOperations:
  - name: x-istio-test
    values:
    - h1action.output.stringPrimitive
    - destination.namespace
`
)

func TestNoSessionBackend(t *testing.T) {
	testdata := []struct {
		name   string
		calls  []adapter_integration.Call
		status rpc.Status
		config []string
		want   string
	}{
		{
			name: "Check output call",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"destination.namespace": "testing-namespace"},
				},
			},
			want: `
{
	"AdapterState": [],
	"Returns": [
	{
	 "Check": {
		"Status": {},
		"ValidDuration": 5000000000,
		"ValidUseCount": 31,
		"RouteDirective": {
			"request_header_operations": [
				{
					"name": "x-istio-test",
					"value": "abracadabra"
				},
				{
					"name": "x-istio-test",
					"value": "testing-namespace"
				}
			],
			"response_header_operations": null
		}
	 },
	 "Quota": null,
	 "Error": null
	}
	]
}`,
			config: []string{i6Checkoutput, r8Checkoutput},
		},
		{
			// sets request.size to hardcoded value 1337
			name: "APA call with attributes",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"destination.namespace": "trigger_apa"},
				},
			},
			want: `
						{
						 "AdapterState": [
						  {
						   "dedup_id": "stripped_for_test",
						   "instances": [
						    {
						     "dimensions": {
						      "destination_service": {
						       "stringValue": "unknown"
						      },
						      "response_code": {
						       "int64Value": "400"
						      }
						     },
						     "name": "i2metric.instance.istio-system",
						     "value": {
						      "int64Value": "1337"
						     }
						    }
						   ]
						  },
						  {
						   "dedup_id": "stripped_for_test",
						   "instances": [
						    {
						     "dimensions": {
						      "destination_service": {
						       "stringValue": "unknown"
						      },
						      "response_code": {
						       "int64Value": "200"
						      }
						     },
						     "name": "i1metric.instance.istio-system",
						     "value": {
						      "int64Value": "1337"
						     }
						    }
						   ]
						  }
						 ],
						 "Returns": [
						  {
						   "Check": {
						    "Status": {},
						    "ValidDuration": 0,
						    "ValidUseCount": 0
						   },
						   "Quota": null,
						   "Error": null
						  }
						 ]
						}
				`,
		},
		{
			name: "single report call with attributes",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": int64(666)},
				},
			},
			want: `
						{
						 "AdapterState": [
						  {
						   "dedup_id": "stripped_for_test",
						   "instances": [
						    {
						     "dimensions": {
						      "destination_service": {
						       "stringValue": "unknown"
						      },
						      "response_code": {
						       "int64Value": "400"
						      }
						     },
						     "name": "i2metric.instance.istio-system",
						     "value": {
						      "int64Value": "666"
						     }
						    }
						   ]
						  },
						  {
						   "dedup_id": "stripped_for_test",
						   "instances": [
						    {
						     "dimensions": {
						      "destination_service": {
						       "stringValue": "unknown"
						      },
						      "response_code": {
						       "int64Value": "200"
						      }
						     },
						     "name": "i1metric.instance.istio-system",
						     "value": {
						      "int64Value": "666"
						     }
						    }
						   ]
						  }
						 ],
						 "Returns": [
						  {
						   "Check": {
						    "Status": {},
						    "ValidDuration": 0,
						    "ValidUseCount": 0
						   },
						   "Quota": null,
						   "Error": null
						  }
						 ]
						}
				`,
		},
		{
			name: "single report call no attributes",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{},
				},
			},
			want: `
							{
							 "AdapterState": [
							  {
							   "dedup_id": "stripped_for_test",
							   "instances": [
							    {
							     "dimensions": {
							      "destination_service": {
							       "stringValue": "unknown"
							      },
							      "response_code": {
							       "int64Value": "400"
							      }
							     },
							     "name": "i2metric.instance.istio-system",
							     "value": {
							      "int64Value": "456"
							     }
							    }
							   ]
							  },
							  {
							   "dedup_id": "stripped_for_test",
							   "instances": [
							    {
							     "dimensions": {
							      "destination_service": {
							       "stringValue": "unknown"
							      },
							      "response_code": {
							       "int64Value": "200"
							      }
							     },
							     "name": "i1metric.instance.istio-system",
							     "value": {
							      "int64Value": "123"
							     }
							    }
							   ]
							  }
							 ],
							 "Returns": [
							  {
							   "Check": {
							    "Status": {},
							    "ValidDuration": 0,
							    "ValidUseCount": 0
							   },
							   "Quota": null,
							   "Error": null
							  }
							 ]
							}
					`,
		},
		{
			name: "single check call with attributes",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "foobar"},
				},
			},
			want: `
					   		{
					    		 "AdapterState": [
					    		  {
					    		   "dedup_id": "stripped_for_test",
					    		   "instance": {
					    		    "name": "i3list.instance.istio-system",
					    		    "value": {
                                      "stringValue": "foobar"
                                    }
					    		   }
					    		  }
					    		 ],
					    		 "Returns": [
					    		  {
					    		   "Check": {
					    		    "Status": {},
					    		    "ValidDuration": 0,
					    		    "ValidUseCount": 31
					    		   },
					    		   "Quota": null,
					    		   "Error": null
					    		  }
					    		 ]
					    		}
					`,
		},
		{
			name: "single check call no attributes",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{},
				},
			},
			want: `
					    		{
					    		 "AdapterState": [
					    		  {
					    		   "dedup_id": "stripped_for_test",
					    		   "instance": {
					    		    "name": "i3list.instance.istio-system",
					                "value": {
                                      "stringValue": "defaultstr"
                                    }
					    		   }
					    		  }
					    		 ],
					    		 "Returns": [
					    		  {
					    		   "Check": {
					    		    "Status": {},
					    		    "ValidDuration": 0,
					    		    "ValidUseCount": 31
					    		   },
					    		   "Quota": null,
					    		   "Error": null
					    		  }
					    		 ]
					    		}
					`,
		},
		{
			name: "check custom error",
			calls: []adapter_integration.Call{
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{},
				},
			},
			status: rpc.Status{
				Code: int32(rpc.DATA_LOSS),
				Details: []*types.Any{status.PackErrorDetail(&policy_v1beta1.DirectHttpResponse{
					Code: policy_v1beta1.Unauthorized,
					Body: "nope",
				})},
			},
			want: `
{
    "AdapterState": [
        {
            "dedup_id": "stripped_for_test",
            "instance": {
                "name": "i3list.instance.istio-system",
                "value": {
                    "stringValue": "defaultstr"
                }
            }
        }
    ],
    "Returns": [
        {
            "Check": {
                "RouteDirective": {
                    "direct_response_body": "nope",
                    "direct_response_code": 401,
                    "request_header_operations": null,
                    "response_header_operations": null
                },
                "Status": {
                    "code": 15,
                    "message": "h1.handler.istio-system:"
                },
                "ValidDuration": 0,
                "ValidUseCount": 31
            },
            "Error": null,
            "Quota": null
        }
    ]
}
					`,
		},
		{
			name: "single quota call with attributes",
			calls: []adapter_integration.Call{{
				CallKind: adapter_integration.CHECK,
				Quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
					"requestQuota": {
						Amount:     35,
						BestEffort: true,
					},
				},
				Attrs: map[string]interface{}{"source.name": "foobar"},
			}},
			want: `
					    		{
					    		 "AdapterState": [
					    		  {
					    		   "dedup_id": "stripped_for_test",
					    		   "instance": {
					    		    "name": "i3list.instance.istio-system",
					    		    "value": {
                                      "stringValue": "foobar"
                                    }
					    		   }
					    		  },
					    		  {
					    		   "dedup_id": "stripped_for_test",
					    		   "instance": {
					    		    "dimensions": {
					    		     "destination": {
					    		      "stringValue": "unknown"
					    		     },
					    		     "destinationVersion": {
					    		      "stringValue": "unknown"
					    		     },
					    		     "source": {
					    		      "stringValue": "foobar"
					    		     },
					    		     "sourceVersion": {
					    		      "stringValue": "unknown"
					    		     }
					    		    },
					    		    "name": "requestQuota.instance.istio-system"
					    		   },
					    		   "quota_request": {
					    		    "quotas": {
					    		     "requestQuota.instance.istio-system": {
					    		      "amount": 35,
					    		      "best_effort": true
					    		     }
					    		    }
					    		   }
					    		  }
					    		 ],
					    		 "Returns": [
					    		  {
					    		   "Check": {
					    		    "Status": {},
					    		    "ValidDuration": 0,
					    		    "ValidUseCount": 0
					    		   },
					    		   "Quota": {
					    		    "requestQuota": {
					    		     "Status": {},
					    		     "ValidDuration": 0,
					    		     "Amount": 32
					    		    }
					    		   },
					    		   "Error": null
					    		  }
					    		 ]
					    		}
					`,
		},
		{
			name: "single quota call no attributes",
			calls: []adapter_integration.Call{{
				CallKind: adapter_integration.CHECK,
				Quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
					"requestQuota": {
						Amount:     35,
						BestEffort: true,
					},
				},
			}},
			want: `
					    		{
					    		 "AdapterState": [
					    		  {
					    		   "dedup_id": "stripped_for_test",
					    		   "instance": {
					    		    "name": "i3list.instance.istio-system",
					    		    "value": {
                                      "stringValue": "defaultstr"
                                    }
					    		   }
					    		  },
					    		  {
					    		   "dedup_id": "stripped_for_test",
					    		   "instance": {
					    		    "dimensions": {
					    		     "destination": {
					    		      "stringValue": "unknown"
					    		     },
					    		     "destinationVersion": {
					    		      "stringValue": "unknown"
					    		     },
					    		     "source": {
					    		      "stringValue": "unknown"
					    		     },
					    		     "sourceVersion": {
					    		      "stringValue": "unknown"
					    		     }
					    		    },
					    		    "name": "requestQuota.instance.istio-system"
					    		   },
					    		   "quota_request": {
					    		    "quotas": {
					    		     "requestQuota.instance.istio-system": {
					    		      "amount": 35,
					    		      "best_effort": true
					    		     }
					    		    }
					    		   }
					    		  }
					    		 ],
					    		 "Returns": [
					    		  {
					    		   "Check": {
					    		    "Status": {},
					    		    "ValidDuration": 0,
					    		    "ValidUseCount": 0
					    		   },
					    		   "Quota": {
					    		    "requestQuota": {
					    		     "Status": {},
					    		     "ValidDuration": 0,
					    		     "Amount": 32
					    		    }
					    		   },
					    		   "Error": null
					    		  }
					    		 ]
					    		}
					`,
		},

		{
			name: "multiple mix calls",
			calls: []adapter_integration.Call{
				// 3 report calls; varying request.size attribute and no attributes call too.
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": int64(666)},
				},
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.size": int64(888)},
				},
				{
					CallKind: adapter_integration.REPORT,
				},

				// 3 check calls; varying source.name attribute and no attributes call too.,
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "foobar"},
				},
				{
					CallKind: adapter_integration.CHECK,
					Attrs:    map[string]interface{}{"source.name": "bazbaz"},
				},
				{
					CallKind: adapter_integration.CHECK,
				},

				// one call with quota args
				{
					CallKind: adapter_integration.CHECK,
					Quotas: map[string]istio_mixer_v1.CheckRequest_QuotaParams{
						"requestQuota": {
							Amount:     35,
							BestEffort: true,
						},
					},
				},
				// one report request with request.id to match r4 rule
				{
					CallKind: adapter_integration.REPORT,
					Attrs:    map[string]interface{}{"request.id": "somereqid"},
				},
			},

			// want: --> multiple-mix-calls.golden.json
			// * 4 i2metric.instance.istio-system for 4 report calls
			// * 5 i1metric.instance.istio-system for 4 report calls (3 report calls without request.id attribute and 1 report calls
			//     with request.id attribute, which result into 2 dispatch report rules to resolve successfully).
			// * 4 i3list.instance.istio-system for 4 check calls
			// * 1 requestQuota.instance.istio-system for 1 quota call
		},
	}

	adptCfgBytes, err := ioutil.ReadFile("nosession.yaml")
	if err != nil {
		t.Fatalf("cannot open file: %v", err)
	}

	for _, td := range testdata {
		t.Run(td.name, func(tt *testing.T) {
			want := td.want
			if want == "" {
				want = readGoldenFile(tt, td.name)
			}
			adapter_integration.RunTest(
				tt,
				nil,
				adapter_integration.Scenario{
					Setup: func() (interface{}, error) {
						args := DefaultArgs()
						args.Behavior.HandleMetricResult = &v1beta1.ReportResult{}
						args.Behavior.HandleListEntryResult = &v1beta1.CheckResult{
							Status:        td.status,
							ValidUseCount: 31,
						}
						args.Behavior.HandleQuotaResult = &v1beta1.QuotaResult{
							Quotas: map[string]v1beta1.QuotaResult_Result{"requestQuota.instance.istio-system": {GrantedAmount: 32}}}
						// populate the APA output with all values
						args.Behavior.HandleSampleApaResult = &sampleapa.OutputMsg{
							Int64Primitive:  1337,
							BoolPrimitive:   true,
							DoublePrimitive: 456.123,
							StringPrimitive: "abracadabra",
							StringMap:       map[string]string{"x": "y"},
							Ip:              &policy_v1beta1.IPAddress{Value: []byte{127, 0, 0, 1}},
							Duration:        &policy_v1beta1.Duration{Value: types.DurationProto(5 * time.Second)},
							Timestamp:       &policy_v1beta1.TimeStamp{Value: types.TimestampNow()},
							Dns:             &policy_v1beta1.DNSName{Value: "google.com"},
						}
						args.Behavior.HandleSampleCheckResult = &v1beta1.CheckResult{
							ValidUseCount: 31,
							ValidDuration: 5 * time.Second,
						}
						args.Behavior.HandleCheckOutput = &checkproducer.OutputMsg{
							StringPrimitive: "abracadabra",
						}

						var s Server
						var err error
						if s, err = NewNoSessionServer(args); err != nil {
							return nil, err
						}
						s.Run()
						return s, nil
					},
					Teardown: func(ctx interface{}) {
						_ = ctx.(Server).Close()
					},
					GetState: func(ctx interface{}) (interface{}, error) {
						s := ctx.(*NoSessionServer)
						return s.GetState(), nil
					},
					SingleThreaded: false,
					ParallelCalls:  td.calls,
					GetConfig: func(ctx interface{}) ([]string, error) {
						s := ctx.(Server)

						if td.config != nil {
							return append(td.config,
								// CRs for built-in templates are automatically added by the integration test framework.
								string(adptCfgBytes), fmt.Sprintf(h1, s.Addr().String())), nil
						}

						return []string{
							// CRs for built-in templates are automatically added by the integration test framework.
							string(adptCfgBytes),
							fmt.Sprintf(h1, s.Addr().String()),
							i1Metric,
							r1H1I1Metric,
							fmt.Sprintf(h2, s.Addr().String()),
							i2Metric,
							r2H2I2Metric,
							i3List,
							r3H1I3List,
							i4Quota,
							r4h1i4Quota,
							r6MatchIfReqIDH1i4Metric,
							i5Apa,
							r7TriggerAPA,
						}, nil
					},
					Want: want,
				},
			)
		})
	}
}

// readGoldenFile reads contents based on the testname
// "this is a test" --> "this-is-a-test.golden.json"
func readGoldenFile(t *testing.T, testname string) string {
	t.Helper()
	filename := strings.Replace(testname, " ", "-", -1) + ".golden.json"
	ba, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("unable to load verification file: %v", err)
	}
	return string(ba)
}
