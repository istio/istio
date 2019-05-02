/*
Copyright 2018 The Kubernetes authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"log"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
	"sigs.k8s.io/controller-tools/examples/godocbot/pkg/apis"
	"sigs.k8s.io/controller-tools/examples/godocbot/pkg/controller/pullrequest"
)

var enablePRSync = flag.Bool("enable-pr-sync", false, "if set to true, periodically syncs pullrequest with Github")

func main() {
	flag.Parse()
	logf.SetLogger(logf.ZapLogger(false))

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, manager.Options{})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Registering Components.")

	// Setup all Resources
	apis.AddToScheme(mgr.GetScheme())

	stop := signals.SetupSignalHandler()

	_, err = pullrequest.NewGodocDeployer(mgr)
	if err != nil {
		log.Fatalf("failed to create godoc deployer: %v", err)
	}

	_, err = pullrequest.NewGithubSyncer(mgr, *enablePRSync, stop)
	if err != nil {
		log.Fatalf("failed to create the github pull request syncer %v", err)
	}

	log.Printf("Starting the Cmd.")

	// Start the Cmd
	log.Fatal(mgr.Start(stop))
}
