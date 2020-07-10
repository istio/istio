// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package sds

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	xdsapiv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	corev2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pkg/security"

	tlsv2 "github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	sdsv2 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"k8s.io/apimachinery/pkg/util/uuid"

	"istio.io/istio/security/pkg/nodeagent/cache"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
	mca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google/mock"
	"istio.io/istio/security/pkg/nodeagent/plugin/providers/google/stsclient"
	"istio.io/istio/security/pkg/nodeagent/secretfetcher"
	msts "istio.io/istio/security/pkg/stsservice/tokenmanager/google/mock"
)

const mockCAAddress = "localhost:0"

var (
	validCerts = []string{
		`-----BEGIN CERTIFICATE-----
MIIFiDCCA3ACCQDriJFARkUboTANBgkqhkiG9w0BAQsFADCBhTELMAkGA1UEBhMC
VVMxEzARBgNVBAgTCkNhbGlmb3JuaWExEjAQBgNVBAcTCVN1bm55dmFsZTEOMAwG
A1UEChMFSXN0aW8xDTALBgNVBAsTBFRlc3QxEDAOBgNVBAMTB1Jvb3QgQ0ExHDAa
BgkqhkiG9w0BCQEWDXRlc3RAaXN0aW8uaW8wHhcNMTgwMjI1MDg0NjA4WhcNMjgw
MjIzMDg0NjA4WjCBhTELMAkGA1UEBhMCVVMxEzARBgNVBAgTCkNhbGlmb3JuaWEx
EjAQBgNVBAcTCVN1bm55dmFsZTEOMAwGA1UEChMFSXN0aW8xDTALBgNVBAsTBFRl
c3QxEDAOBgNVBAMTB1Jvb3QgQ0ExHDAaBgkqhkiG9w0BCQEWDXRlc3RAaXN0aW8u
aW8wggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQCbA6YHmfD5VhvfvqAS
Vm7nY/Wssna19SSJ5ToInAwIFOoF2MtloZvnbs0t2K75/Vlxgfcpb8HvFFInP6Mw
qJo0T9l6T14ddZFPFJL6zSlBpPzXdkTFElCA5Tzd/2W6csZ/99N58ovmTmvlsStp
omWNtyQXmTLFt0h9WRHkTmiZq/e4aOqsUyLcMXC1+prw+YJi84H9tUdpF67kR3Li
BMzAy3SI6MvpT5MEA99P0BHRnBH8f6ITkvs6A738OAJrIB2RHljmr07SrUSZcz4B
JgUOJtYhi0qutjB+rQbCiEReykLj6+TscGp7apT1+OH5RrpaYNr6WIlOaPg6girY
a96+0YBOryoI8rczvZN+VoiXs4vlC2gkTIeRPBUx0tOJb7yZjudAfu3ivRyEYJnh
zbc9u8JDwfbvi2BtgswNlF/1hZ4ApTMVQJuln9cmQTe7f5O9pID1VcBJyVj2JJw0
71qjGOfV6AI3hhDJPtFFZInrsmD+Y8p3hYhQ0MdEuO1BVfVXWq8C/GPU6liUzQwU
RM63liLqlq4D/4jvLR3PhWby+LgrrY1qXScXfNwA+cc5pvn6gzHoo0PZJiDFJD+w
TMPqL84f+/yWN1StvzvECj9VSpFusu4BgLBIQZEcdGhv2or/Cp889qZK06yBgcNs
4bF4qYN1nd0iXGpaFQVAdRvXiQIDAQABMA0GCSqGSIb3DQEBCwUAA4ICAQA8W6AT
S6zn6WEXRvXAD389QKPtEHQdU8cuiAfxwmAhilJisJk+snTLspB9BtXBH0ZyyNRd
4Jg3aVGu9ylF7Bir0/i3Za+SB7jKjs4XnUnItzJHQXeo1cKY145ubI6bmgaFSNIq
A+pyXDfAQ7PHFPVVRLdXtKgQaJ5qvs++IoVzQTWKUq6XK2WHVlhPtsmovexsI1o0
RUxR/cAdF4qkNamQm00jemjJTaC3VKPREILNGUudiQ1uX5n8yWvtnakGbkNKciO2
jCW6Y0xYinrGM/HmRzEfCloFO7Gq7Q+JZXQsVQbCesw52MgtgXm7H36dMSAkP3YV
aFbIfGN0mpv93kXy0YjzExBh5xYDfFnSDB/1tz7gCb4IEqhCq/iHz4qTBQ4NeUNQ
v/m6dabQ0UYmRupB2ALj9+4cXiCQbn4hxiv7OKeGcR/Z6VI11/sPaxe/uZUdqQhe
qnKUZhHhyibzviYLU5KasT4IsQe4eOu5V/qzSYpB5p2Cw8foDclW7Ekxa/E0CACz
CEn9xDtFZjjYJDw1ujo23Kc6k/FBKF5QQhzDA5nWVubT3hC/RK6FR/4GtzwR2QUe
i5RRLNCjX3KV3JXoAiksmn7sE8Bi6fJjYOStK0EQSPTqUWOe+32iSFDC/FX6PU5s
yPI4TT95yXzBqNaTU1+Igrk2nl7aDTcOKrpwxg==
-----END CERTIFICATE-----
`,
		`-----BEGIN CERTIFICATE-----
MIIFfjCCA2agAwIBAgIJAObrfK7z1ClEMA0GCSqGSIb3DQEBCwUAMIGFMQswCQYD
VQQGEwJVUzETMBEGA1UECBMKQ2FsaWZvcm5pYTESMBAGA1UEBxMJU3Vubnl2YWxl
MQ4wDAYDVQQKEwVJc3RpbzENMAsGA1UECxMEVGVzdDEQMA4GA1UEAxMHUm9vdCBD
QTEcMBoGCSqGSIb3DQEJARYNdGVzdEBpc3Rpby5pbzAeFw0xODAyMjUwODQ2MDla
Fw0yODAyMjMwODQ2MDlaMFgxCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJDQTESMBAG
A1UEBwwJU3Vubnl2YWxlMQ4wDAYDVQQLDAVJc3RpbzEYMBYGA1UEAwwPSW50ZXJt
ZWRpYXRlIENBMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAu/07sB5q
zWz0rkdZRc5t2tc8rg2+ZmFxuhy1zdvCqPV4cY8Ts+7h/F8MANiv40yD4LjHLlIK
I0a3OjtzCLHo7RkHBEKut4qMfbns8Y75/Gh+0h3+T+N0G0LJGCJntr24brlXqbNc
rUFmkUQesFfRAfyko2x5akJ0lrgCgdHJfyyYLgo1RDCxsDbKtdTS4gpju8ekXtan
AuUKNmQuRhpMztQ7EA4/LfNEQPh0RWmqrQ1ALlHSKz8n66HGFycD2e90I3JQH34a
VVVkxhjhaURR7SrdA4FMOuRKtK94XXpKRfAYTaE89XbHoOe7TaN40rApWT3XqpDv
RcpMRL7OUgJ+H4dt9n9/p/JnXauRG2AAMS0jm59/btqCeHfF145orkDrL/mm6vWq
lA1jGDA607RFEk16D39+rX8gH9NgciSm+z3yKwisVZhyJ79/+rcweeWnc17PjSV9
RcUar7dsfrUU+kEsFKy+QOQUfDonfE1Of7VLEqVIYh7z2T1KC+nvELajPvD9Ciq3
19XGLPG9PSuCvEvtm1jXTHmbOqxpAVAPxTfFC9Z+JM3RYJdD0dwoipMco5/FvrAi
nnYA9AefNqz4lcC6IY1lBaXZAJaoMep/x40EhSF5+a262YTuKEkaZ0amutrpE0w/
7LNLdxp9ekosEkb47XH7W94DYM5Zfl5Ee3ECAwEAAaMdMBswDAYDVR0TBAUwAwEB
/zALBgNVHQ8EBAMCAuQwDQYJKoZIhvcNAQELBQADggIBAHn3tgei3SwGjxJ/x3yI
mE8PoxYO38qgg2njIMJ1jMyAsrcpeKBiC1K7b/KYZMPZuKoVT+odMebv0NIn36f2
ubVL7rmIe52w4lDmCMqBMGTSsWWvs5+O9zAw1pcrslGrXF/M+eM7G4D+GpO7YxLU
12J87lwDSq8UtMVhqErWanut/cao2yi9c2t9Otduil8DnQ/xpDMI1kKs0hCtf4KW
qAhLyLxfNrlFagytVzLb4do8CRZcCrCLgqcnFt6cEQiq2fMIf78fu8ltinXN+DvB
hpOnDEHKoIKZhMP9ezZIRgauR9UZDfAMj4NS36dWT+7mY7CfUAB+/d6OGvdAF3B1
VuLT+rP9e9ud7FfTGZfnamT71pgRZi9vTQLP3WY8UBPQ9Ce3PTUw6tOhobTKlo78
n8CF+Pv6U7uzQ0f4g7U+lf5T4hurOLVI7D5uPgT0u/Qw31IBvlAF7Mg5c6Su/CH6
sKzEIc97Qsnjr+ZVvnldLSv63wiws0QH2J7lgn+u6JWQ0JgixjiDQo1vgsqNvnsD
dOhb/Y9XceSf2gpvzR5JqZ1kORnKOO5RRfCoQuoRr9r0iyJSlKbPLwNyKMJOSkeD
8nigsfOm0+TvsDaX5/8wXDGwlkuAwVojrKRmmTH7PfhrC1grtk/x5dmArgobdBpe
oLSC9kB8RdqG6uMY/m51Dbsi
-----END CERTIFICATE-----
`,
		`-----BEGIN CERTIFICATE-----
MIIFUTCCAzmgAwIBAgIJAJFK5GFLpQYTMA0GCSqGSIb3DQEBCwUAMFgxCzAJBgNV
BAYTAlVTMQswCQYDVQQIDAJDQTESMBAGA1UEBwwJU3Vubnl2YWxlMQ4wDAYDVQQL
DAVJc3RpbzEYMBYGA1UEAwwPSW50ZXJtZWRpYXRlIENBMB4XDTE4MDIyNTA4NDYw
OVoXDTI4MDIyMzA4NDYwOVowWTELMAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMRIw
EAYDVQQHDAlTdW5ueXZhbGUxDjAMBgNVBAsMBUlzdGlvMRkwFwYDVQQDDBBJbnRl
cm1lZGlhdGUgQ0EyMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEAmZgW
899dLmozj2pxxdeHEgaBuHeWJih8QRfp4hCsm+E5FuCYDu9JDk401JiNjovB5ciW
sAJRr+pxnUET2dNbCaPf1Ze3cuJ9uttawE6KmsL19dBPyhjYDH2Hbn2XnxvL8Z1L
2jleG6Cdu+UtL1HS8VoLQAwsE/9IRKgxj0nIsw3ZOdKbJWOTCsthjIcn5V2xVRRb
aN8wpbtTdGEir4Wjk4gngT//xDLJaw3w5oPgFf9n0SHQvIp+p0TbCZjVk5s6oZ4E
qMV6U1bGlzNC8O1V0AwQzOH3hT/WNC6uoxI0ebAwXGhrwqtAy/0gyDR3EO/hXP9F
GporcR4j5OJRm2zYZbz6NSwytya6MVuZqINJqbQKaW6ynMGVGJ8mFLITL/xVg5OP
tIFlaXqxhQCooY3lQ2KrfLxRxcgYikfNGvV+fE+VCKpnCaqTRmEsWepuFQ+Fovx4
uax+JXWmuWyR8To7CmZ8sEiT6aCtwimCSoH6HfV8GXs9fvxqYkMypMeow7aNud5X
JevDC7IgjTkAEqMOaMPK0TSiP++PP4WhnJno1tVVvY9nGsEGW1dA+xhWyOfLplz6
4pXrWMMqjaNPUdDVH2w3CzB/zFRE7qYwfWRxVTSdYO/7zh1mrcFZMgzzcpQLNVDw
j6mPpLxLuAxnWjSnjHh78iPGeH+ZR2zR4v5SR9sCAwEAAaMdMBswDAYDVR0TBAUw
AwEB/zALBgNVHQ8EBAMCAuQwDQYJKoZIhvcNAQELBQADggIBAAvKvbPGOJsBQdDA
ViBSBTZk+aX1+vGxCpWjCaE/tR3weYdLRDy1sPrzPQf1ROdppoLapP4neez/BVvc
vXL0fadU4Qe9/p5hwE3A+sSEf8zc+rRqgl/ic8zZ5hFvaxWPLOU2zN2SvGEsbfSD
NkKX+qZ4LkcWqVqN7zepAI7D1w7Bdc77+W19rcQOe2OmVNSyN65Fr54JkYcb3bOG
ziaKdY22zoqx2vM5yu3RDXngJ/mdDLbdx+vmAJ4/8c1SryRBd1pN99u8tBfuy8FR
qawIxn0Squ/qdqYlIChd5uSgVgSdKvJS7KlfMf1a1EdVeiw4E6GMatwtRV8rVUFj
eBtSipMA6kJ2rLSC8hIybzMpJFxYPAC3ZhoqsLijba3YhnB//FG0tUAlTibUfVYs
jcIEXfsi4PN+vr5fi8NArWP4lQdFTD47oC3e9Jnjp/KDVe9bXQzb5ShaL07LV4Bb
Rfz7NMoXvH9HYK0X6+0JH//nq7BaDWXsNqXOuI6BhVutrUglJ/3smvalnMWCy0Jt
mMZV9MqJm1xiDpv5XzJk5CNTml7Svk5T+3kcZRwy6WU99VBC5FC5j2eWeCn3KHVY
ZxcT2U8ZtXBIcU6OpyU8vVUPjJ/lyVem+W9qRkmHkHVLsmNFCbb6YycRyBCgnQ0J
yTi7LtqQOBVq0veaVudHd+9I/JrJ
-----END CERTIFICATE-----
`}
)

