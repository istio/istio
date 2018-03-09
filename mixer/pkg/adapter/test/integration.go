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
	// TestCase fully defines an adapter integration test
	TestCase struct {
		// Cfgs is a list of CRDs that Mixer will read.
		Cfgs []string
		// ParallelCalls is a list of test calls to be made to Mixer
		// in parallel.
		ParallelCalls []Call
		// Expected json string for a WantObject object.
		//
		// New test can start of with an empty "{}" string and then
		// get the baseline from the failure logs upon execution.
		Want string
	}
	// Call represents the input to make a call to Mixer
	Call struct {
		// CallKind can be either CHECK or REPORT
		CallKind CallKind
		// Attrs to call the Mixer with.
		Attrs map[string]interface{}
		// Quotas info to call the Mixer with.
		Quotas map[string]istio_mixer_v1.CheckRequest_QuotaParams
	}
	// CallKind represents the call to make; check or report.
	CallKind int32

	// Result represents the test baseline
	Result struct {
		// AdapterState represents adapter specific baseline data
		AdapterState interface{} `json:"AdapterState"`
		// Returns represents the return data from calls to Mixer
		Returns []Return `json:"Returns"`
	}
	// Return represents the return data from a call to Mixer
	Return struct {
		// Check is the response from a check call to Mixer
		Check adapter.CheckResult `json:"Check"`
		// Quota is the response from a check call to Mixer
		Quota map[string]adapter.QuotaResult `json:"Quota"`
		// Error is the error from call to Mixer
		Error error `json:"Error"`
	}
)

const (
	// CHECK for  Mixer Check
	CHECK CallKind = 0
	// REPORT for  Mixer Report
	REPORT CallKind = 1
)

type (
	setupFn    func() (ctx interface{}, err error)
	teardownFn func(ctx interface{})
	getStateFn func(ctx interface{}) (interface{}, error)
)

// AdapterIntegrationTest executes the given test case on
// an in-memory Mixer with given adapters, templates and configs.
func AdapterIntegrationTest(
	t *testing.T,
	adapters []adapter.InfoFn,
	tmpls map[string]template.Info,
	setup setupFn,
	teardown teardownFn,
	getState getStateFn,
	testCase TestCase,

) {

	// Let the test do some initial setup.
	var ctx interface{}
	var err error
	if setup != nil {
		ctx, err = setup()
		// Teardown the initial setup
		if teardown != nil {
			defer teardown(ctx)
		}
		if err != nil {
			t.Fatalf("initial setup failed: %v", err)
		}
	}

	// Start Mixer
	var args *server.Args
	var env *server.Server
	if args, err = getServerArgs(tmpls, adapters, testCase.Cfgs); err != nil {
		t.Fatalf("fail to create mixer args: %v", err)
	} else if env, err = server.New(args); err != nil {
		t.Fatalf("fail to new mixer: %v", err)
	} else {
		env.Run()
		defer closeHelper(env)
	}

	// Connect the client to Mixer
	conn, err := grpc.Dial(env.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}
	client := istio_mixer_v1.NewMixerClient(conn)
	defer closeHelper(conn)

	// Invoke calls async
	done := make(chan Return)
	got := Result{Returns: make([]Return, len(testCase.ParallelCalls))}
	for i, call := range testCase.ParallelCalls {
		go execute(call, args.ConfigIdentityAttribute, args.ConfigIdentityAttributeDomain, client, got.Returns, i, done)
	}
	// wait for calls to finish
	for i := 0; i < len(testCase.ParallelCalls); i++ {
		<-done
	}

	// get adapter state. NOTE: We are doing marshal and then unmarshal it back into generic interface{}.
	// This is done to make getState output into generic json map or array; which is exactly what we get when un-marshalling
	// the baseline json. Without this, deep equality on un-marshalled baseline AdapterState would defer
	// from the rich object returned by getState function.
	if getState != nil {
		adptState, _ := getState(ctx)
		if adptStateBytes, err := json.Marshal(adptState); err != nil {
			t.Fatalf("Unable to convert %v into json: %v", adptState, err)
		} else if err = json.Unmarshal(adptStateBytes, &got.AdapterState); err != nil {
			t.Fatalf("Unable to unmarshal %s into interface{}: %v", string(adptStateBytes), err)
		}
	}

	var want Result
	if err = json.Unmarshal([]byte(testCase.Want), &want); err != nil {
		t.Fatalf("Unable to unmarshal %s into Result: %v", testCase.Want, err)
	}

	// compare
	if !reflect.DeepEqual(want, got) {
		gotJson, err := json.MarshalIndent(got, "", " ")
		if err != nil {
			t.Fatalf("Unable to convert %v into json: %v", got, err)
		}
		wantJson, err := json.MarshalIndent(want, "", " ")
		if err != nil {
			t.Fatalf("Unable to convert %v into json: %v", want, err)
		}
		t.Errorf("\ngot=>\n%s\nwant=>\n%s", gotJson, wantJson)
	}
}

func execute(c Call, idAttr string, idAttrDomain string, client istio_mixer_v1.MixerClient, returns []Return, i int, done chan Return) {
	ret := Return{}
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
		ret.Error = resultErr
		if len(c.Quotas) > 0 {
			ret.Quota = make(map[string]adapter.QuotaResult)
			for k := range c.Quotas {
				ret.Quota[k] = adapter.QuotaResult{
					Amount: result.Quotas[k].GrantedAmount, ValidDuration: result.Quotas[k].ValidDuration,
				}
			}
		} else {
			ret.Check.ValidDuration = result.Precondition.ValidDuration
			ret.Check.ValidUseCount = result.Precondition.ValidUseCount
			ret.Check.Status = result.Precondition.Status
		}

	case REPORT:
		req := istio_mixer_v1.ReportRequest{
			Attributes: []istio_mixer_v1.CompressedAttributes{
				getAttrBag(c.Attrs,
					idAttr,
					idAttrDomain)},
		}
		_, responseErr := client.Report(context.Background(), &req)
		ret.Error = responseErr
	}
	returns[i] = ret
	done <- ret
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
