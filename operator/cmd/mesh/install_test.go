package mesh

import (
	"bytes"
	"github.com/onsi/gomega"
	"testing"
)

func TestInstallEmptyRevision(t *testing.T) {
	g := gomega.NewWithT(t)
	args := []string{"install", "--revision", ""}
	rootCmd := GetRootCmd(args)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	err := rootCmd.Execute()
	g.Expect(err).To(gomega.HaveOccurred())
}