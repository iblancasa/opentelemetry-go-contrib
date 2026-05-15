// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ecs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	metadata "github.com/brunoscheufler/aws-ecs-metadata-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Create interface for functions that need to be mocked.
type MockDetectorUtils struct {
	mock.Mock
}

func (detectorUtils *MockDetectorUtils) getContainerID() (string, error) {
	args := detectorUtils.Called()
	return args.String(0), args.Error(1)
}

func (detectorUtils *MockDetectorUtils) getContainerName() (string, error) {
	args := detectorUtils.Called()
	return args.String(0), args.Error(1)
}

func (detectorUtils *MockDetectorUtils) getContainerMetadataV4(context.Context) (*metadata.ContainerMetadataV4, error) {
	args := detectorUtils.Called()
	return args.Get(0).(*metadata.ContainerMetadataV4), args.Error(1)
}

func (detectorUtils *MockDetectorUtils) getTaskMetadataV4(context.Context) (*metadata.TaskMetadataV4, error) {
	args := detectorUtils.Called()
	return args.Get(0).(*metadata.TaskMetadataV4), args.Error(1)
}

func TestNewResourceDetector(t *testing.T) {
	oldNewDetectorUtils := newDetectorUtils
	t.Cleanup(func() {
		newDetectorUtils = oldNewDetectorUtils
	})

	utils := new(MockDetectorUtils)
	newDetectorUtils = func() detectorUtils { return utils }

	detector := NewResourceDetector()
	assert.Same(t, utils, detector.(*resourceDetector).utils)
}

// successfully returns resource when process is running on Amazon ECS environment
// with no Metadata v4.
func TestDetectV3(t *testing.T) {
	t.Setenv(metadataV3EnvVar, "3")

	detectorUtils := new(MockDetectorUtils)

	detectorUtils.On("getContainerName").Return("container-Name", nil)
	detectorUtils.On("getContainerID").Return("0123456789A", nil)
	detectorUtils.On("getContainerMetadataV4").Return(nil, fmt.Errorf("not supported"))
	detectorUtils.On("getTaskMetadataV4").Return(nil, fmt.Errorf("not supported"))

	attributes := []attribute.KeyValue{
		semconv.CloudProviderAWS,
		semconv.CloudPlatformAWSECS,
		semconv.ContainerName("container-Name"),
		semconv.ContainerID("0123456789A"),
	}
	expectedResource := resource.NewWithAttributes(semconv.SchemaURL, attributes...)
	detector := &resourceDetector{utils: detectorUtils}
	res, _ := detector.Detect(t.Context())

	assert.Equal(t, expectedResource, res, "Resource returned is incorrect")
}

// successfully returns resource when process is running on Amazon ECS environment
// with Metadata v4.
func TestDetectV4(t *testing.T) {
	t.Setenv(metadataV4EnvVar, "4")

	detectorUtils := new(MockDetectorUtils)

	detectorUtils.On("getContainerName").Return("container-Name", nil)
	detectorUtils.On("getContainerID").Return("0123456789A", nil)
	detectorUtils.On("getContainerMetadataV4").Return(&metadata.ContainerMetadataV4{
		ContainerARN: "arn:aws:ecs:us-west-2:111122223333:container/05966557-f16c-49cb-9352-24b3a0dcd0e1",
	}, nil)
	detectorUtils.On("getTaskMetadataV4").Return(&metadata.TaskMetadataV4{
		Cluster:       "arn:aws:ecs:us-west-2:111122223333:cluster/default",
		TaskARN:       "arn:aws:ecs:us-west-2:111122223333:task/default/e9028f8d5d8e4f258373e7b93ce9a3c3",
		Family:        "curltest",
		Revision:      "3",
		DesiredStatus: "RUNNING",
		KnownStatus:   "RUNNING",
		Limits: metadata.Limits{
			CPU:    0.25,
			Memory: 512,
		},
		AvailabilityZone: "us-west-2a",
		LaunchType:       "FARGATE",
	}, nil)

	attributes := []attribute.KeyValue{
		semconv.CloudProviderAWS,
		semconv.CloudPlatformAWSECS,
		semconv.CloudAccountID("111122223333"),
		semconv.CloudRegion("us-west-2"),
		semconv.CloudAvailabilityZone("us-west-2a"),
		semconv.CloudResourceID("arn:aws:ecs:us-west-2:111122223333:container/05966557-f16c-49cb-9352-24b3a0dcd0e1"),
		semconv.ContainerName("container-Name"),
		semconv.ContainerID("0123456789A"),
		semconv.AWSECSClusterARN("arn:aws:ecs:us-west-2:111122223333:cluster/default"),
		semconv.AWSECSTaskARN("arn:aws:ecs:us-west-2:111122223333:task/default/e9028f8d5d8e4f258373e7b93ce9a3c3"),
		semconv.AWSECSLaunchtypeKey.String("fargate"),
		semconv.AWSECSTaskFamily("curltest"),
		semconv.AWSECSTaskRevision("3"),
		semconv.AWSECSContainerARN("arn:aws:ecs:us-west-2:111122223333:container/05966557-f16c-49cb-9352-24b3a0dcd0e1"),
	}
	expectedResource := resource.NewWithAttributes(semconv.SchemaURL, attributes...)
	detector := &resourceDetector{utils: detectorUtils}
	res, _ := detector.Detect(t.Context())

	assert.Equal(t, expectedResource, res, "Resource returned is incorrect")
}