func createRealSDSServer(t *testing.T, socket string) *Server {
	// Create a local grpc server to mock Mesh CA
	caService := &mca.CAService{Certs: validCerts, Err: nil}
	mockMeshCAServer, err := mca.CreateServer(mockCAAddress, caService)
	if err != nil {
		t.Fatalf("Mock Mesh CA failed to start: %v", err)
	}
	fmt.Println("CA server is up.")

	// Create a local HTTP server to mock Cloud Gaia
	mockSTSServer, err := msts.StartNewServer(t, msts.Config{Port: 0})
	if err != nil {
		t.Fatalf("Mock STS server failed to start: %v", err)
	}
	fmt.Println("STS server is up.")

	// Create a SDS server talking to the fake servers
	stsclient.GKEClusterURL = msts.FakeGKEClusterURL
	stsclient.SecureTokenEndpoint = mockSTSServer.URL + "/v1/identitybindingtoken"
	arg := security.Options{
		EnableGatewaySDS:  false,
		EnableWorkloadSDS: true,
		RecycleInterval:   100 * time.Millisecond,
		WorkloadUDSPath:   socket,
	}
	caClient, err := gca.NewGoogleCAClient(mockMeshCAServer.Address, false)
	if err != nil {
		t.Fatalf("failed to create secretFetcher for workload proxy: %v", err)
	}

	wSecretFetcher := &secretfetcher.SecretFetcher{
		UseCaClient: true,
		CaClient:    caClient,
	}

	workloadSdsCacheOptions := &security.Options{}
	workloadSdsCacheOptions.TrustDomain = "FakeTrustDomain"
	workloadSdsCacheOptions.Pkcs8Keys = false
	workloadSdsCacheOptions.TokenExchangers = NewPlugins([]string{"GoogleTokenExchange"})
	workloadSdsCacheOptions.RotationInterval = 10 * time.Minute
	workloadSdsCacheOptions.InitialBackoffInMilliSec = 10
	workloadSecretCache := cache.NewSecretCache(wSecretFetcher, NotifyProxy, workloadSdsCacheOptions)

	server, err := NewServer(&arg, workloadSecretCache, nil)
	if err != nil {
		t.Fatalf("failed to start grpc server for sds: %v", err)
	}

	// The goroutine starting the server may not be ready, results in flakiness.
	time.Sleep(1 * time.Second)

	return server
}

