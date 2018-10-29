// Copyright 2018 AirMap Inc.

//go:generate $GOPATH/src/istio.io/istio/bin/mixer_codegen.sh -f mixer/adapter/airmap/config/config.proto

// Package airmap provides an adapter that dispatches to an in-cluster adapter via ReST.
// It implements the checkNothing, quota and listEntry templates.
package airmap

import (
	"context"
	"regexp"
	"time"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"istio.io/istio/mixer/adapter/airmap/access"
	"istio.io/istio/mixer/adapter/airmap/config"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/apikey"
	"istio.io/istio/mixer/template/authorization"
)

const (
	keyAPIKey            = "api-key"
	keyPath              = "path"
	keyVersion           = "version"
	defaultValidDuration = 5 * time.Second
)

var (
	apiKeyRegExp  = regexp.MustCompile(".*?apikey=(.*)")
	statusCodeLut = map[access.Code]rpc.Code{
		access.CodeOK:            rpc.OK,
		access.CodeForbidden:     rpc.PERMISSION_DENIED,
		access.CodeUnauthorized:  rpc.UNAUTHENTICATED,
		access.CodeQuotaExceeded: rpc.RESOURCE_EXHAUSTED,
	}
)

type handler struct {
	controller access.ControllerClient
}

func defaultParam() *config.Params {
	return &config.Params{}
}

func (h *handler) HandleApiKey(ctxt context.Context, instance *apikey.Instance) (adapter.CheckResult, error) {
	params := access.VerifyAPIKeyParameters{
		Name: &access.API_Name{
			AsString: instance.Api,
		},
		Method: &access.API_Method{
			AsString: instance.ApiOperation,
		},
		Key: &access.API_Key{
			AsString: instance.ApiKey,
		},
	}

	if len(instance.ApiVersion) > 0 {
		params.Version = &access.API_Version{
			AsString: instance.ApiVersion,
		}
	}

	ts, err := types.TimestampProto(instance.Timestamp)
	if err != nil {
		ts = types.TimestampNow()
	}

	params.Timestamp = ts

	result, err := h.controller.VerifyAPIKey(ctxt, &params)
	if err != nil {
		return adapter.CheckResult{
			Status: rpc.Status{
				Code: int32(rpc.INTERNAL),
			},
			ValidDuration: defaultValidDuration,
			ValidUseCount: 1,
		}, err
	}

	duration, err := types.DurationFromProto(result.Validity.Duration)
	if err != nil {
		duration = defaultValidDuration
	}

	return adapter.CheckResult{
		Status: rpc.Status{
			Code:    int32(statusCodeLut[result.Status.Code]),
			Message: result.Status.Message,
		},
		ValidDuration: duration,
		ValidUseCount: int32(result.Validity.Count),
	}, nil
}

func (h *handler) HandleAuthorization(ctxt context.Context, instance *authorization.Instance) (adapter.CheckResult, error) {
	params := access.AuthorizeAccessParameters{
		Subject: &access.AuthorizeAccessParameters_Subject{
			Credentials: &access.Credentials{
				Username: &access.Credentials_Username{
					AsString: instance.Subject.User,
				},
				Groups: []*access.Credentials_Group{
					&access.Credentials_Group{
						AsString: instance.Subject.Groups,
					},
				},
			},
		},
		Action: &access.AuthorizeAccessParameters_Action{
			Namespace: &access.API_Namespace{
				AsString: instance.Action.Namespace,
			},
			Name: &access.API_Name{
				AsString: instance.Action.Service,
			},
			Method: &access.API_Method{
				AsString: instance.Action.Method,
			},
		},
		Timestamp: types.TimestampNow(),
	}

	if auth, ok := instance.Subject.Properties["Authorization"].(string); ok {
		params.Raw = &access.Raw{
			Authorization: &access.Raw_Authorization{
				AsString: auth,
			},
		}
	}

	// Handling the case of tiledata here: The api key comes in via a query parameter
	// named 'apikey' and envoy only extracts api keys from query parameters with keys:
	//   key
	//   api_key
	if v, present := instance.Action.Properties[keyPath]; present {
		if s, ok := v.(string); ok {
			if match := apiKeyRegExp.FindStringSubmatch(s); match != nil {
				params.Subject.Key = &access.API_Key{
					AsString: match[1],
				}
			}
		}
	}

	if v, present := instance.Subject.Properties[keyAPIKey]; present {
		if s, ok := v.(string); ok {
			params.Subject.Key = &access.API_Key{
				AsString: s,
			}
		}
	}

	if v, present := instance.Action.Properties[keyVersion]; present {
		if s, ok := v.(string); ok {
			params.Action.Version = &access.API_Version{
				AsString: s,
			}
		}
	}

	if len(instance.Action.Path) > 0 {
		params.Action.Resource = &access.API_Resource{
			AsString: instance.Action.Path,
		}
	}

	result, err := h.controller.AuthorizeAccess(ctxt, &params, grpc.FailFast(true))
	if err != nil {
		return adapter.CheckResult{
			Status: rpc.Status{
				Code: int32(rpc.INTERNAL),
			},
			ValidDuration: defaultValidDuration,
			ValidUseCount: 1,
		}, err
	}

	duration, err := types.DurationFromProto(result.Validity.Duration)
	if err != nil {
		duration = defaultValidDuration
	}

	return adapter.CheckResult{
		Status: rpc.Status{
			Code:    int32(statusCodeLut[result.Status.Code]),
			Message: result.Status.Message,
		},
		ValidDuration: duration,
		ValidUseCount: int32(result.Validity.Count),
	}, nil
}

func (*handler) Close() error {
	return nil
}

// GetInfo returns the Info associated with this adapter implementation.
func GetInfo() adapter.Info {
	return adapter.Info{
		Name:        "airmap",
		Impl:        "istio.io/istio/mixer/adapter/airmap",
		Description: "Dispatches to an in-cluster adapter via ReST",
		SupportedTemplates: []string{
			apikey.TemplateName,
			authorization.TemplateName,
		},
		DefaultConfig: defaultParam(),
		NewBuilder:    func() adapter.HandlerBuilder { return &builder{} },
	}
}

var _ apikey.HandlerBuilder = &builder{}
var _ authorization.HandlerBuilder = &builder{}

type builder struct {
	adapterConfig *config.Params
}

func (*builder) SetApiKeyTypes(map[string]*apikey.Type)               {}
func (*builder) SetAuthorizationTypes(map[string]*authorization.Type) {}

func (b *builder) SetAdapterConfig(cfg adapter.Config) {
	b.adapterConfig = cfg.(*config.Params)
}

func (*builder) Validate() (ce *adapter.ConfigErrors) {
	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	// TODO(tvoss): Investigate whether we should use a secure transport here despite operating in-cluster.
	cc, err := grpc.Dial(b.adapterConfig.Endpoint, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	return &handler{
		controller: access.NewControllerClient(cc),
	}, nil
}
