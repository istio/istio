package main

import (
	// "bytes"
	"context"
	"testing"

	// "encoding/base64"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	// "istio.io/istio/pilot/pkg/networking/util"

	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"github.com/buger/jsonparser"
	// sigs "github.com/sigstore/sigstore/pkg/signature"
	// sigs "github.com/sigstore/cosign/cmd/cosign/cli"
	// sigs "github.com/sigstore/cosign/pkg/signature"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/authz/builder"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pkg/config"

	meshconfig "istio.io/api/mesh/v1alpha1"

	cosigncli "github.com/sigstore/cosign/cmd/cosign/cli"
	fulcioclient "github.com/sigstore/fulcio/pkg/client"
)

const (
	rekorServerEnvKey     = "REKOR_SERVER"
	defaultRekorServerURL = "https://rekor.sigstore.dev"
	defaultOIDCIssuer     = "https://oauth2.sigstore.dev/auth"
	defaultOIDCClientID   = "sigstore"
	cosignPasswordEnvKey  = "COSIGN_PASSWORD"
)

func GetRekorServerURL() string {
	url := os.Getenv(rekorServerEnvKey)
	if url == "" {
		url = defaultRekorServerURL
	}
	return url
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}

func SignListenerFilters(configFileFolder string, listenerConfigBytes []byte) map[string][]string {
	signatures := make(map[string][]string)
	var sigList []string
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	jsonparser.ArrayEach(listenerConfigBytes, func(filterChain []byte, dataType jsonparser.ValueType, offset int, err error) {
		filterChainName, _, _, err := jsonparser.Get(filterChain, "name")
		if err != nil {
			log.Errorf("Could not find filter chain name, error = %s", err.Error())
		} else {
			log.Infof("Filter Chain Name = %s", string(filterChainName))
		}
		jsonparser.ArrayEach(filterChain, func(filter []byte, filterDataType jsonparser.ValueType, filterOffset int, err error) {
			filterName, _, _, err := jsonparser.Get(filter, "name")
			if err != nil {
				log.Errorf("Could not find filter name, error: %s", err.Error())
				return
			}
			log.Infof("Filter name: %s", filterName)
			filterTypedConfig, _, _, err := jsonparser.Get(filter, "ConfigType", "TypedConfig")
			if err != nil {
				log.Errorf("Could not find filter typed config, error: %s", err.Error())
				return
			}
			filterTypeUrl, _, _, err := jsonparser.Get(filterTypedConfig, "type_url")
			if err != nil {
				log.Errorf("Could not find filter type url, error: %s", err.Error())
				return
			}
			filterValue, _, _, err := jsonparser.Get(filterTypedConfig, "value")
			if err != nil {
				log.Errorf("Could not find filter value, nothing to sign, error: %s", err.Error())
				return
			}
			if string(filterTypeUrl) != "type.googleapis.com/envoy.extensions.filters.network.rbac.v3.RBAC" {
				return
			}

			var outDir string
			_, fileErr := os.Stat("/cfg")
			if fileErr == nil {
				outDir = "/cfg"
			} else {
				outDir = "."
			}
			outFilename := "xds_api.json"
			outFilepath := filepath.Join(outDir, outFilename)
			outFile, err := os.Create(outFilepath)
			if err != nil {
				log.Fatalf("Failed to open out file %s for writing.  Error: %v", outFilepath, err)
			}
			num, err := outFile.Write(filterValue)
			log.Infof("*** Wrote %d bytes to %s", num, outFilepath)
			outFile.Sync()
			outFile.Close()

			xdsApiResourcePath := outFilepath
			keyPath := filepath.Join(configFileFolder, "cosign.key")

			sk := false
			idToken := ""
			rekorSeverURL := GetRekorServerURL()
			fulcioServerURL := fulcioclient.SigstorePublicServerURL

			opt := cosigncli.KeyOpts{
				Sk:           sk,
				IDToken:      idToken,
				RekorURL:     rekorSeverURL,
				FulcioURL:    fulcioServerURL,
				OIDCIssuer:   defaultOIDCIssuer,
				OIDCClientID: defaultOIDCClientID,
				PassFunc:     cosigncli.GetPass,
				KeyRef:       keyPath,
			}
			log.Infof("Signing file %s", xdsApiResourcePath)
			sig, err := cosigncli.SignBlobCmd(context.Background(), opt, xdsApiResourcePath, false, "")
			if err != nil {
				log.Errorf("error occured in signing: %s", err.Error())
			} else {
				sigEnc := b64.StdEncoding.EncodeToString(sig)
				sigList = append(sigList, sigEnc)
			}
		}, "filters")
	}, "filter_chains")
	signatures["authorization_policies"] = sigList
	return signatures
}

