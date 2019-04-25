package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/emicklei/go-restful"
)

type User struct {
	Id   int
	Name string
}

//
// This example shows how to log both the request body and response body

func main() {
	restful.Add(newUserService())
	log.Print("start listening on localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func newUserService() *restful.WebService {
	ws := new(restful.WebService)
	ws.
		Path("/users").
		Consumes(restful.MIME_XML, restful.MIME_JSON).
		Produces(restful.MIME_JSON, restful.MIME_XML)

	ws.Route(ws.POST("/").Filter(bodyLogFilter).To(createUser))
	return ws
}

// Route Filter (defines FilterFunction)
func bodyLogFilter(req *restful.Request, resp *restful.Response, chain *restful.FilterChain) {

	inBody, err := ioutil.ReadAll(req.Request.Body)
	if err != nil {
		resp.WriteError(400, err)
		return
	}
	req.Request.Body = ioutil.NopCloser(bytes.NewReader(inBody))

	c := NewResponseCapture(resp.ResponseWriter)
	resp.ResponseWriter = c

	chain.ProcessFilter(req, resp)

	log.Println("Request body:", string(inBody))
	log.Println("Response body:", string(c.Bytes()))
}

// curl -H "content-type:application/json" http://localhost:8080/users -d '{"Id":42, "Name":"Captain Marvel"}'
//
func createUser(request *restful.Request, response *restful.Response) {
	u := new(User)
	err := request.ReadEntity(u)
	log.Print("createUser", err, u)
	response.WriteEntity(u)
}

type ResponseCapture struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
	body        *bytes.Buffer
}

func NewResponseCapture(w http.ResponseWriter) *ResponseCapture {
	return &ResponseCapture{
		ResponseWriter: w,
		wroteHeader:    false,
		body:           new(bytes.Buffer),
	}
}

func (c ResponseCapture) Header() http.Header {
	return c.ResponseWriter.Header()
}

func (c ResponseCapture) Write(data []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	c.body.Write(data)
	return c.ResponseWriter.Write(data)
}

func (c ResponseCapture) WriteHeader(statusCode int) {
	c.status = statusCode
	c.ResponseWriter.WriteHeader(statusCode)
}

func (c ResponseCapture) Bytes() []byte {
	return c.body.Bytes()
}

func (c ResponseCapture) StatusCode() int {
	return c.status
}
