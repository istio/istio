package status

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
)

type Fraction struct {
	Numerator int
	Denominator int
}

type DistributionController struct {
	CurrentState map[Resource] map[string] Fraction
}

func (c *DistributionController) Start(stop <-chan struct{}) {
	// create watch
	i := informers.SharedInformerFactory.Core().V1().ConfigMaps()
	i.Informer().AddEventHandler(DistroReportHandler{dc:*c})
	i.Informer().Run(stop)
}

func (c *DistributionController) handleReport(d DistributionReport) {
	for res := range d.InProgressResources {
		if _, ok := c.CurrentState[res]; !ok {
			c.CurrentState[res] = make(map[string]Fraction)
		}
		c.CurrentState[res][d.Reporter] = Fraction{d.InProgressResources[res], d.DataPlaneCount}
	}
}

func (c *DistributionController) Reconcile() {
	for x := range c.CurrentState {
		// sum up numer, denom
		// get resource
		// if needs write
		// write, but don't overwrite?
	}
}

type DistroReportHandler struct {
	dc DistributionController
}
func (drh DistroReportHandler) OnAdd(obj interface{}){
	drh.HandleNew(obj)
}

func (drh DistroReportHandler) OnUpdate(oldObj, newObj interface{}) {
	drh.HandleNew(newObj)
}

func (drh DistroReportHandler) HandleNew(obj interface{}) {
	cm := obj.(v1.ConfigMap)
	dr, _ := ReportFromYaml([]byte(cm.Data["somekey"]))
	// TODO: Handle err
	drh.dc.handleReport(dr)
}

func (drh DistroReportHandler) OnDelete(obj interface{}){
	// TODO: what do we do here?  will these ever be deleted?
}