func SignRoutePolicies(configFileFolder string, routeConfigBytes []byte) map[string][]string {
	signatures := make(map[string][]string)
	var sigList []string
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	jsonparser.ArrayEach(routeConfigBytes, func(virtualHost []byte, dataType jsonparser.ValueType, offset int, err error) {
		vhName, _, _, err := jsonparser.Get(virtualHost, "name")
		log.Infof("vhName = %s", vhName)
		jsonparser.ArrayEach(virtualHost, func(route []byte, routeDataType jsonparser.ValueType, routeOffset int, err error) {
			var configInterface map[string]interface{}
			matchPolicy, _, _, err := jsonparser.Get(route, "match")
			if err != nil {
				log.Errorf("Failed to get match policy %s", err.Error())
			}
			err = json.Unmarshal(matchPolicy, &configInterface)
			if err != nil {
				log.Errorf("Failed to unmarshal match policy %s", err.Error())
			}
			matchPolicy, err = json.Marshal(configInterface)
			if err != nil {
				log.Errorf("Failed to marshal match policy %s", err.Error())
			}
			log.Infof(string(matchPolicy))

			mirrorPolicy, _, _, err := jsonparser.Get(route, "Action", "Route", "request_mirror_policies")
			if err != nil {
				// log.Errorf("Failed to get mirror policy %s", err.Error())
			} else {
				var configMirrorInterface []map[string]interface{}
				err = json.Unmarshal(mirrorPolicy, &configMirrorInterface)
				if err != nil {
					log.Errorf("Failed to unmarshal mirror policy %s", err.Error())
				} else {
					mirrorPolicy, err = json.Marshal(configMirrorInterface)
					if err != nil {
						log.Errorf("Failed to marshal mirror policy %s", err.Error())
					} else {
						log.Infof(string(mirrorPolicy))
						var outDir string
						_, err := os.Stat("/cfg")
						if err == nil {
							outDir = "/cfg"
						} else {
							outDir = "."
						}
						outFilename := "xds_api.json"
						// tt := time.Now()
						// outFilename := fmt.Sprintf("xds_api_resources_%s.json", tt.Format("20060102150405"))
						outFilepath := filepath.Join(outDir, outFilename)
						outFile, err := os.Create(outFilepath)
						if err != nil {
							log.Fatalf("Failed to open out file %s for writing.  Error: %v", outFilepath, err)
						}
						num, err := outFile.Write(mirrorPolicy)
						log.Infof("*** Wrote %d bytes to %s", num, outFilepath)
						outFile.Sync()
						outFile.Close()

						xdsApiResourcePath := outFilepath
						keyPath := filepath.Join(configFileFolder, "cosign.key")

						sk := false
						idToken := ""
						rekorSeverURL := GetRekorServerURL()
						fulcioServerURL := fulcioclient.SigstorePublicServerURL

						opt := cosigncli.KeyOpts{
							Sk:           sk,
							IDToken:      idToken,
							RekorURL:     rekorSeverURL,
							FulcioURL:    fulcioServerURL,
							OIDCIssuer:   defaultOIDCIssuer,
							OIDCClientID: defaultOIDCClientID,
							PassFunc:     cosigncli.GetPass,
							KeyRef:       keyPath,
						}
						log.Infof("Signing file %s", xdsApiResourcePath)
						sig, err := cosigncli.SignBlobCmd(context.Background(), opt, xdsApiResourcePath, false, "")
						if err != nil {
							log.Errorf("error occured in signing: %s", err.Error())
						} else {
							sigEnc := b64.StdEncoding.EncodeToString(sig)
							sigList = append(sigList, sigEnc)
						}
					}
				}
			}
		}, "routes")
	}, "virtual_hosts")
	signatures["request_mirror_policies"] = sigList
	return signatures
}

