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

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/terminal"

	clientnetworking "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclient "istio.io/client-go/pkg/clientset/versioned"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/security/pkg/k8s/secret"
	"istio.io/istio/security/pkg/pki/util"
)

var (
	all               bool
	certDuration      time.Duration
	dumpDir           string
	istioProxyImage   string
	mutualTLS         bool
	organization      string
	remoteDirectory   string
	scpPath           string
	scpTimeout        time.Duration
	spiffeTrustDomain string
	sshAuthMethod     ssh.AuthMethod
	sshKeyLocation    string
	sshIgnoreHostKeys bool
	sshPort           int
	sshUser           string
	startIstio        bool
)

type workloadEntryAddressKeys struct {
	CaCert           []byte
	Cert             []byte
	Key              []byte
	ServiceName      string
	ServiceNamespace string
}

type describedCert struct {
	PemEncodedCa []byte
	Ca           *x509.Certificate
	Key          crypto.PrivateKey
}

type remoteResponse struct {
	typ     uint8
	message string
}

func fetchSingleWorkloadEntry(workloadName string, client istioclient.Interface) ([]clientnetworking.WorkloadEntry, string, error) {
	workloadSplit := strings.Split(workloadName, ".")
	if len(workloadSplit) != 2 {
		return nil, "", fmt.Errorf("workload name: %s is not in the format: workloadName.Namespace", workloadName)
	}

	we, err := client.NetworkingV1alpha3().WorkloadEntries(workloadSplit[1]).Get(context.Background(), workloadSplit[0], metav1.GetOptions{})
	if we == nil || err != nil {
		return nil, "", fmt.Errorf("workload entry: %s in namespace: %s was not found", workloadSplit[0], workloadSplit[1])
	}

	return []clientnetworking.WorkloadEntry{*we}, workloadSplit[1], nil
}

func fetchAllWorkloadEntries(client istioclient.Interface) ([]clientnetworking.WorkloadEntry, string, error) {
	list, err := client.NetworkingV1alpha3().WorkloadEntries(namespace).List(context.Background(), metav1.ListOptions{})
	return list.Items, namespace, err
}

func getCertificate(kubeClient kubernetes.Interface) (describedCert, error) {
	secret, err := kubeClient.CoreV1().Secrets(istioNamespace).Get(context.TODO(), secret.CASecret, metav1.GetOptions{})
	if err != nil {
		return describedCert{}, err
	}

	key, err := util.ParsePemEncodedKey(secret.Data["ca-key.pem"])
	if err != nil {
		return describedCert{}, err
	}
	certContents := secret.Data["ca-cert.pem"]
	cert, err := util.ParsePemEncodedCertificate(certContents)
	if err != nil {
		return describedCert{}, err
	}

	return describedCert{
		PemEncodedCa: certContents,
		Ca:           cert,
		Key:          key,
	}, nil
}

func extractOrgName(cert *x509.Certificate) string {
	return cert.Subject.Organization[0]
}

func getCertificatesForEachAddress(
	workloadEntries []clientnetworking.WorkloadEntry,
	namespace string,
	root describedCert) (map[string]workloadEntryAddressKeys, error) {
	seenIps := make(map[string]workloadEntryAddressKeys)

	if organization == "" {
		organization = extractOrgName(root.Ca)
	}

	if spiffeTrustDomain != "" {
		spiffe.SetTrustDomain(spiffeTrustDomain)
	}

	for _, entryCfg := range workloadEntries {
		wle := entryCfg.Spec
		// Only generate one certificate per address.
		if _, ok := seenIps[wle.Address]; ok {
			continue
		}
		if wle.ServiceAccount == "" {
			return nil, fmt.Errorf("cannot generate certificate for a workload entry without a service account")
		}

		spiffeURI, err := spiffe.GenSpiffeURI(namespace, wle.ServiceAccount)
		if err != nil {
			return nil, err
		}

		signerOpts := util.CertOptions{
			Host:         spiffeURI,
			NotBefore:    time.Now(),
			TTL:          certDuration,
			SignerCert:   root.Ca,
			SignerPriv:   root.Key,
			Org:          organization,
			IsCA:         false,
			IsSelfSigned: false,
			IsClient:     true,
			IsServer:     true,
			RSAKeySize:   2048,
		}
		certPem, privPem, err := util.GenCertKeyFromOptions(signerOpts)
		if err != nil {
			return nil, err
		}

		seenIps[wle.Address] = workloadEntryAddressKeys{
			root.PemEncodedCa,
			certPem,
			privPem,
			wle.ServiceAccount,
			namespace,
		}
	}

	return seenIps, nil
}

