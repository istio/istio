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

package envoy_test

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/ptypes"
	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/envoy"
	testEnvoy "istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/util/reserveport"
	"istio.io/istio/pkg/test/util/tmpl"
)

var (
	envoyLogFormat = envoy.LogFormat("[ENVOY][%Y-%m-%d %T.%e][%t][%l][%n]")
)

func TestNewWithoutConfigShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := envoy.New(envoy.Config{})
	g.Expect(err).ToNot(BeNil())
}

func TestNewWithDuplicateOptionsShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	_, err := envoy.New(envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options: options(
			envoy.ConfigPath(h.BootstrapFile()),
			envoy.ConfigPath(h.BootstrapFile())),
	})
	g.Expect(err).ToNot(BeNil())
}

func TestNewWithInvalidOptionShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := envoy.New(envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath("file/does/not/exist")),
	})
	g.Expect(err).ToNot(BeNil())
}

func TestNewWithoutAdminPortShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := envoy.New(envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigYaml("{}")),
	})
	g.Expect(err).ToNot(BeNil())
}

func TestNewWithoutBinaryPathShouldFail(t *testing.T) {
	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	_, err := envoy.New(envoy.Config{
		Options: options(envoy.ConfigPath(h.BootstrapFile())),
	})
	g.Expect(err).ToNot(BeNil())
}

func TestNewWithConfigPathShouldSucceed(t *testing.T) {
	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})
	g.Expect(i.Config().Name).To(Equal("envoy"))
	g.Expect(i.Config().BinaryPath).ToNot(Equal(""))
}

func TestNewWithConfigYamlShouldSucceed(t *testing.T) {
	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigYaml(h.BootstrapContent(t))),
	})
	g.Expect(i.Config().Name).To(Equal("envoy"))
	g.Expect(i.Config().BinaryPath).ToNot(Equal(""))
}

