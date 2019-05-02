package main

import (
	"fmt"
	"log"

	"github.com/docopt/docopt-go"
	"github.com/googleapis/gnostic/plugins/gnostic-go-generator/examples/googleauth"
	"github.com/googleapis/gnostic/plugins/gnostic-go-generator/examples/v3.0/urlshortener/urlshortener"
)

func main() {
	usage := `
Usage:
	urlshortener get <url>
	urlshortener list
	urlshortener insert <url>
	`
	arguments, err := docopt.Parse(usage, nil, false, "URL Shortener 1.0", false)
	if err != nil {
		log.Fatalf("%+v", err)
	}

	path := "https://www.googleapis.com/urlshortener/v1" // this should be generated

	client, err := googleauth.NewOAuth2Client("https://www.googleapis.com/auth/urlshortener")
	if err != nil {
		log.Fatalf("Error building OAuth client: %v", err)
	}
	c := urlshortener.NewClient(path, client)

	// get
	if arguments["get"].(bool) {
		response, err := c.Urlshortener_Url_Get("FULL", arguments["<url>"].(string))
		if err != nil {
			log.Fatalf("%+v", err)
		}
		fmt.Println(response.Default.LongUrl)
	}

	// list
	if arguments["list"].(bool) {
		response, err := c.Urlshortener_Url_List("", "")
		if err != nil {
			log.Fatalf("%+v", err)
		}
		for _, item := range response.Default.Items {
			fmt.Printf("%-40s %s\n", item.Id, item.LongUrl)
		}
	}

	// insert
	if arguments["insert"].(bool) {
		var url urlshortener.Url
		url.LongUrl = arguments["<url>"].(string)
		response, err := c.Urlshortener_Url_Insert(url)
		if err != nil {
			log.Fatalf("%+v", err)
		}
		fmt.Printf("%+v\n", response.Default.Id)
	}
}
