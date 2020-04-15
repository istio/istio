package status

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/utils/clock"
)

func TestStatusMaps(t *testing.T) {
	r := initReporterWithoutStarting()
	typ := ""
	r.RegisterEvent("conA", typ, "a")
	r.RegisterEvent("conB", typ, "a")
	r.RegisterEvent("conC", typ, "c")
	r.RegisterEvent("conD", typ, "d")
	RegisterTestingT(t)
	Expect(r.status).To(Equal(map[string]string{"conA": "a", "conB": "a", "conC": "c", "conD": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string][]string{"a": {"conA", "conB"}, "c": {"conC"}, "d": {"conD"}}))
	r.RegisterEvent("conA", typ, "d")
	Expect(r.status).To(Equal(map[string]string{"conA": "d", "conB": "a", "conC": "c", "conD": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string][]string{"a": {"conB"}, "c": {"conC"}, "d": {"conD", "conA"}}))
	r.RegisterDisconnect("conA", []string{""})
	Expect(r.status).To(Equal(map[string]string{"conB": "a", "conC": "c", "conD": "d"}))
	Expect(r.reverseStatus).To(Equal(map[string][]string{"a": {"conB"}, "c": {"conC"}, "d": {"conD"}}))
}

func initReporterWithoutStarting() (out Reporter) {
	out.PodName = "tespod"
	out.inProgressResources = []string{}
	out.client = nil              // TODO
	out.clock = clock.RealClock{} // TODO
	out.UpdateInterval = 300 * time.Millisecond
	out.store = nil // TODO
	out.cm = nil    // TODO
	out.reverseStatus = make(map[string][]string)
	out.status = make(map[string]string)
	return
}