func parseRemoteResponse(reader io.Reader) (*remoteResponse, error) {
	buffer := make([]uint8, 1)
	if _, err := reader.Read(buffer); err != nil {
		return nil, err
	}

	typ := buffer[0]
	if typ > 0 {
		buf := bufio.NewReader(reader)
		message, err := buf.ReadString('\n')
		if err != nil {
			return nil, err
		}
		return &remoteResponse{typ, message}, nil
	}

	return &remoteResponse{typ: typ, message: ""}, nil
}

func checkRemoteResponse(r io.Reader) error {
	response, err := parseRemoteResponse(r)
	if err != nil {
		return err
	}

	if response.typ > 0 {
		return errors.New(response.message)
	}

	return nil
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally.
	case <-time.After(timeout):
		return true // timed out.
	}
}

func remoteCopyFile(data []byte, location string, client *ssh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	filename := path.Base(location)
	r := bytes.NewReader(data)

	wg := sync.WaitGroup{}
	wg.Add(2)
	errCh := make(chan error, 2)

	size := len(data)

	go func() {
		defer wg.Done()
		w, err := session.StdinPipe()
		if err != nil {
			errCh <- err
			return
		}
		defer w.Close()

		stdout, err := session.StdoutPipe()
		if err != nil {
			errCh <- err
			return
		}

		// Set the unix file permissions to `0644`.
		//
		// If you don't read unix permissions this correlates to:
		//
		//   Owning User: READ/WRITE
		//   Owning Group: READ
		//   "Other": READ.
		//
		// We keep "OTHER"/"OWNING GROUP" to read so this seemlessly
		// works with the Istio container we start up below.
		_, err = fmt.Fprintln(w, "C0644", size, filename)
		if err != nil {
			errCh <- err
			return
		}

		if err = checkRemoteResponse(stdout); err != nil {
			errCh <- err
			return
		}

		_, err = io.Copy(w, r)
		if err != nil {
			errCh <- err
			return
		}

		_, err = fmt.Fprint(w, "\x00")
		if err != nil {
			errCh <- err
			return
		}

		if err = checkRemoteResponse(stdout); err != nil {
			errCh <- err
			return
		}
	}()

	go func() {
		defer wg.Done()
		err := session.Run(fmt.Sprintf("%s -qt %s", scpPath, location))
		if err != nil {
			errCh <- err
			return
		}
	}()

	if waitTimeout(&wg, scpTimeout) {
		return fmt.Errorf("timeout uploading file")
	}

	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func remoteRunCommand(command string, client *ssh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return session.Run(command)
}

func dumpCertificates(directory string, addressCertMapping map[string]workloadEntryAddressKeys) error {
	err := os.MkdirAll(directory, os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return err
	}
	for address, certs := range addressCertMapping {
		err = ioutil.WriteFile(path.Join(directory, "cert-"+address+".pem"), certs.Cert, 0644)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(path.Join(directory, "key-"+address+".pem"), certs.Key, 0644)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(path.Join(directory, "ca-cert-"+address+".pem"), certs.CaCert, 0644)
		if err != nil {
			return err
		}
	}
	return nil
}

func addressToPodNameAddition(address string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(address)))[0:7]
}

