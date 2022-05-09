package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
)

const (
	contentTypeHeader = "Content-Type"
	jsonFormat        = "application/json"
)

type AdminServer struct {
	httpServer *http.Server
	lis        net.Listener
}

func newAdminServer() (*AdminServer, error) {
	log.Println("initializing sink admin server")
	mux := http.NewServeMux()
	httpServer := &http.Server{
		Addr:    ":16000",
		Handler: mux,
	}

	// create http listener
	lis, err := net.Listen("tcp", ":16000")
	if err != nil {
		return nil, err
	}

	mux.HandleFunc("/metadata", dumpMetadata)
	mux.HandleFunc("/reset", cacheReset)
	return &AdminServer{
		httpServer: httpServer,
		lis:        lis,
	}, nil
}

func (a *AdminServer) Start() error {
	log.Printf("admin server listening on %v", a.httpServer.Addr)
	err := a.httpServer.Serve(a.lis)
	if err != nil {
		return fmt.Errorf("start admin server: %w", err)
	}
	return nil
}

func cacheReset(_ http.ResponseWriter, _ *http.Request) {
	cache = make(map[string][]Metadata)
}

func dumpMetadata(writer http.ResponseWriter, _ *http.Request) {
	log.Printf("dumpMetadata: cache %+v", cache)
	writeData(writer, cache, jsonFormat)
}

func writeData(writer http.ResponseWriter, data interface{}, format string) {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(err.Error()))
		return
	}
	writer.Header().Add(contentTypeHeader, format)
	_, err = writer.Write(out)
	if err != nil {
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
	writer.WriteHeader(http.StatusOK)
}
