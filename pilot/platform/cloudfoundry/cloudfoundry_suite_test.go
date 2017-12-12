package cloudfoundry_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCloudFoundry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cloud Foundry Suite")
}
