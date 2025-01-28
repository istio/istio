package config_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/config"
)

// Define a sample Spec struct and its proto representation
type TestSpec struct {
	Key string
}

func (t *TestSpec) Reset()        { *t = TestSpec{} }
func (t *TestSpec) ProtoMessage() {}

func TestPilotConfigToResource(t *testing.T) {
	spec := &TestSpec{Key: "test-value"}
	creationTime := time.Now()

	testConfig := &config.Config{
		Meta: config.Meta{
			Name:              "test-name",
			Namespace:         "test-namespace",
			CreationTimestamp: creationTime,
			ResourceVersion:   "1",
			Labels: map[string]string{
				"app": "test-app",
			},
			Annotations: map[string]string{
				"annotation-key": "annotation-value",
			},
		},
		Spec: spec,
	}

	resource, err := config.PilotConfigToResource(testConfig)
	require.NoError(t, err, "Expected no error from PilotConfigToResource")

	assert.NotNil(t, resource, "Expected non-nil resource")
	assert.Equal(t, "test-namespace/test-name", resource.Metadata.Name, "Metadata.Name should match")
	assert.Equal(t, timestamppb.New(creationTime), resource.Metadata.CreateTime, "Metadata.CreateTime should match")
	assert.Equal(t, "1", resource.Metadata.Version, "Metadata.Version should match")
	assert.Equal(t, testConfig.Labels, resource.Metadata.Labels, "Metadata.Labels should match")
	assert.Equal(t, testConfig.Annotations, resource.Metadata.Annotations, "Metadata.Annotations should match")

	assert.NotNil(t, resource.Body, "Resource Body should not be nil")
}
