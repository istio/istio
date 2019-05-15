package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/onsi/gomega"
)

type OutputCollector struct {
	Commands string
}

func (o *OutputCollector) Run(command Command) error {
	o.Commands = o.Commands + "\n" + command.Command + " " + strings.Join(command.Args, ",")
	return nil
}

func TestDotEnvLoad(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	envFile := "/tmp/test.env"
	err := ioutil.WriteFile(envFile, []byte("THIS_IS_A_CONFIG_VARIABLE=test\n"), 0644)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	dotEnvLoad("TEST_CONFIG_FILE", envFile)
	g.Expect(os.Getenv("THIS_IS_A_CONFIG_VARIABLE")).To(gomega.Equal("test"))

}

func TestDefaultCommandRunner(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	err := defaultCommandRunner(Command{Command: "echo", Args: []string{"Hello", "World"}})
	g.Expect(err).NotTo(gomega.HaveOccurred())
}

func TestGetLocalIP(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ip, err := getLocalIP()
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(ip).NotTo(gomega.BeNil())

}

func TestRunOrFail(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	commandRunner = func(command Command) error {
		return fmt.Errorf("test")
	}
	var err interface{}
	{
		var recoverFromPanic = func() {
			if e := recover(); e != nil {
				err = e
			}
		}
		defer recoverFromPanic()

		iptables().RunOrFail()
	}
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestSeparateV4V6(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	v4range, v6range, err := separateV4V6("*")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(v4range.IsWildcard).To(gomega.BeTrue())
	g.Expect(v6range.IsWildcard).To(gomega.BeTrue())

	// For bug compatibility with istio-iptables.sh
	// TODO: fix this in caller (* should not be expanded to "bin boot dev home ...") and then
	// fix this tests as well to expect panic when -i argument is invalid
	// _, _, err = separateV4V6("bin")
	// g.Expect(err).To(gomega.HaveOccurred())
	v4range, v6range, err = separateV4V6("bin")
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(v4range).To(gomega.BeEquivalentTo(NetworkRange{IsWildcard: false, IPNets: make([]*net.IPNet, 0)}))
	g.Expect(v6range).To(gomega.BeEquivalentTo(NetworkRange{IsWildcard: false, IPNets: make([]*net.IPNet, 0)}))
}

func TestGetEnvWithDefault(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	g.Expect(getEnvWithDefault("TEST_DEFAULT_VALUE", "default-value")).To(gomega.Equal("default-value"))
	os.Setenv("TEST_DEFAULT_VALUE", "1234")
	g.Expect(getEnvWithDefault("TEST_DEFAULT_VALUE", "default-value")).To(gomega.Equal("1234"))
}

func TestCompatibilityWithoutArgs(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run([]string{},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("127.0.0.1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,15001
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,15001
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,1337,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,0,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,1337,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,0,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
iptables-save 
ip6tables-save `))
}

func TestCompatibilityWithTPROXYMode(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run(
		[]string{"-p", "12345", "-u", "4321", "-g", "4444", "-m", "TPROXY", "-b", "5555,6666",
			"-d", "7777,8888", "-i", "1.1.0.0/16", "-x", "9.9.0.0/16", "-k", "eth1,eth2"},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("127.0.0.1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,mangle,-N,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_DIVERT,-j,MARK,--set-mark,1337
iptables -t,mangle,-A,ISTIO_DIVERT,-j,ACCEPT
ip -f,inet,rule,add,fwmark,1337,lookup,133
ip -f,inet,route,add,local,default,dev,lo,table,133
iptables -t,mangle,-N,ISTIO_TPROXY
iptables -t,mangle,-A,ISTIO_TPROXY,!,-d,127.0.0.1/32,-p,tcp,-j,TPROXY,--tproxy-mark,1337/0xffffffff,--on-port,12345
iptables -t,mangle,-N,ISTIO_INBOUND
iptables -t,mangle,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-j,ISTIO_TPROXY
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-j,ISTIO_TPROXY
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,9.9.0.0/16,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
iptables-save 
ip6tables-save `))
}

func TestCompatibilityWithTPROXYModeAndWildcardPort(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run(
		[]string{"-p", "12345", "-u", "4321", "-g", "4444", "-m", "TPROXY", "-b", "*", "-d", "7777,8888", "-i", "1.1.0.0/16", "-x", "9.9.0.0/16", "-k", "eth1,eth2"},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("127.0.0.1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,mangle,-N,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_DIVERT,-j,MARK,--set-mark,1337
iptables -t,mangle,-A,ISTIO_DIVERT,-j,ACCEPT
ip -f,inet,rule,add,fwmark,1337,lookup,133
ip -f,inet,route,add,local,default,dev,lo,table,133
iptables -t,mangle,-N,ISTIO_TPROXY
iptables -t,mangle,-A,ISTIO_TPROXY,!,-d,127.0.0.1/32,-p,tcp,-j,TPROXY,--tproxy-mark,1337/0xffffffff,--on-port,12345
iptables -t,mangle,-N,ISTIO_INBOUND
iptables -t,mangle,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,22,-j,RETURN
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,7777,-j,RETURN
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,8888,-j,RETURN
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,-j,ISTIO_TPROXY
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,9.9.0.0/16,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
iptables-save 
ip6tables-save `))
}

func TestCompatibilityWithREDIRECTMode(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run(
		[]string{"-p", "12345", "-u", "4321", "-g", "4444", "-m", "REDIRECT", "-b", "5555,6666",
			"-d", "7777,8888", "-i", "1.1.0.0/16", "-x", "9.9.0.0/16", "-k", "eth1,eth2"},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("127.0.0.1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_INBOUND
iptables -t,nat,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-j,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-j,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,9.9.0.0/16,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-d,1.1.0.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
iptables-save 
ip6tables-save `))
}

func TestClean(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run(
		[]string{"clean"},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("127.0.0.1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables-save 
ip6tables-save `))
}

func TestTProxyModeAndIpV6(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run(
		[]string{"-p", "12345", "-u", "4321", "-g", "4444", "-m", "TPROXY", "-b", "*",
			"-d", "7777,8888", "-i", "2001:db8:1::1/32", "-x", "2019:db8:1::1/32", "-k", "eth1,eth2"},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("2001:db8:1::1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,mangle,-N,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_DIVERT,-j,MARK,--set-mark,1337
iptables -t,mangle,-A,ISTIO_DIVERT,-j,ACCEPT
ip -f,inet,rule,add,fwmark,1337,lookup,133
ip -f,inet,route,add,local,default,dev,lo,table,133
iptables -t,mangle,-N,ISTIO_TPROXY
iptables -t,mangle,-A,ISTIO_TPROXY,!,-d,127.0.0.1/32,-p,tcp,-j,TPROXY,--tproxy-mark,1337/0xffffffff,--on-port,12345
iptables -t,mangle,-N,ISTIO_INBOUND
iptables -t,mangle,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,22,-j,RETURN
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,7777,-j,RETURN
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,8888,-j,RETURN
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,-j,ISTIO_TPROXY
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
ip6tables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
ip6tables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
ip6tables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
ip6tables -t,nat,-F,ISTIO_OUTPUT
ip6tables -t,nat,-X,ISTIO_OUTPUT
ip6tables -t,nat,-F,ISTIO_INBOUND
ip6tables -t,nat,-X,ISTIO_INBOUND
ip6tables -t,mangle,-F,ISTIO_INBOUND
ip6tables -t,mangle,-X,ISTIO_INBOUND
ip6tables -t,mangle,-F,ISTIO_DIVERT
ip6tables -t,mangle,-X,ISTIO_DIVERT
ip6tables -t,mangle,-F,ISTIO_TPROXY
ip6tables -t,mangle,-X,ISTIO_TPROXY
ip6tables -t,nat,-F,ISTIO_REDIRECT
ip6tables -t,nat,-X,ISTIO_REDIRECT
ip6tables -t,nat,-F,ISTIO_IN_REDIRECT
ip6tables -t,nat,-X,ISTIO_IN_REDIRECT
ip6tables -t,nat,-N,ISTIO_REDIRECT
ip6tables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
ip6tables -t,nat,-N,ISTIO_IN_REDIRECT
ip6tables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
ip6tables -t,nat,-N,ISTIO_INBOUND
ip6tables -t,nat,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
ip6tables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,22,-j,RETURN
ip6tables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,7777,-j,RETURN
ip6tables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,8888,-j,RETURN
ip6tables -t,nat,-N,ISTIO_OUTPUT
ip6tables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
ip6tables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,::1/128,-j,ISTIO_REDIRECT
ip6tables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
ip6tables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
ip6tables -t,nat,-A,ISTIO_OUTPUT,-d,::1/128,-j,RETURN
ip6tables -t,nat,-A,ISTIO_OUTPUT,-d,2019:db8::/32,-j,RETURN
ip6tables -t,nat,-I,PREROUTING,1,-i,eth1,-d,2001:db8::/32,-j,ISTIO_REDIRECT
ip6tables -t,nat,-I,PREROUTING,1,-i,eth2,-d,2001:db8::/32,-j,ISTIO_REDIRECT
ip6tables -t,nat,-A,ISTIO_OUTPUT,-d,2001:db8::/32,-j,ISTIO_REDIRECT
ip6tables -t,nat,-A,ISTIO_OUTPUT,-j,RETURN
iptables-save 
ip6tables-save `))
}

func TestWildcardIncludeIPRange(t *testing.T) {
	output := OutputCollector{}
	commandRunner = output.Run
	g := gomega.NewGomegaWithT(t)
	run(
		[]string{"-p", "12345", "-u", "4321", "-g", "4444", "-m", "REDIRECT", "-b", "5555,6666",
			"-d", "7777,8888", "-i", "*", "-x", "9.9.0.0/16", "-k", "eth1,eth2"},
		flag.NewFlagSet("test", flag.ExitOnError),
		func() (net.IP, error) {
			return net.ParseIP("127.0.0.1"), nil
		})
	g.Expect(output.Commands).To(gomega.Equal(`
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_INBOUND
iptables -t,nat,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-j,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-j,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,9.9.0.0/16,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,ISTIO_REDIRECT
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
iptables-save 
ip6tables-save `))
}