func main() {

	t := &testing.T{}
	// t := &test.ToolErrorWrapper{}
	var configFileName, configFileFolder string

	if configFileFolder = os.Getenv("CONFIG_FILE_DIR"); configFileFolder != "" {
		log.Infof("Using config file directory %s from environment", configFileFolder)
	} else {
		configFileFolder = "."
		log.Infof("Config file path not set, using default value: %s", configFileFolder)
	}

	if configFileName = os.Getenv("CONFIG_FILE_NAME"); configFileName != "" {
		log.Infof("Using config file name %s from environment", configFileName)
	} else {
		configFileName = "authz.yaml"
		log.Infof("Config file path not set, using default value: %s", configFileName)
	}

	configFilePath := filepath.Join(configFileFolder, configFileName)
	testConfigBytes, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		log.Fatalf("Failed to read config file %s, error: %v", configFilePath, err)
	}

	testConfigString := string(testConfigBytes)
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: testConfigString})

	var proxyLabels = map[string]string{
		"app": "httpbin",
	}

	proxy := s.SetupProxy(&model.Proxy{
		ID:              "httpbin",
		ConfigNamespace: "default",
		Metadata: &model.NodeMetadata{
			Labels: proxyLabels,
		},
	})

	listeners := s.Listeners(proxy)
	log.Infof("# of listeners = %d", len(listeners))
	log.Infof("listeners = %s", prettyPrint(listeners))

	for _, listener := range listeners {
		jsonListener, err := json.Marshal(listener)
		if err != nil {
			log.Errorf("Failed to JSON Marshall listener %s, skipping", listener.Name)
			continue
		}
		listenerSignatures := SignListenerFilters(configFileFolder, jsonListener)
		if len(listenerSignatures) > 0 {
			log.Infof("To sign the Authorization Policy, please add the following lines to the CRD:")
			fmt.Println("metadata:")
			fmt.Println("  annotations:")
			for sigName, sigList := range listenerSignatures {
				fmt.Printf("    %s: %s\n", sigName, strings.Join(sigList[:], ":"))
			}
		}
	}

	routes := s.Routes(proxy)
	// log.Infof("routes = %s", prettyPrint(routes))

	for _, route := range routes {
		jsonRoute, err := json.Marshal(route)
		if err != nil {
			log.Errorf("Failed to JSON Marshall route %s, skipping", route.Name)
			continue
		}
		signatures := SignRoutePolicies(configFileFolder, jsonRoute)
		log.Infof("To sign the CRD, please add the following lines to the httpbin VirtualService:")
		fmt.Println("metadata:")
		fmt.Println("  annotations:")
		for sigName, sigList := range signatures {
			fmt.Printf("    %s: %s\n", sigName, strings.Join(sigList[:], ":"))
			/* 			for _, sig := range sigList {
			   				fmt.Println("    - ", sig)
			   			}
			*/
		}
		// log.Infof("The signature for route %s is: %v", route.Name, signatures)
	}

	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")
	log.Info("  ")

	// ads := s.ConnectADS().WithType(v3.RouteType) //.ExpectResponse()

	// log.Infof("ads = %s", prettyPrint(ads))

	tt := time.Now()
	outFilename := fmt.Sprintf("xds_api_resources_%s.json", tt.Format("20060102150405"))
	outFilepath := filepath.Join(configFileFolder, outFilename)
	outFile, err := os.Create(outFilepath)
	if err != nil {
		log.Fatalf("Failed to open out file %s for writing.  Error: %v", outFilepath, err)
	}
	// num, err := outFile.WriteString(prettyPrint(routes))
	num, err := outFile.WriteString(prettyPrint(listeners))
	if err != nil {
		log.Fatalf("Failed to write listeners to %s, error: %v", outFilepath, err)
		// log.Fatalf("Failed to write routes to %s, error: %v", outFilepath, err)
	}
	log.Infof("Wrote %d bytes to %s", num, outFilepath)
	outFile.Sync()
	outFile.Close()

	// for _, tc := range testCases {
	// 	t.Run(tc.name, func(t *testing.T) {
	// 		option := Option{
	// 			IsCustomBuilder: tc.meshConfig != nil,
	// 			Logger:          &AuthzLogger{},
	// 		}
	// 		in := inputParams(t, baseDir+tc.input, tc.meshConfig)
	// 		defer option.Logger.Report(in)
	// 		g := New(tc.tdBundle, in, option)
	// 		if g == nil {
	// 			t.Fatalf("failed to create generator")
	// 		}
	// 		got := g.BuildTCP()
	//		verify(t, convertTCP(got), baseDir, tc.want, true /* forTCP */)
	// 	})
	// }

	var (
		tdBundle   trustdomain.Bundle
		meshConfig *meshconfig.MeshConfig
	)

	/* 	{
	   		name:     "trust-domain-one-alias",
	   		tdBundle: trustdomain.NewBundle("td1", []string{"cluster.local"}),
	   		input:    "simple-policy-td-aliases-in.yaml",
	   		want:     []string{"simple-policy-td-aliases-out.yaml"},
	   	},
	   	}
	*/
	option := builder.Option{
		IsCustomBuilder: false,
		Logger:          &builder.AuthzLogger{},
	}
	policyName := "/cfg/authz.yaml"
	in, err := inputParams(policyName, meshConfig)
	if err != nil {
		log.Errorf("Failed to create input parameters: %s", err.Error())
		return
	}
	defer option.Logger.Report(in)
	g := builder.New(tdBundle, in, option)
	if g == nil {
		log.Errorf("failed to create generator")
		return
	}
	got := g.BuildHTTP()
	protoMsgs := convertHTTP(got)
	log.Infof("protoMsgs = %v", protoMsgs)

	// for idx, protoMsg := range protoMsgs {
	// 	pbanyMsg := util.MessageToAny(protoMsg)
	//	log.Infof("%d, pbanyMsg = %v", idx, pbanyMsg)
	// }

	/* 	gotYaml, err := protomarshal.ToYAML(gotHttp[0])
	   	log.Infof("gotYaml = %v", gotYaml)
	*/
	for {
		log.Infof(" ")
		time.Sleep(1000 * time.Second)
	}
}

