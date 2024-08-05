package platform

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
)

const (
	AliyunIdentifier = "Alibaba Cloud"

	NodeInstanceTypeEnvKey         = "NODE_INSTANCE_TYPE"
	NodeInstanceIdEnvKey           = "NODE_INSTANCE_ID"
	NodePrivateIpv4EnvKey          = "NODE_PRIVATE_IPV4"
	NodeZoneIdEnvKey               = "NODE_ZONE_ID"
	NodeRegionIdEnvKey             = "NODE_REGION_ID"
	MetadataConfigSourceEnvKey     = "METADATA_CONFIG_SOURCE"
	MetadataFileConfigSourceEnvKey = "METADATA_FILE_CONFIG_SOURCE"
	CPUInfoPathEnvKey              = "CPU_INFO_PATH"

	MaxRetryCount = 10
	cpuInfoPath   = "/proc/cpuinfo"
)

var AliyunEndpoint = "http://100.100.100.200"

// var AliyunDocumentUrl = AliyunEndpoint + "/latest/dynamic/instance-identity/document"

type aliyunEnv struct {
	instance aliyunInstance
}

type aliyunInstance struct {
	ZoneId       string `json:"zone-id"`
	InstanceId   string `json:"instance-id"`
	RegionId     string `json:"region-id"`
	PrivateIpv4  string `json:"private-ipv4"`
	Mac          string `json:"mac"`
	InstanceType string `json:"instance-type"`
	CPUInfo      string `json:"cpu-info"`
	retryCount   int
}

var (
	MetadataConfigSource = env.RegisterStringVar(
		MetadataConfigSourceEnvKey,
		"metaserver",
		"The metadata config source, support for extracting from metadata-server, files, and environment variables, configuration options include: metaserver, file, env",
	)

	MetadataFileConfigSourcePath = env.RegisterStringVar(
		MetadataFileConfigSourceEnvKey,
		"/etc/istio/pod/labels",
		"The path to the file where the meta information is stored",
	)

	NodeInstanceType = env.RegisterStringVar(
		NodeInstanceTypeEnvKey,
		"",
		"The machine specification of the node.",
	)

	NodeInstanceId = env.RegisterStringVar(
		NodeInstanceIdEnvKey,
		"",
		"The unique ID of the node.",
	)

	NodePrivateIpv4 = env.RegisterStringVar(
		NodePrivateIpv4EnvKey,
		"",
		"The IPv4 address of the node.",
	)

	NodeZoneId = env.RegisterStringVar(
		NodeZoneIdEnvKey,
		"",
		"The zone in which the node is located.",
	)

	NodeRegionId = env.RegisterStringVar(
		NodeRegionIdEnvKey,
		"",
		"The region in which the node is located.",
	)

	CPUInfoPath = env.RegisterStringVar(
		CPUInfoPathEnvKey,
		cpuInfoPath,
		"the path of cpu info",
	)

	NodeInstanceTypeRegexp = regexp.MustCompile(`topology.kone.ali/node-instance-type="(.*)"`)
	NodeInstanceIdRegexp   = regexp.MustCompile(`topology.kone.ali/node-instance-id="(.*)"`)
	NodePrivateIpv4Regexp  = regexp.MustCompile(`topology.kone.ali/node-private-ipv4="(.*)"`)
	NodeZoneIdRegexp       = regexp.MustCompile(`topology.kone.ali/zone="(.*)"`)
	NodeRegionIdRegexp     = regexp.MustCompile(`topology.kone.ali/region="(.*)"`)
)

// Check if sys_vendor contains alibaba cloud
func IsAliyun() bool {

	sysVendor, err := ioutil.ReadFile(SysVendorPath)
	if err != nil {
		log.Errorf("Error reading sys_vendor in Aliyun platform detection: %v", err)
	}
	return strings.Contains(string(sysVendor), AliyunIdentifier)
}

func ecsDocumentRequest() aliyunInstance {
	instance := aliyunInstance{}
	client := http.Client{Timeout: defaultTimeout}
	aliyunDocumentUrl := AliyunEndpoint + "/latest/dynamic/instance-identity/document"
	req, err := http.NewRequest("GET", aliyunDocumentUrl, nil)
	if err != nil {
		log.Warnf("Failed to create HTTP request: %v", err)
		return instance
	}

	response, err := client.Do(req)
	if err != nil {
		log.Warnf("HTTP request failed: %v", err)
		return instance
	}
	if response.StatusCode != http.StatusOK {
		log.Warnf("HTTP request unsuccessful with status: %v", response.Status)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&instance); err != nil {
		log.Warnf("failed to decode response: %v", err)
		return instance
	}
	return instance
}

