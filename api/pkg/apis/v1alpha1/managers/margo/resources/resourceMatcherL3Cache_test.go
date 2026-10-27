package resources

import (
	"encoding/json"
	"testing"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	margoStdAPI "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strPtr(s string) *string {
	return &s
}

func createTestDeviceWithCache(totalL3Size string, allocTypes []margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes) *margoStdAPI.DeviceCapabilitiesManifest {
	var caps margoStdAPI.DeviceCapabilitiesManifest
	caps.Properties.Id = "test-device-1"
	caps.Properties.Cache = &[]struct {
		AllocationTypes []margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes `json:"allocationTypes"`
		Level           margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheLevel             `json:"level"`
		Size            string                                                                 `json:"size"`
	}{
		{
			Level:           margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
			AllocationTypes: allocTypes,
			Size:            totalL3Size,
		},
	}

	return &caps
}

func createTestDeploymentWithCache(profileCache *string, compCache *string, allocation margoNonStdAPI.DeploymentCacheRequirementAllocation) margoNonStdAPI.ApplicationDeploymentManifestResp {
	dep := margoNonStdAPI.ApplicationDeploymentManifestResp{
		Spec: margoNonStdAPI.ApplicationDeploymentSpec{
			DeploymentProfile: margoNonStdAPI.DeploymentExecutionProfile{
				Type: margoNonStdAPI.DeploymentExecutionProfileTypeCompose,
			},
		},
	}

	if profileCache != nil {
		dep.Spec.DeploymentProfile.DeviceConstraints = &margoNonStdAPI.DeviceConstraints{
			CapacityRequirements: &margoNonStdAPI.CapacityRequirements{
				Cache: &[]margoNonStdAPI.DeploymentCacheRequirement{
					{
						Level:      margoNonStdAPI.L3,
						Allocation: allocation,
						Size:       profileCache,
					},
				},
			},
		}
	}

	if compCache != nil {
		comp := margoNonStdAPI.DeploymentProfileComponent{
			Name: "comp-1",
			DeviceConstraints: &margoNonStdAPI.DeviceConstraints{
				CapacityRequirements: &margoNonStdAPI.CapacityRequirements{
					Cache: &[]margoNonStdAPI.DeploymentCacheRequirement{
						{
							Level:      margoNonStdAPI.L3,
							Allocation: allocation,
							Size:       compCache,
						},
					},
				},
			},
		}
		raw, _ := json.Marshal(comp)
		var item margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		_ = json.Unmarshal(raw, &item)
		dep.Spec.DeploymentProfile.Components = []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{item}
	}

	return dep
}

func TestHasDeviceCacheCapacityAndSupport_ComponentOverridesProfile(t *testing.T) {
	device := createTestDeviceWithCache("16MiB", []margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
		margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
		margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
	})

	// Profile requests 32MiB (which exceeds device 16MiB), but Component overrides with 8MiB (which fits)
	dep := createTestDeploymentWithCache(strPtr("32MiB"), strPtr("8MiB"), margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive)

	exclusiveBytes, err := ValidateRequestedL3CacheCapacity("test-device-1", device, nil, dep)
	require.NoError(t, err)
	assert.Equal(t, int64(8*1024*1024), exclusiveBytes)
}

func TestHasDeviceCacheCapacityAndSupport_UnsupportedAllocationType(t *testing.T) {
	// Device only supports shared cache
	device := createTestDeviceWithCache("16MiB", []margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
		margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
	})

	// Deployment requests exclusive cache
	dep := createTestDeploymentWithCache(nil, strPtr("8MiB"), margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive)

	_, err := ValidateRequestedL3CacheCapacity("test-device-1", device, nil, dep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device does not support \"exclusive\" cache allocation on l3")
}

func TestHasDeviceCacheCapacityAndSupport_CapacityLimits(t *testing.T) {
	device := createTestDeviceWithCache("16MiB", []margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
		margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
	})

	// Existing active deployment already claims 10MiB exclusive
	existingAlloc := DeploymentAllocation{
		DeploymentID:                   "existing-dep-1",
		ExclusiveL3CacheBytesRequested: 10 * 1024 * 1024,
	}

	// New deployment asks for 8MiB -> Total would be 18MiB > 16MiB
	newDep := createTestDeploymentWithCache(nil, strPtr("8MiB"), margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive)
	_, err := ValidateRequestedL3CacheCapacity("test-device-1", device, []DeploymentAllocation{existingAlloc}, newDep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds available exclusive l3 cache")

	// New deployment asks for 6MiB -> Total is 16MiB <= 16MiB, fits exactly
	fittingDep := createTestDeploymentWithCache(nil, strPtr("6MiB"), margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive)
	exclusiveBytes, err := ValidateRequestedL3CacheCapacity("test-device-1", device, []DeploymentAllocation{existingAlloc}, fittingDep)
	require.NoError(t, err)
	assert.Equal(t, int64(6*1024*1024), exclusiveBytes)
}

func TestParseBinarySizeBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"9216Ki", 9216 * 1024, false},
		{"9216KiB", 9216 * 1024, false},
		{"16Mi", 16 * 1024 * 1024, false},
		{"16MiB", 16 * 1024 * 1024, false},
		{"1Gi", 1024 * 1024 * 1024, false},
		{"1000KB", 1000 * 1000, false},
		{"1000B", 1000, false},
		{"", 0, true},
		{"invalid", 0, true},
		{"-5MiB", 0, true},
		{"NaNB", 0, true},
		{"InfB", 0, true},
		{"8388608TiB", 0, true},
	}

	for _, tt := range tests {
		val, err := parseBinarySizeBytes(tt.input)
		if tt.hasError {
			assert.Error(t, err, "input: %s", tt.input)
		} else {
			assert.NoError(t, err, "input: %s", tt.input)
			assert.Equal(t, tt.expected, val, "input: %s", tt.input)
		}
	}
}
