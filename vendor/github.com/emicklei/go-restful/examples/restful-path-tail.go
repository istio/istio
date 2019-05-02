package main

import (
	. "github.com/emicklei/go-restful"
	"io"
	"log"
	"net/http"
)

// This example shows how to create a Route matching the "tail" of a path.
// Requires the use of a CurlyRouter and the star "*" path parameter pattern.
//
// GET http://localhost:8080/basepath/some/other/location/test.xml

func main() {
	DefaultContainer.Router(CurlyRouter{})
	ws := new(WebService)
	ws.Route(ws.GET("/basepath/{resource:*}").To(staticFromPathParam))
	Add(ws)

	println("[go-restful] serve path tails from http://localhost:8080/basepath")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func staticFromPathParam(req *Request, resp *Response) {
	io.WriteString(resp, "Tail="+req.PathParameter("resource"))
}
