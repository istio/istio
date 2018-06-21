package main

import (
	"net/http"
)

func main() {
	finish := make(chan bool)
	server8001 := http.NewServeMux()
	server8001.HandleFunc("/foo", foo8001)
	server8001.HandleFunc("/bar", bar8001)

	server8002 := http.NewServeMux()
	server8002.HandleFunc("/foo", foo8002)
	server8002.HandleFunc("/bar", bar8002)

	go func() {
		http.ListenAndServe(":8001", server8001)
	}()

	go func() {
		http.ListenAndServe(":8002", server8002)
	}()

	<-finish
}

func foo8001(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Listening on 8001: foo "))
}

func bar8001(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Listening on 8001: bar "))
}

func foo8002(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Listening on 8002: foo "))
}

func bar8002(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Listening on 8002: bar "))
}