// returns empty resource when detector receives a bad task ARN from the Metadata v4 endpoint.
func TestDetectBadARNsv4(t *testing.T) {
	t.Setenv(metadataV4EnvVar, "4")

	detectorUtils := new(MockDetectorUtils)

	detectorUtils.On("getContainerName").Return("container-Name", nil)
	detectorUtils.On("getContainerID").Return("0123456789A", nil)
	detectorUtils.On("getContainerMetadataV4").Return(&metadata.ContainerMetadataV4{
		ContainerARN: "container/05966557-f16c-49cb-9352-24b3a0dcd0e1",
	}, nil)
	detectorUtils.On("getTaskMetadataV4").Return(&metadata.TaskMetadataV4{
		Cluster:       "default",
		TaskARN:       "default/e9028f8d5d8e4f258373e7b93ce9a3c3",
		Family:        "curltest",
		Revision:      "3",
		DesiredStatus: "RUNNING",
		KnownStatus:   "RUNNING",
		Limits: metadata.Limits{
			CPU:    0.25,
			Memory: 512,
		},
		AvailabilityZone: "us-west-2a",
		LaunchType:       "FARGATE",
	}, nil)

	detector := &resourceDetector{utils: detectorUtils}
	_, err := detector.Detect(t.Context())

	assert.Equal(t, errCannotParseTaskArn, err)
}

// returns empty resource when detector cannot read container ID.
func TestDetectCannotReadContainerID(t *testing.T) {
	t.Setenv(metadataV3EnvVar, "3")
	detectorUtils := new(MockDetectorUtils)

	detectorUtils.On("getContainerName").Return("container-Name", nil)
	detectorUtils.On("getContainerID").Return("", nil)
	detectorUtils.On("getContainerMetadataV4").Return(nil, fmt.Errorf("not supported"))
	detectorUtils.On("getTaskMetadataV4").Return(nil, fmt.Errorf("not supported"))

	attributes := []attribute.KeyValue{
		semconv.CloudProviderAWS,
		semconv.CloudPlatformAWSECS,
		semconv.ContainerName("container-Name"),
		semconv.ContainerID(""),
	}
	expectedResource := resource.NewWithAttributes(semconv.SchemaURL, attributes...)
	detector := &resourceDetector{utils: detectorUtils}
	res, err := detector.Detect(t.Context())

	assert.NoError(t, err)
	assert.Equal(t, expectedResource, res, "Resource returned is incorrect")
}

// returns empty resource when detector cannot read container Name.
func TestDetectCannotReadContainerName(t *testing.T) {
	t.Setenv(metadataV3EnvVar, "3")
	detectorUtils := new(MockDetectorUtils)

	detectorUtils.On("getContainerName").Return("", errCannotReadContainerName)
	detectorUtils.On("getContainerID").Return("0123456789A", nil)
	detectorUtils.On("getContainerMetadataV4").Return(nil, fmt.Errorf("not supported"))
	detectorUtils.On("getTaskMetadataV4").Return(nil, fmt.Errorf("not supported"))

	detector := &resourceDetector{utils: detectorUtils}
	res, err := detector.Detect(t.Context())

	assert.Equal(t, errCannotReadContainerName, err)
	assert.Empty(t, res.Attributes())
}

// returns empty resource when process is not running ECS.
func TestReturnsIfNoEnvVars(t *testing.T) {
	detector := &resourceDetector{utils: nil}
	res, err := detector.Detect(t.Context())

	// When not on ECS, the detector should return nil and not error.
	assert.NoError(t, err, "failure to detect when not on platform must not be an error")
	assert.Nil(t, res, "failure to detect should return a nil Resource to optimize merge")
}

