package integration

import (
	"fmt"
	"os"
	"testing"
)

const InstanceNamePrefix = "SdkIntegrationTestInstance"

type Config struct {
	AccessKeyId     string
	AccessKeySecret string
	PublicKeyId     string
	PrivateKey      string
	RoleArn         string
	ChildAK         string
	ChildSecret     string
}

func getConfigFromEnv() *Config {
	config := &Config{
		AccessKeyId:     os.Getenv("ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("ACCESS_KEY_SECRET"),
		PublicKeyId:     os.Getenv("PUBLIC_KEY_ID"),
		PrivateKey:      os.Getenv("PRIVATE_KEY"),
		RoleArn:         os.Getenv("ROLE_ARN"),
		ChildAK:         os.Getenv("CHILD_AK"),
		ChildSecret:     os.Getenv("CHILD_SECRET"),
	}
	if config.AccessKeyId == "" {
		panic("Get ACCESS_KEY_ID from environment variables failed")
	} else {
		return config
	}
}

func getEcsDemoInstanceId() string {
	return os.Getenv("DEMO_ECS_INSTANCE_ID")
}

func assertErrorNil(t *testing.T, err error, message string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, message+": %v\n", err)
	}
}