func deriveDiscoveryAddressPort(service string, namespace string, kubeClient kubernetes.Interface) (string, error) {
	foundService, err := kubeClient.CoreV1().Services(namespace).Get(context.TODO(), service, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, port := range foundService.Spec.Ports {
		if strings.HasPrefix(port.Name, "tcp-istiod") {
			return string(port.NodePort), nil
		}
	}
	return "", fmt.Errorf("failed to find tcp-istiod port")
}

func copyCertificates(kubeClient kubernetes.Interface,
	addressCertMapping map[string]workloadEntryAddressKeys) error {
	var callback ssh.HostKeyCallback
	if sshIgnoreHostKeys {
		callback = ssh.InsecureIgnoreHostKey()
	} else {
		user, err := user.Current()
		if err != nil {
			return err
		}
		callback, err = knownhosts.New(path.Join(user.HomeDir, ".ssh", "known_hosts"))
		if err != nil {
			return err
		}
	}

	for address, certs := range addressCertMapping {
		sshConfig := &ssh.ClientConfig{
			User:            sshUser,
			Auth:            []ssh.AuthMethod{sshAuthMethod},
			HostKeyCallback: callback,
		}
		client, err := ssh.Dial("tcp", address+":"+strconv.Itoa(sshPort), sshConfig)
		if err != nil {
			return err
		}
		// Ensure the remote directory exists.
		mkdirErr := remoteRunCommand("mkdir -p "+remoteDirectory, client)
		if mkdirErr != nil {
			client.Close()
			return err
		}

		remoteCaPath := path.Join(remoteDirectory, "ca-cert-"+address+".pem")
		err = remoteCopyFile(certs.CaCert, remoteCaPath, client)
		if err != nil {
			client.Close()
			return err
		}

		remoteCertPath := path.Join(remoteDirectory, "cert-"+address+".pem")
		err = remoteCopyFile(certs.Cert, remoteCertPath, client)
		if err != nil {
			client.Close()
			return err
		}
		remoteKeyPath := path.Join(remoteDirectory, "key-"+address+".pem")
		err = remoteCopyFile(certs.Key, remoteKeyPath, client)
		if err != nil {
			client.Close()
			return err
		}

		if startIstio {
			addressIdentifier := addressToPodNameAddition(address)
			istiodPort, err := deriveDiscoveryAddressPort(certs.ServiceName, certs.ServiceNamespace, kubeClient)
			if err != nil {
				client.Close()
				return err
			}
			discoveryAddress := "istiod.istio-system.svc:" + istiodPort
			var authPolicyStr string
			if mutualTLS {
				authPolicyStr = "MUTUAL_TLS"
			} else {
				authPolicyStr = "NONE"
			}

			meshPath := path.Join(remoteDirectory, "mesh.yaml")
			err = remoteCopyFile([]byte(`---
defaultConfig:
	serviceCluster:`+certs.ServiceNamespace+`
	controlPlaneAuthPolicy: `+authPolicyStr+`
	discoveryAddress: `+discoveryAddress), meshPath, client)
			if err != nil {
				client.Close()
				return err
			}

			//you need to deal with Sidecar CR if you want it to be "non-captured" mode
			err = remoteRunCommand(
				"docker run -d --name istio-proxy --network host "+
					"-v "+meshPath+":/etc/istio/config/mesh "+
					"-v "+remoteCaPath+":/var/run/secrets/istio/root-cert.pem "+
					"-v "+remoteCertPath+":/var/run/secrets/istio/cert-chain.pem "+
					"-v "+remoteKeyPath+":/var/run/secrets/istio/key.pem "+
					"--add-host istio-pilot.istio-system.svc:"+address+" "+
					"--add-host istiod.istio-system.svc:"+address+" "+
					"-e POD_NAME="+certs.ServiceName+"-"+addressIdentifier+" "+
					"-e JWT_POLICY=none "+
					"-e PROV_CERT=/var/run/secrets/istio "+
					"-e OUTPUT_CERTS=/var/run/secrets/istio "+
					"-e PILOT_CERT_PROVIDER=istiod "+
					"-e POD_NAMESPACE="+certs.ServiceNamespace+" "+
					"-e ISTIO_META_WORKLOAD_NAME="+certs.ServiceName+" "+
					istioProxyImage+" proxy sidecar", client)
			if err != nil {
				client.Close()
				return err
			}
		}

		client.Close()
	}

	return nil
}

func deriveSSHMethod() error {
	if sshKeyLocation == "" {
		term := terminal.NewTerminal(os.Stdin, "")
		var err error
		sshPassword, err := term.ReadPassword("Please enter the SSH password: ")
		if err != nil {
			return err
		}
		if sshPassword == "" {
			return fmt.Errorf("a password, or SSH key location is required for sidecar-bootstrap")
		}
		sshAuthMethod = ssh.Password(sshPassword)
	} else {
		// Attempt to parse the key.
		rawKey, err := ioutil.ReadFile(sshKeyLocation)
		if err != nil {
			return err
		}
		key, err := ssh.ParsePrivateKey(rawKey)
		if err != nil {
			if err, ok := err.(*ssh.PassphraseMissingError); ok {
				term := terminal.NewTerminal(os.Stdin, "")
				sshKeyPassword, err := term.ReadPassword("Please enter the password for the SSH key: ")
				if err != nil {
					return err
				}
				decryptedKey, err := ssh.ParsePrivateKeyWithPassphrase(rawKey, []byte(sshKeyPassword))
				if err != nil {
					return err
				}
				sshAuthMethod = ssh.PublicKeys(decryptedKey)
			} else {
				return err
			}
		} else {
			sshAuthMethod = ssh.PublicKeys(key)
		}
	}

	return nil
}

func vmBootstrapCommand() *cobra.Command {
	vmBSCommand := &cobra.Command{
		Use:   "sidecar-bootstrap <workloadEntry>.<namespace>",
		Short: "(experimental) bootstraps a non-kubernetes workload (e.g. VM, Baremetal) onto an Istio mesh",
		Long: `(experimental) Takes in one or more WorkloadEntries generates identities for them, and copies to
the particular identities to the workloads over SSH. Optionally allowing for saving the certificates locally
for use in CI like environments, and starting istio-proxy where no special configuration is needed.
This allows for workloads to participate in the Istio mesh.

To autenticate to a remote node you can use either SSH Keys, or SSH Passwords. If using passwords you
must have a TTY for you to be asked your password, we do not accept an argument for it so it
cannot be left inside your shell history.

Copying is performed with scp, and as such is required if you'd like to copy a file over.
If SCP is not at the standard path "/usr/bin/scp", you should provide it's location with
the "--remote-scp-path" option.

In order to start Istio on the remote node you must have docker installed on the remote node.
Istio will be started on the host network as a docker container in capture mode.`,
		Example: `  # Copy certificates to a WorkloadEntry named "we" in the "ns" namespace:
	istioctl x sidecar-bootstrap we.ns

	# Copy certificates, and start istio to a WorkloadEntry named "we" in the "ns" namespace:
	istioctl x sidecar-bootstrap we.ns --start-istio-proxy

	# Generate Certs locally, but do not copy them to a WorkloadEntry named "we" in the "ns" namespace:
	istioctl x sidecar-bootstrap we.ns --local-dir path/where/i/want/certs/`,
		Args: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) == all {
				cmd.Println(cmd.UsageString())
				return fmt.Errorf("sidecar-bootstrap requires a workload entry, or the --all flag")
			}
			if all && namespace == "" {
				return fmt.Errorf("sidecar-bootstrap needs a namespace if fetching all workspaces")
			}
			if !startIstio && istioProxyImage != "istio/proxyv2:latest" {
				return fmt.Errorf("sidecar-bootstrap received a non default IstioProxy image argument, but is not starting Istio")
			}
			if sshUser == "" {
				user, err := user.Current()
				if err != nil {
					return err
				}
				sshUser = user.Username
			}
			if dumpDir == "" {
				err := deriveSSHMethod()
				if err != nil {
					return err
				}
			}

			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var configClient istioclient.Interface
			var err error

			if configClient, err = configStoreFactory(); err != nil {
				return err
			}

			var entries []clientnetworking.WorkloadEntry
			var chosenNS string
			if all {
				entries, chosenNS, err = fetchAllWorkloadEntries(configClient)
			} else {
				entries, chosenNS, err = fetchSingleWorkloadEntry(args[0], configClient)
			}
			if err != nil {
				return err
			}

			kubeClient, err := interfaceFactory(kubeconfig)
			if err != nil {
				return err
			}

			certs, err := getCertificate(kubeClient)
			if err != nil {
				return err
			}

			addresses, err := getCertificatesForEachAddress(entries, chosenNS, certs)
			if err != nil {
				return err
			}

			if dumpDir != "" {
				err = dumpCertificates(dumpDir, addresses)
				if err != nil {
					return err
				}
			} else {
				err = copyCertificates(kubeClient, addresses)
				if err != nil {
					return err
				}
			}

			return nil
		},
	}

	vmBSCommand.PersistentFlags().BoolVarP(&all, "all", "a", false,
		"attempt to bootstrap all workload entries")
	vmBSCommand.PersistentFlags().DurationVar(&certDuration, "duration", 365*24*time.Hour,
		"(experimental) duration the certificates generated are valid for.")
	vmBSCommand.PersistentFlags().StringVarP(&dumpDir, "local-dir", "d", "",
		"directory to place certs in locally as opposed to copying")
	vmBSCommand.PersistentFlags().StringVar(&istioProxyImage, "istio-image", "istio/proxyv2:latest",
		"(experimental) the Istio proxy image to start up when starting Istio")
	vmBSCommand.PersistentFlags().BoolVar(&mutualTLS, "mutual-tls", false,
		"(experimental) enable mutual TLS if starting Istio-Proxy.")
	vmBSCommand.PersistentFlags().StringVarP(&organization, "organization", "o", "",
		"(experimental) the organization to use on the certificate, defaults to the same as the root cert.")
	vmBSCommand.PersistentFlags().StringVar(&remoteDirectory, "remote-directory", "/var/run/istio",
		"(experimental) the directory to create on the remote machine.")
	vmBSCommand.PersistentFlags().StringVar(&scpPath, "remote-scp-path", "/usr/bin/scp",
		"(experimental) the scp binary location on the target machine if not at /usr/bin/scp")
	vmBSCommand.PersistentFlags().DurationVar(&scpTimeout, "timeout", 60*time.Second,
		"(experimental) the timeout for copying certificates")
	vmBSCommand.PersistentFlags().StringVar(&spiffeTrustDomain, "spiffe-trust-domain", "",
		"(experimental) the SPIFFE trust domain for the generated certs")
	vmBSCommand.PersistentFlags().BoolVar(&sshIgnoreHostKeys, "ignore-host-keys", false,
		"(experimental) ignore host keys on the remote host")
	vmBSCommand.PersistentFlags().StringVarP(&sshKeyLocation, "ssh-key", "k", "",
		"(experimental) the location of the SSH key")
	vmBSCommand.PersistentFlags().IntVar(&sshPort, "ssh-port", 22,
		"(experimental) the port to SSH to the machine on")
	vmBSCommand.PersistentFlags().StringVarP(&sshUser, "ssh-user", "u", "",
		"(experimental) the user to SSH as, defaults to the current user")
	vmBSCommand.PersistentFlags().BoolVar(&startIstio, "start-istio-proxy", false,
		"start Istio proxy on a remote host after copying certs")

	return vmBSCommand
}
