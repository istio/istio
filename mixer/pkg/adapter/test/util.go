package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/pkg/template"
)

type (
	TestCase struct {
		Cfgs  []string
		Calls []Call // Calls are made async.
		Want  string
	}
	Call struct {
		CallKind CallKind
		Attrs    map[string]interface{}
		Quotas   map[string]istio_mixer_v1.CheckRequest_QuotaParams
	}
	CallKind int32

	WantObject struct {
		AdapterState interface{}    `json:"AdapterState"`
		Returns      map[int]Return `json:"Returns"`
	}
	Return struct {
		Check adapter.CheckResult            `json:"Check"`
		Quota map[string]adapter.QuotaResult `json:"Quota"`
		Error error                          `json:"Error"`
	}
)

const (
	CHECK  CallKind = 0
	REPORT CallKind = 1
)

type beforeFn func() (ctx interface{}, err error)
type afterFn func(ctx interface{})
type getStateFn func(ctx interface{}) (interface{}, error)

func AdapterIntegrationTest(
	t *testing.T,
	fns []adapter.InfoFn,
	tmpls map[string]template.Info,
	before beforeFn,
	after afterFn,
	getState getStateFn,
	testCase TestCase,

) {

	// Let the test do some initial setup.
	var ctx interface{}
	var err error
	if before != nil {
		ctx, err = before()
		// Teardown the initial setup
		if after != nil {
			defer after(ctx)
		}
		if err != nil {
			t.Error(err)
		}
	}

	// Start Mixer
	var args *server.Args
	if args, err = getServerArgs(tmpls, fns, testCase.Cfgs); err != nil {
		t.Fatal(err)
	}
	var env *server.Server
	if env, err = server.New(args); err != nil {
		t.Fatalf("fail to create mixer: %v", err)
	}
	env.Run()
	defer closeHelper(env)

	// Connect the client to Mixer
	conn, err := grpc.Dial(env.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}
	client := istio_mixer_v1.NewMixerClient(conn)
	defer closeHelper(conn)

	// Invoke calls async
	done := make(chan Return)
	returns := make([]Return, len(testCase.Calls))
	for i, call := range testCase.Calls {
		go execute(call, args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain, client, returns, i, done)
	}
	// wait for calls to finish
	for i := 0; i < len(testCase.Calls); i++ {
		<-done
	}

	// gather result
	got := newResult()
	for i := 0; i < len(testCase.Calls); i++ {
		got.Returns[i] = returns[i]
	}

	// get adapter state. NOTE: We need to marshal and then unmarshal it back into generic interface{}, which is exactly
	// what we get when unmarshalling the baseline json.
	adptState, _ := getState(ctx)
	if adptStateBytes, err := json.Marshal(adptState); err != nil {
		t.Fatal(err)
	} else if err = json.Unmarshal(adptStateBytes, &got.AdapterState); err != nil {
		t.Fatal(err)
	}

	var want WantObject
	if err = json.Unmarshal([]byte(testCase.Want), &want); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(want, got) {
		gotJson, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		wantJson, err := json.MarshalIndent(want, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		t.Errorf("\ngot  %s\nwant %s", gotJson, wantJson)
	}
}

func execute(c Call, idAttr string, idAttrDomain string, client istio_mixer_v1.MixerClient, callRes []Return, i int, done chan Return) {
	callResult := Return{}
	switch c.CallKind {
	case CHECK:
		req := istio_mixer_v1.CheckRequest{
			Attributes: getAttrBag(c.Attrs,
				idAttr,
				idAttrDomain),
			Quotas: c.Quotas,
		}

		result, resultErr := client.Check(context.Background(), &req)
		result.Precondition.ReferencedAttributes = istio_mixer_v1.ReferencedAttributes{}
		callResult.Error = resultErr
		if len(c.Quotas) > 0 {
			callResult.Quota = make(map[string]adapter.QuotaResult)
			for k := range c.Quotas {
				callResult.Quota[k] = adapter.QuotaResult{
					Amount: result.Quotas[k].GrantedAmount, ValidDuration: result.Quotas[k].ValidDuration,
				}
			}
		} else {
			callResult.Check.ValidDuration = result.Precondition.ValidDuration
			callResult.Check.ValidUseCount = result.Precondition.ValidUseCount
			callResult.Check.Status = result.Precondition.Status
		}

		callRes[i] = callResult
	case REPORT:
		req := istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{
				getAttrBag(c.Attrs,
					idAttr,
					idAttrDomain)},
		}
		_, responseErr := client.Report(context.Background(), &req)
		callResult.Error = responseErr
		callRes[i] = callResult
	}
	done <- callResult
}

func newResult() WantObject {
	return WantObject{Returns: make(map[int]Return)}
}

func closeHelper(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Fatal(err)
	}
}

func getServerArgs(
	tmpls map[string]template.Info,
	adpts []adapter.InfoFn,
	cfgs []string) (*server.Args, error) {

	args := server.DefaultArgs()
	args.Templates = tmpls
	args.Adapters = adpts

	data := make([]string, 0)
	for _, f := range cfgs {
		data = append(data, f)
	}

	// always include the attribute vocabulary
	_, filename, _, _ := runtime.Caller(0)
	if f, err := filepath.Abs(path.Join(path.Dir(filename), "../../../testdata/config/attributes.yaml")); err != nil {
		return nil, fmt.Errorf("cannot load attributes.yaml: %v", err)
	} else if f, err := ioutil.ReadFile(f); err != nil {
		return nil, fmt.Errorf("cannot load attributes.yaml: %v", err)
	} else {
		data = append(data, string(f))
	}

	var err error
	args.ConfigStore, err = storetest.SetupStoreForTest(data...)
	return args, err
}

func getAttrBag(attrs map[string]interface{}, identityAttr, identityAttrDomain string) istio_mixer_v1.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(identityAttr, identityAttrDomain)
	for k, v := range attrs {
		switch v.(type) {
		case map[string]interface{}:
			mapCast := make(map[string]string, len(v.(map[string]interface{})))

			for k1, v1 := range v.(map[string]interface{}) {
				mapCast[k1] = v1.(string)
			}
			requestBag.Set(k, mapCast)
		default:
			requestBag.Set(k, v)
		}
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}
