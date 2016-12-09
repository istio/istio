package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/grpc"

	"istio.io/mixer/api/v1"
)

type clientState struct {
	client     mixerpb.MixerClient
	connection *grpc.ClientConn
}

func createAPIClient(port string) (*clientState, error) {
	cs := clientState{}

	var opts []grpc.DialOption
	opts = append(opts, grpc.WithInsecure())

	var err error
	if cs.connection, err = grpc.Dial(port, opts...); err != nil {
		return nil, err
	}

	cs.client = mixerpb.NewMixerClient(cs.connection)
	return &cs, nil
}

func deleteAPIClient(cs *clientState) {
	cs.connection.Close()
	cs.client = nil
	cs.connection = nil
}

func errorf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func check(cs *clientState, attrs map[string]string) {
	stream, err := cs.client.Check(context.Background())
	if err != nil {
		errorf("Check RPC failed: %v", err)
		return
	}

	// send the request
	request := mixerpb.CheckRequest{RequestIndex: 0}
	if err := stream.Send(&request); err != nil {
		errorf("Failed to send Check RPC: %v", err)
		return
	}

	response, err := stream.Recv()
	if err == io.EOF {
		errorf("Got no response from Check RPC")
		return
	} else if err != nil {
		errorf("Failed to receive a response from Check RPC: %v", err)
		return
	}
	stream.CloseSend()

	fmt.Printf("Check RPC returned %v\n", response.Result)
}

func main() {
	mixer := flag.String("mixer", "localhost:9091", "URL to mixer instance")
	rawAttrs := flag.String("attributes", "", "List of name/value pairs in the form name1=value1,name2=value2")
	api := flag.String("api", "check", "One of check|report|quota")
	flag.Parse()

	if flag.NArg() > 0 {
		for _, a := range flag.Args() {
			errorf("Unexpected arguments %s", a)
		}
		return
	}

	// validate & process args
	if *api != "check" && *api != "report" && *api != "quota" {
		errorf("-api must be one of check, report, or quota")
		return
	}

	attrs := make(map[string]string)
	if len(*rawAttrs) > 0 {
		for _, a := range strings.Split(*rawAttrs, ",") {
			i := strings.Index(a, "=")
			if i < 0 {
				errorf("Attribute value %v does not include an = sign", a)
				return
			} else if i == 0 {
				errorf("Attribute value %v does not contain a valid name", a)
				return
			}
			name := a[0:i]
			value := a[i+1:]
			attrs[name] = value
		}
	}

	cs, err := createAPIClient(*mixer)
	if err != nil {
		errorf("Unable to establish connection to %s", *mixer)
		return
	}
	defer deleteAPIClient(cs)

	switch *api {
	case "check":
		check(cs, attrs)
	case "report":
		errorf("report is not implemented yet")
	case "quota":
		errorf("quota is not implemented yet")
	}
}
