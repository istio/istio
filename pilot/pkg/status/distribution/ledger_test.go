package distribution

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/ledger"
)

func TestTryLedgerPutAndDelete(t *testing.T) {
	RegisterTestingT(t)
	l := ledger.Make(5 * time.Second)
	resources := []config.Config{
		{
			Meta: config.Meta{
				Namespace:  "default",
				Name:       "foo",
				Generation: 1,
			},
		},
		{
			Meta: config.Meta{
				Namespace:  "default",
				Name:       "bar",
				Generation: 2,
			},
		},
		{
			Meta: config.Meta{
				Namespace:  "alternate",
				Name:       "boo",
				Generation: 3,
			},
		},
	}
	var result []string
	for _, res := range resources {
		tryLedgerPut(l, res)

		r, err := l.Get(res.Key())
		if err != nil {
			t.Errorf("ledger get %s error %v", res.Key(), err)
		}
		result = append(result, r)
	}

	Expect(result[0]).To(Equal("1"))
	Expect(result[1]).To(Equal("2"))
	Expect(result[2]).To(Equal("3"))

	tryLedgerDelete(l, resources[0])
	time.Sleep(6 * time.Second)
	result1, err := l.Get(resources[0].Key())
	if err != nil {
		t.Errorf("ledger get %s error %v", resources[0].Key(), err)
	}
	Expect(result1).To(Equal(""))
}