func NewAliyun() Environment {
	if MetadataConfigSource.Get() == "file" {
		env := &aliyunEnv{
			instance: aliyunInstance{
				retryCount: 0,
			},
		}
		if err := env.readMetadataFromFileWithRetry(MetadataFileConfigSourcePath.Get()); err != nil {
			log.Errorf("%v", err)
			env.clean()
		}

		// fallback
		if env.instance.InstanceType == "" {
			env.instance.CPUInfo = readeCPUInfo(CPUInfoPath.Get())
		}

		return env
	} else if MetadataConfigSource.Get() == "env" {
		env := &aliyunEnv{
			instance: aliyunInstance{
				ZoneId:       NodeZoneId.Get(),
				RegionId:     NodeRegionId.Get(),
				InstanceId:   NodeInstanceId.Get(),
				PrivateIpv4:  NodePrivateIpv4.Get(),
				InstanceType: NodeInstanceType.Get(),
			},
		}

		// fallback
		if env.instance.InstanceType == "" {
			env.instance.CPUInfo = readeCPUInfo(CPUInfoPath.Get())
		}

		return env
	} else {
		return &aliyunEnv{
			instance: ecsDocumentRequest(),
		}
	}
}

func (e *aliyunEnv) Metadata() map[string]string {
	md := map[string]string{}
	md["zone"] = e.instance.ZoneId
	md["region"] = e.instance.RegionId
	md["instance-type"] = e.instance.InstanceType
	md["instance-id"] = e.instance.InstanceId
	md["private-ipv4"] = e.instance.PrivateIpv4
	md["provider"] = "alibaba-cloud"
	md["retry-count"] = strconv.Itoa(e.instance.retryCount)
	md["cpu-info"] = e.instance.CPUInfo
	return md
}

// Locality returns the region and zone
func (e *aliyunEnv) Locality() *core.Locality {
	var l core.Locality
	l.Region = e.instance.RegionId
	l.Zone = e.instance.ZoneId
	return &l
}

func (e *aliyunEnv) Labels() map[string]string {
	return map[string]string{}
}

func (e *aliyunEnv) IsKubernetes() bool {
	return true
}

func (e *aliyunEnv) clean() {
	e.instance.InstanceId = ""
	e.instance.RegionId = ""
	e.instance.InstanceType = ""
	e.instance.ZoneId = ""
	e.instance.PrivateIpv4 = ""
}

func (e *aliyunEnv) readMetadataFromFileWithRetry(filepath string) error {
	e.instance.retryCount = 0
	var err error
	for {
		log.Infof("read metadata: %s", filepath)
		if err = e.readMetadataFromFile(filepath); err == nil {
			log.Infof("sccessfully read metadata from file, path: %s", filepath)
			return nil
		}

		if e.instance.retryCount >= MaxRetryCount {
			return err
		}

		select {
		case <-time.After(500 * time.Millisecond):
			e.instance.retryCount += 1
		}
		log.Infof("retry count: %d", e.instance.retryCount)
	}
}

func (e *aliyunEnv) readMetadataFromFile(filepath string) error {
	var err error
	bytes, err := ioutil.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("failed to read file: %s, error: %v", filepath, err)
	}

	content := string(bytes)
	log.Infof("content: %s", content)

	var value string
	value, err = readValue(NodeInstanceTypeRegexp, content)
	if err != nil {
		return err
	}
	e.instance.InstanceType = value

	value, err = readValue(NodeInstanceIdRegexp, content)
	if err != nil {
		return err
	}
	e.instance.InstanceId = value

	value, err = readValue(NodePrivateIpv4Regexp, content)
	if err != nil {
		return err
	}
	e.instance.PrivateIpv4 = value

	value, err = readValue(NodeZoneIdRegexp, content)
	if err != nil {
		return err
	}
	e.instance.ZoneId = value

	value, err = readValue(NodeRegionIdRegexp, content)
	if err != nil {
		return err
	}
	e.instance.RegionId = value

	return nil
}

func readValue(reg *regexp.Regexp, originalContent string) (string, error) {
	match := reg.FindStringSubmatch(originalContent)
	if match == nil {
		return "", fmt.Errorf("no matched %s, original content %s", reg.String(), originalContent)
	}

	if len(match[1]) == 0 {
		return "", fmt.Errorf("the matched value is empty, regexp: %s, original content %s", reg.String(), originalContent)
	}
	log.Infof("%s", string(match[1]))
	return string(match[1]), nil
}

func readeCPUInfo(filePath string) string {
	file, err := os.Open(filePath)
	if err != nil {
		log.Errorf("open cpuinfo fail, err: %v", err)
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			} else {
				log.Error("cpu info format is invalid")
				return ""
			}
		}
	}

	if err := scanner.Err(); err != nil {
		log.Errorf("read file fail, err: %v", err)
	}
	return ""
}