func runSDSClientBasicV2(stream sdsv2.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan notifyMsg) {
	req := &xdsapiv2.DiscoveryRequest{
		TypeUrl:       SecretTypeV2,
		ResourceNames: []string{testResourceName},
		Node: &corev2.Node{
			Id: proxyID,
		},
		// Set a non-empty version info so that StreamSecrets() starts a cache check, and cache miss
		// metric is updated accordingly.
		VersionInfo: "initial_version",
	}
	if err := stream.Send(req); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"stream: stream.Send failed: %v", err)}
	}
	resp, err := stream.Recv()
	if err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"stream: stream.Recv failed: %v", err)}
	}
	if err = validateSDSSResponseV2(resp); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"SDS response validation failed: %v", err)}
	}

	notifyChan <- notifyMsg{Err: nil, Message: "close stream"}
}

func runSDSClientBasic(stream sds.SecretDiscoveryService_StreamSecretsClient, proxyID string,
	notifyChan chan notifyMsg) {
	req := &discovery.DiscoveryRequest{
		TypeUrl:       SecretTypeV3,
		ResourceNames: []string{testResourceName},
		Node: &core.Node{
			Id: proxyID,
		},
		// Set a non-empty version info so that StreamSecrets() starts a cache check, and cache miss
		// metric is updated accordingly.
		VersionInfo: "initial_version",
	}
	if err := stream.Send(req); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"stream: stream.Send failed: %v", err)}
	}
	resp, err := stream.Recv()
	if err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"stream: stream.Recv failed: %v", err)}
	}
	if err = validateSDSSResponse(resp); err != nil {
		notifyChan <- notifyMsg{Err: err, Message: fmt.Sprintf(
			"SDS response validation failed: %v", err)}
	}

	notifyChan <- notifyMsg{Err: nil, Message: "close stream"}
}