// handles alternative aws partitions (e.g. AWS GovCloud).
func TestLogsAttributesAlternatePartition(t *testing.T) {
	detector := &resourceDetector{utils: nil}

	containerMetadata := &metadata.ContainerMetadataV4{
		LogDriver: "awslogs",
		LogOptions: struct {
			AwsLogsCreateGroup string `json:"awslogs-create-group"`
			AwsLogsGroup       string `json:"awslogs-group"`
			AwsLogsStream      string `json:"awslogs-stream"`
			AwsRegion          string `json:"awslogs-region"`
		}{
			"fake-create",
			"fake-group",
			"fake-stream",
			"",
		},
		ContainerARN: "arn:arn-partition:arn-svc:arn-region:arn-account:arn-resource",
	}
	actualAttributes, err := detector.getLogsAttributes(containerMetadata)
	assert.NoError(t, err, "failure with nonstandard partition")

	expectedAttributes := []attribute.KeyValue{
		semconv.AWSLogGroupNames(containerMetadata.LogOptions.AwsLogsGroup),
		semconv.AWSLogGroupARNs("arn:arn-partition:logs:arn-region:arn-account:log-group:fake-group:*"),
		semconv.AWSLogStreamNames(containerMetadata.LogOptions.AwsLogsStream),
		semconv.AWSLogStreamARNs("arn:arn-partition:logs:arn-region:arn-account:log-group:fake-group:log-stream:fake-stream"),
	}
	assert.Equal(t, expectedAttributes, actualAttributes, "logs attributes are incorrect")
}

func TestCgroupContainerID(t *testing.T) {
	cgroups := []struct {
		cgroupPath      string
		wantContainerID string
	}{
		{
			"10:memory:/ecs/my-task-name/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		{
			"10:memory:/ecs/api_service_1/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			"10:memory:/ecs/my-task-name/12345abc",
			"",
		},
		{
			"10:memory:/docker/my-task-name/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			"",
		},
	}

	for _, c := range cgroups {
		t.Run(c.cgroupPath, func(t *testing.T) {
			containerID := getCgroupContainerID([]byte(c.cgroupPath))
			assert.Equal(t, c.wantContainerID, containerID)
		})
	}
}

func TestECSDetectorUtilsInjection(t *testing.T) {
	t.Run("getContainerMetadataV4 uses injectable function", func(t *testing.T) {
		oldGetContainerMetadataV4 := getContainerMetadataV4
		t.Cleanup(func() {
			getContainerMetadataV4 = oldGetContainerMetadataV4
		})

		expected := &metadata.ContainerMetadataV4{ContainerARN: "arn:aws:ecs:us-west-2:111122223333:container/test"}
		getContainerMetadataV4 = func(_ context.Context, client *http.Client) (*metadata.ContainerMetadataV4, error) {
			assert.NotNil(t, client)
			return expected, nil
		}

		got, err := (ecsDetectorUtils{}).getContainerMetadataV4(t.Context())
		require.NoError(t, err)
		assert.Same(t, expected, got)
	})

	t.Run("getTaskMetadataV4 uses injectable function", func(t *testing.T) {
		oldGetTaskMetadataV4 := getTaskMetadataV4
		t.Cleanup(func() {
			getTaskMetadataV4 = oldGetTaskMetadataV4
		})

		expected := &metadata.TaskMetadataV4{TaskARN: "arn:aws:ecs:us-west-2:111122223333:task/default/taskid"}
		getTaskMetadataV4 = func(_ context.Context, client *http.Client) (*metadata.TaskMetadataV4, error) {
			assert.NotNil(t, client)
			return expected, nil
		}

		got, err := (ecsDetectorUtils{}).getTaskMetadataV4(t.Context())
		require.NoError(t, err)
		assert.Same(t, expected, got)
	})

	t.Run("getContainerID uses injected readFile on linux", func(t *testing.T) {
		utils := ecsDetectorUtils{
			readFile: func(string) ([]byte, error) {
				return []byte("10:memory:/ecs/my-task-name/1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"), nil
			},
			goos: "linux",
		}

		got, err := utils.getContainerID()
		require.NoError(t, err)
		assert.Equal(t, "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", got)
	})

	t.Run("getContainerID returns empty on non-linux", func(t *testing.T) {
		utils := ecsDetectorUtils{
			goos: "darwin",
		}

		got, err := utils.getContainerID()
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("getContainerName uses injected hostname", func(t *testing.T) {
		utils := ecsDetectorUtils{
			hostname: func() (string, error) {
				return "test-host", nil
			},
		}

		got, err := utils.getContainerName()
		require.NoError(t, err)
		assert.Equal(t, "test-host", got)
	})

	t.Run("getContainerName maps hostname error", func(t *testing.T) {
		utils := ecsDetectorUtils{
			hostname: func() (string, error) {
				return "", errors.New("boom")
			},
		}

		_, err := utils.getContainerName()
		assert.ErrorIs(t, err, errCannotReadContainerName)
	})
}
