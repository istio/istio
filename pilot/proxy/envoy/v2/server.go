package v2

import (
	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
)

type Hasher struct{}

func (Hasher) Hash(node *api.Node) (cache.Key, error) {
	return "service-node", nil
}

func main() {
	/*
		r2 := api.Bootstrap{}
		s2, _ := json.Marshal(out.Bootstrap)
		if err := jsonpb.UnmarshalString(string(s2), &r2); err != nil {
			log.Fatal(err)
		}
		log.Printf("%v", clusters)

		if err = grpcServer.Serve(lis); err != nil {
			glog.Error(err)
		}
	*/
}