func createSDSClient(t *testing.T, socket string) (*grpc.ClientConn, sds.SecretDiscoveryService_StreamSecretsClient) {
	// Try to call the server
	conn, err := setupConnection(socket)
	if err != nil {
		t.Errorf("failed to setup connection to socket %q", socket)
	}
	sdsClient := sds.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(credentialTokenHeaderKey, "FakeSubjectToken")
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		t.Errorf("StreamSecrets failed: %v", err)
	}
	return conn, stream
}

// Create a legacy v2 XDS server
func createSDSClientV2(t *testing.T, socket string) (*grpc.ClientConn, sdsv2.SecretDiscoveryService_StreamSecretsClient) {
	// Try to call the server
	conn, err := setupConnection(socket)
	if err != nil {
		t.Errorf("failed to setup connection to socket %q", socket)
	}
	sdsClient := sdsv2.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(credentialTokenHeaderKey, "FakeSubjectToken")
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		t.Errorf("StreamSecrets failed: %v", err)
	}
	return conn, stream
}

// This is the same as TestNodeAgentBasic, but connects as a legacy v2 SDS client
func TestNodeAgentBasicV2(t *testing.T) {
	// reset connectionNumber since since its value is kept in memory for all unit test cases
	// lifetime, reset since it may be updated in other test case.
	atomic.StoreInt64(&connectionNumber, 0)

	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	server := createRealSDSServer(t, socket)
	defer server.Stop()

	connTwo, streamTwo := createSDSClientV2(t, socket)
	proxyIDTwo := "sidecar~127.0.0.1~SecretsPushStreamTwo~local"
	notifyChanTwo := make(chan notifyMsg)
	go runSDSClientBasicV2(streamTwo, proxyIDTwo, notifyChanTwo)
	waitForNotificationToProceed(t, notifyChanTwo, "close stream")
	connTwo.Close()
}