func convertHTTP(in []*httppb.HttpFilter) []proto.Message {
	ret := make([]proto.Message, len(in))
	for i := range in {
		ret[i] = in[i]
	}
	return ret
}

func inputParams(input string, mc *meshconfig.MeshConfig) (*plugin.InputParams, error) {
	var httpbin = map[string]string{
		"app": "httpbin",
	}

	authzPolicies, err := yamlPolicy(input)
	if err != nil {
		return nil, err
	}

	ret := &plugin.InputParams{
		Node: &model.Proxy{
			ID:              "httpbin",
			ConfigNamespace: "default",
			Metadata: &model.NodeMetadata{
				Labels: httpbin,
			},
		},
		Push: &model.PushContext{
			AuthzPolicies: authzPolicies,
			Mesh:          mc,
		},
	}
	ret.Push.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{
		"httpbin.default.svc.cluster.local": {
			"default": &model.Service{
				Hostname: "httpbin.default.svc.cluster.local",
			},
		},
	}
	return ret, nil
}

func yamlPolicy(filename string) (*model.AuthorizationPolicies, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
		// t.Fatalf("failed to read input yaml file: %v", err)
	}
	c, _, err := crd.ParseInputs(string(data))
	if err != nil {
		return nil, err
		// t.Fatalf("failde to parse CRD: %v", err)
	}
	var configs []*config.Config
	for i := range c {
		configs = append(configs, &c[i])
	}

	return newAuthzPolicies(configs)
}

func newAuthzPolicies(policies []*config.Config) (*model.AuthorizationPolicies, error) {
	store := model.MakeIstioStore(memory.Make(collections.Pilot))
	for _, p := range policies {
		if _, err := store.Create(*p); err != nil {
			log.Errorf("newAuthzPolicies error: %s", err.Error())
			return nil, err
		}
	}

	authzPolicies, err := model.GetAuthorizationPolicies(&model.Environment{
		IstioConfigStore: store,
	})
	if err != nil {
		return nil, err
	}
	return authzPolicies, nil
}