func TestStartWithBadBinaryShouldFail(t *testing.T) {
	runLinuxOnly(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: absPath("testdata/envoy_bootstrap.json"), // Not a binary file.
		Options:    options(envoy.ConfigYaml(h.BootstrapContent(t))),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitForError(t, i)
}

func TestStartEnvoyShouldSucceed(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitLive(t, i)

	// Wait for a bit and verify that Envoy hasn't terminated.
	err := i.Wait().WithTimeout(time.Second * 1).Do()
	g.Expect(err).To(Equal(context.DeadlineExceeded))
}

func TestStartTwiceShouldDoNothing(t *testing.T) {
	runLinuxOnly(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	i.Start(ctx)
	waitLive(t, i)
}

func TestHotRestartTwiceShouldFail(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)

	_, err := i.NewInstanceForHotRestart()
	g.Expect(err).To(BeNil())

	_, err = i.NewInstanceForHotRestart()
	g.Expect(err).ToNot(BeNil())
}

func TestHotRestart(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options: options(
			envoy.ConfigPath(h.BootstrapFile()),
			// Setting parameters to force shutdown of the first Envoy quickly.
			envoy.DrainDuration(1*time.Second),
			envoy.ParentShutdownDuration(1*time.Second)),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	restartInstance, err := i.NewInstanceForHotRestart()
	g.Expect(err).To(BeNil())
	g.Expect(restartInstance.Epoch()).To(Equal(envoy.Epoch(1)))

	// Confirm that the first instance is live.
	info, err := i.GetServerInfo()
	g.Expect(err).To(BeNil())
	g.Expect(info.State).To(Equal(envoyAdmin.ServerInfo_LIVE))

	// Start the restart instance.
	restartInstance.Start(ctx)

	// Wait for the first instance to exit
	if err := i.Wait().WithTimeout(5 * time.Second).Do(); err != nil {
		t.Fatal(err)
	}

	waitLive(t, restartInstance)
}

func TestCommandLineArgs(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	logPath := filepath.Join(h.tempDir, "envoyLogPath.txt")
	drainDuration := time.Second * 30
	parentShutdownDuration := time.Second * 60
	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options: options(
			envoy.ConfigPath(h.BootstrapFile()),
			envoy.ConfigYaml("{}"),
			envoy.LogLevelDebug,
			envoy.LogPath(logPath),
			envoy.ComponentLogLevels{
				envoy.ComponentLogLevel{
					Name:  "upstream",
					Level: envoy.LogLevelWarning,
				},
			},
			envoy.LocalAddressIPVersion(envoy.IPV4),
			envoy.Concurrency(7),
			envoy.DisableHotRestart(true),
			envoy.Epoch(1),
			envoy.ServiceCluster("mycluster"),
			envoy.ServiceNode("mynode"),
			envoy.DrainDuration(drainDuration),
			envoy.ParentShutdownDuration(parentShutdownDuration),
		),
	})
	g.Expect(i.Epoch()).To(Equal(envoy.Epoch(1)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitLive(t, i)

	info, err := i.GetServerInfo()
	g.Expect(err).To(BeNil())

	// Just verify that options we specified are reflected in the binary. We don't
	// do an exhaustive test of the various options here, since that is done in the options tests.
	opts := info.CommandLineOptions
	g.Expect(opts.ConfigPath).To(Equal(h.BootstrapFile()))
	g.Expect(opts.ConfigYaml).To(Equal("{}"))
	g.Expect(opts.LogLevel).To(Equal(envoy.LogLevelDebug.FlagValue()))
	g.Expect(opts.LogFormat).To(Equal(envoyLogFormat.FlagValue()))
	g.Expect(opts.LogPath).To(Equal(logPath))
	g.Expect(opts.ComponentLogLevel).To(Equal("upstream:warning"))
	g.Expect(opts.LocalAddressIpVersion).To(Equal(envoyAdmin.CommandLineOptions_v4))
	g.Expect(opts.BaseId).To(Equal(h.baseID.GetInternalEnvoyValue()))
	g.Expect(opts.Concurrency).To(Equal(uint32(7)))
	g.Expect(opts.DisableHotRestart).To(BeTrue())
	g.Expect(opts.RestartEpoch).To(Equal(uint32(1)))
	g.Expect(opts.ServiceCluster).To(Equal("mycluster"))
	g.Expect(opts.ServiceNode).To(Equal("mynode"))
	g.Expect(opts.DrainTime.AsDuration()).To(Equal(drainDuration))
	g.Expect(opts.ParentShutdownTime.AsDuration()).To(Equal(parentShutdownDuration))
}

func TestShutdown(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitLive(t, i)

	// Initiate the shutdown.
	g.Expect(i.Shutdown()).To(BeNil())

	// Wait for the shutdown to complete.
	wait(t, i)
}

func TestKillEnvoy(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitLive(t, i)

	// Kill the process.
	g.Expect(i.Kill()).To(BeNil())

	// Expect the process to return an error.
	waitForError(t, i)
}

func TestCancelContext(t *testing.T) {
	runLinuxOnly(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitLive(t, i)

	// Cancel the context.
	cancel()

	// Expect the process to return an error.
	waitForError(t, i)
}

func TestConfigDump(t *testing.T) {
	runLinuxOnly(t)

	g := NewGomegaWithT(t)

	h := newBootstrapHelper(t)
	defer h.Close()

	i := h.NewOrFail(t, envoy.Config{
		BinaryPath: testEnvoy.FindBinaryOrFail(t),
		Options:    options(envoy.ConfigPath(h.BootstrapFile())),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	i.Start(ctx)
	waitLive(t, i)

	cd, err := i.GetConfigDump()
	g.Expect(err).To(BeNil())

	// Basic verification of the config dump..
	for _, c := range cd.Configs {
		if c.TypeUrl == "type.googleapis.com/envoy.admin.v3.BootstrapConfigDump" {
			b := envoyAdmin.BootstrapConfigDump{}
			g.Expect(ptypes.UnmarshalAny(c, &b)).To(BeNil())

			g.Expect(b.Bootstrap.Admin.GetAddress().GetSocketAddress().GetPortValue()).To(Equal(i.AdminPort()))
			return
		}
	}
	t.Fatal("failed validating envoy config dump")
}

type bootstrapHelper struct {
	tempDir       string
	portMgr       reserveport.PortManager
	bootstrapFile string
	baseID        envoy.BaseID
}

func newBootstrapHelper(t *testing.T) *bootstrapHelper {
	t.Helper()
	tempDir := newTempDir(t)
	portMgr := reserveport.NewPortManagerOrFail(t)
	adminPort := portMgr.ReservePortNumberOrFail(t)
	listenerPort := portMgr.ReservePortNumberOrFail(t)
	bootstrapFile := newBootstrapFile(t, tempDir, adminPort, listenerPort)
	return &bootstrapHelper{
		tempDir:       tempDir,
		portMgr:       portMgr,
		bootstrapFile: bootstrapFile,
		baseID:        envoy.GenerateBaseID(),
	}
}

func (h *bootstrapHelper) BootstrapFile() string {
	return h.bootstrapFile
}

func (h *bootstrapHelper) BootstrapContent(t *testing.T) string {
	t.Helper()
	content, err := ioutil.ReadFile(h.bootstrapFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func (h *bootstrapHelper) Close() {
	_ = os.RemoveAll(h.tempDir)
	h.portMgr.CloseSilently()
}

func (h *bootstrapHelper) New(cfg envoy.Config) (envoy.Instance, error) {
	cfg.Options = append(cfg.Options, h.baseID)
	return envoy.New(cfg)
}

func (h *bootstrapHelper) NewOrFail(t *testing.T, cfg envoy.Config) envoy.Instance {
	t.Helper()
	i, err := h.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func absPath(path string) string {
	path, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return path
}

func newTempDir(t *testing.T) string {
	t.Helper()
	dir, err := ioutil.TempDir("", t.Name())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func newBootstrapFile(t *testing.T, tempDir string, adminPort, listenerPort uint16) string {
	t.Helper()
	data := map[string]interface{}{
		"nodeID":       t.Name(),
		"cluster":      t.Name(),
		"localhost":    "127.0.0.1",
		"wildcard":     "0.0.0.0",
		"adminPort":    adminPort,
		"listenerPort": listenerPort,
	}

	// Read the template file.
	tmplContent, err := ioutil.ReadFile("testdata/envoy_bootstrap_v2.tmpl.json")
	if err != nil {
		t.Fatal(err)
	}

	// Parse the template file.
	tpl := tmpl.ParseOrFail(t, string(tmplContent))

	// Execute the template with the given data.
	content := tmpl.ExecuteOrFail(t, tpl, data)

	// Write out the result to the output file.
	outputFile := filepath.Join(tempDir, t.Name()+".json")
	if err := ioutil.WriteFile(outputFile, []byte(content), os.ModePerm); err != nil {
		t.Fatal(err)
	}
	return outputFile
}

func wait(t *testing.T, i envoy.Instance) {
	t.Helper()
	if err := i.Wait().WithTimeout(2 * time.Second).Do(); err != nil {
		t.Fatal(err)
	}
}

func waitForError(t *testing.T, i envoy.Instance) {
	t.Helper()
	if err := i.Wait().WithTimeout(2 * time.Second).Do(); err == nil {
		t.Fatal(errors.New("expected error"))
	}
}

func waitLive(t *testing.T, i envoy.Instance) {
	t.Helper()
	if err := i.WaitLive().WithTimeout(10 * time.Second).Do(); err != nil {
		t.Fatal(err)
	}
}

func options(opts ...envoy.Option) []envoy.Option {
	return append([]envoy.Option{envoyLogFormat}, opts...)
}

func runLinuxOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("test requires GOOS=linux")
	}
}