func TestNodeAgentBasic(t *testing.T) {
	// reset connectionNumber since since its value is kept in memory for all unit test cases
	// lifetime, reset since it may be updated in other test case.
	atomic.StoreInt64(&connectionNumber, 0)

	socket := fmt.Sprintf("/tmp/gotest%s.sock", string(uuid.NewUUID()))
	server := createRealSDSServer(t, socket)
	defer server.Stop()

	connTwo, streamTwo := createSDSClient(t, socket)
	proxyIDTwo := "sidecar~127.0.0.1~SecretsPushStreamTwo~local"
	notifyChanTwo := make(chan notifyMsg)
	go runSDSClientBasic(streamTwo, proxyIDTwo, notifyChanTwo)
	waitForNotificationToProceed(t, notifyChanTwo, "close stream")
	connTwo.Close()
}

func validateSDSSResponseV2(resp *xdsapiv2.DiscoveryResponse) error {
	if resp == nil {
		return fmt.Errorf("response is nil")
	}
	var secret tlsv2.Secret
	if err := ptypes.UnmarshalAny(resp.Resources[0], &secret); err != nil {
		return fmt.Errorf("unmarshalAny SDS response failed: %v", err)
	}

	tlsCert, ok := secret.Type.(*tlsv2.Secret_TlsCertificate)
	if !ok {
		return fmt.Errorf("error validating SDS response: response secret type conversion failed")
	}
	certChain := string(tlsCert.TlsCertificate.CertificateChain.GetInlineBytes())
	privateKey := string(tlsCert.TlsCertificate.GetPrivateKey().GetInlineBytes())

	caCerts := strings.Replace(validCerts[0]+validCerts[1]+validCerts[2], "\n", "", -1)
	sdsCerts := strings.Replace(certChain, "\n", "", -1)

	if caCerts != sdsCerts {
		return fmt.Errorf("error validating SDS response: certs do not match:\n%s\nVS:\n%s", caCerts, sdsCerts)
	}

	if len(privateKey) == 0 {
		return fmt.Errorf("error validating SDS response: private key is empty")
	}

	return nil
}
func validateSDSSResponse(resp *discovery.DiscoveryResponse) error {
	var secret tls.Secret
	if resp == nil {
		return fmt.Errorf("response is nil")
	}
	if err := ptypes.UnmarshalAny(resp.Resources[0], &secret); err != nil {
		return fmt.Errorf("unmarshalAny SDS response failed: %v", err)
	}

	tlsCert, ok := secret.Type.(*tls.Secret_TlsCertificate)
	if !ok {
		return fmt.Errorf("error validating SDS response: response secret type conversion failed")
	}

	caCerts := strings.Replace(validCerts[0]+validCerts[1]+validCerts[2], "\n", "", -1)
	sdsCerts := strings.Replace(string(tlsCert.TlsCertificate.CertificateChain.GetInlineBytes()), "\n", "", -1)

	if caCerts != sdsCerts {
		return fmt.Errorf("error validating SDS response: certs do not match:\n%s\nVS:\n%s", caCerts, sdsCerts)
	}

	if len(tlsCert.TlsCertificate.GetPrivateKey().GetInlineBytes()) == 0 {
		return fmt.Errorf("error validating SDS response: private key is empty")
	}

	return nil
}
