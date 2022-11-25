package debug

import (
	"fmt"
	"istio.io/istio/centralized/pkg/server"
	"istio.io/pkg/log"
	"net/http"
	"strconv"
)

func printMap(w http.ResponseWriter, r *http.Request) {
	mapInfo := server.Get()
	for k, v := range mapInfo {
		lineSvc := k + "->" + strconv.Itoa(int(v)) + "\n"
		_, err := fmt.Fprintf(w, lineSvc)
		if err != nil {
			return
		}
	}
}

func run() error {
	http.HandleFunc("/virtualService_map", printMap)
	err := http.ListenAndServe(":2323", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
	return err
}

func StartDebugServer() {
	log.Info("Start debug server")
	go func() {
		err := run()
		if err != nil {
			panic(err)
		}
	}()
}
