package test

import (
	"io/ioutil"
	"fmt"
	"istio.io/api/mixer/v1"
	"log"
	"io"
	"context"
	"istio.io/istio/mixer/pkg/attribute"
	"testing"
	"istio.io/istio/mixer/pkg/server"
	"google.golang.org/grpc"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/mixer/pkg/adapter"
	"github.com/ghodss/yaml"
	"path"
	"encoding/json"
)

type Setup struct {
	// Baseline content
	BaselinePath string `json:"baseline"`

	ConfigsPaths []string `json:"configs,omitempty"`

	Attributes map[string]interface{}  `json:"attributes,omitempty"`
}

// marshallSetup converts a setup object into YAML serialized form.
func marshallSetup(setup *Setup) ([]byte, error) {
	return yaml.Marshal(setup)
}

// unmarshallSetup reads a setup object from a YAML serialized form.
func unmarshallSetup(bytes []byte, setup *Setup) error {
	return yaml.Unmarshal(bytes, setup)
}

func GetNewArgs(
	supportedTemplates map[string]template.Info,
	adapters []adapter.InfoFn,
	dataFilesFullPath ...string) (*server.Args, error) {
	args := server.NewArgs()
	args.Templates = supportedTemplates
	args.Adapters = adapters
	args.ConfigStoreURL = "memstore://tmp22"
	data := make([]string, 0)
	for _, f := range dataFilesFullPath {
		if d, err := ioutil.ReadFile(f); err != nil {
			return nil, err
		} else {
			data = append(data, string(d))
		}
	}
	err := store.SetupMemstore(args.ConfigStoreURL,data...)
	return args, err
}


func Test2(t *testing.T, wd string, fns []adapter.InfoFn, tmpls map[string]template.Info, yml string) {
	b := []byte(yml)
	var m Setup
	err := json.Unmarshal(b, &m)
	if err != nil {
		t.Fatal(err)
	}
	dat, err := ioutil.ReadFile(path.Join(wd, m.BaselinePath))
	if err != nil {
		panic(nil)
	}
	fmt.Println("err came back", err)
	fmt.Print(string(dat))


	fullCfgs := make([]string, 0)
	for _, f := range m.ConfigsPaths {
		fullCfgs = append(fullCfgs, path.Join(wd, f))
	}

	args, err := GetNewArgs(
		tmpls,
		fns,
		fullCfgs...)

	if err != nil {
		t.Fatal(err)
	}

	env, err := server.New(args)
	if err != nil {
		t.Fatalf("fail to create mixer: %v", err)
	}

	env.Run()

	defer closeHelper(env)

	conn, err := grpc.Dial(env.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Unable to connect to gRPC server: %v", err)
	}

	client := istio_mixer_v1.NewMixerClient(conn)
	defer closeHelper(conn)

	{

		req := istio_mixer_v1.CheckRequest{
			Attributes: getAttrBag(m.Attributes,
				args.ConfigIdentityAttribute,
				args.ConfigIdentityAttributeDomain),
		}


		_, err = client.Check(context.Background(), &req)
	}
}

func closeHelper(c io.Closer) {
	err := c.Close()
	if err != nil {
		log.Fatal(err)
	}
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
