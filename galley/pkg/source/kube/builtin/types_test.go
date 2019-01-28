package builtin_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/source/kube/builtin"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParse(t *testing.T) {
	t.Run("Endpoints", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := getJSON(t, "endpoints.yaml")

		objMeta, objResource := parse(t, input, "Endpoints")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.Endpoints)
		if !ok {
			t.Fatal("failed casting item to Endpoints")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns"))
	})

	t.Run("Node", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := getJSON(t, "node.yaml")

		objMeta, objResource := parse(t, input, "Node")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.NodeSpec)
		if !ok {
			t.Fatal("failed casting item to NodeSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("gke-istio-test-default-pool-866a0405-420r"))
	})

	t.Run("Pod", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := getJSON(t, "pod.yaml")

		objMeta, objResource := parse(t, input, "Pod")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.Pod)
		if !ok {
			t.Fatal("failed casting item to Pod")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns-548976df6c-d9kkv"))
	})

	t.Run("Service", func(t *testing.T) {
		g := NewGomegaWithT(t)
		input := getJSON(t, "service.yaml")

		objMeta, objResource := parse(t, input, "Service")

		// Just validate a couple of things...
		_, ok := objResource.(*coreV1.ServiceSpec)
		if !ok {
			t.Fatal("failed casting item to ServiceSpec")
		}
		g.Expect(objMeta.GetName()).To(Equal("kube-dns"))
	})
}

func TestEquals(t *testing.T) {
	g := NewGomegaWithT(t)

	for _, spec := range builtin.GetSchema().All() {
		bt := builtin.GetType(spec.Kind)

		t.Run(spec.Kind, func(t *testing.T) {
			// First, test nils
			t.Run("Nils", func(t *testing.T) {
				obj := empty(spec.Kind)
				actual := bt.IsEqual(&struct{}{}, obj)
				g.Expect(actual).To(BeFalse())
				actual = bt.IsEqual(obj, &struct{}{})
				g.Expect(actual).To(BeFalse())
			})

			t.Run("Equal", func(t *testing.T) {
				if spec.Kind == "Endpoints" {
					// Equal if subsets match, regardless of the rest.
					actual := bt.IsEqual(&coreV1.Endpoints{
						ObjectMeta: metaV1.ObjectMeta{
							ResourceVersion: "v1",
						},
					}, &coreV1.Endpoints{
						ObjectMeta: metaV1.ObjectMeta{
							ResourceVersion: "v2",
						},
					})
					g.Expect(actual).To(BeTrue())
				} else {
					// All other types use the resource version alone.
					o1 := empty(spec.Kind)
					o1.SetResourceVersion("v0")
					o1.SetName("bob")

					o2 := empty(spec.Kind)
					o2.SetResourceVersion("v0")
					actual := bt.IsEqual(o1, o2)
					g.Expect(actual).To(BeTrue())
				}
			})

			t.Run("NotEqual", func(t *testing.T) {
				if spec.Kind == "Endpoints" {
					// Not Equal if subsets differ
					actual := bt.IsEqual(&coreV1.Endpoints{
						Subsets: []coreV1.EndpointSubset{
							{
								Addresses: []coreV1.EndpointAddress{
									{
										Hostname: "somehost.com",
									},
								},
							},
						},
					}, &coreV1.Endpoints{
						ObjectMeta: metaV1.ObjectMeta{
							ResourceVersion: "v2",
						},
					})
					g.Expect(actual).To(BeFalse())
				} else {
					// All other types use the resource version alone.
					o1 := empty(spec.Kind)
					o1.SetResourceVersion("v0")

					o2 := empty(spec.Kind)
					o2.SetResourceVersion("v1")
					actual := bt.IsEqual(o1, o2)
					g.Expect(actual).To(BeFalse())
				}
			})
		})
	}
}

func TestExtractObject(t *testing.T) {
	for _, spec := range builtin.GetSchema().All() {
		bt := builtin.GetType(spec.Kind)

		t.Run(spec.Kind, func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				out := bt.ExtractObject(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(out).To(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out := bt.ExtractObject(empty(spec.Kind))
				g := NewGomegaWithT(t)
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func TestExtractResource(t *testing.T) {
	for _, spec := range builtin.GetSchema().All() {
		bt := builtin.GetType(spec.Kind)

		t.Run(spec.Kind, func(t *testing.T) {
			t.Run("WrongTypeShouldReturnNil", func(t *testing.T) {
				out := bt.ExtractResource(struct{}{})
				g := NewGomegaWithT(t)
				g.Expect(out).To(BeNil())
			})

			t.Run("Success", func(t *testing.T) {
				out := bt.ExtractResource(empty(spec.Kind))
				g := NewGomegaWithT(t)
				g.Expect(out).ToNot(BeNil())
			})
		})
	}
}

func getJSON(t *testing.T, fileName string) []byte {
	t.Helper()
	g := NewGomegaWithT(t)

	inputYaml, err := ioutil.ReadFile(filepath.Join("testdata", fileName))
	g.Expect(err).To(BeNil())

	inputJson, err := yaml.YAMLToJSON(inputYaml)
	g.Expect(err).To(BeNil())

	return inputJson
}

func parse(t *testing.T, input []byte, kind string) (metaV1.Object, proto.Message) {
	t.Helper()
	g := NewGomegaWithT(t)

	bt := builtin.GetType(kind)
	obj, err := bt.ParseJSON(input)
	g.Expect(err).To(BeNil())

	return bt.ExtractObject(obj), bt.ExtractResource(obj)
}

func empty(kind string) metaV1.Object {
	switch kind {
	case "Node":
		return &coreV1.Node{}
	case "Service":
		return &coreV1.Service{}
	case "Pod":
		return &coreV1.Pod{}
	case "Endpoints":
		return &coreV1.Endpoints{}
	default:
		panic("unsupported kind")
	}
}
