package margo

import (
	"encoding/json"
	"testing"

	p "github.com/margo/sandbox/shared-lib/pointers"
	clModels "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type contractTestCPU = struct {
	Architecture *clModels.DeviceCapabilitiesManifestPropertiesCpusArchitecture `json:"architecture,omitempty"`
	Class        *clModels.DeviceCapabilitiesManifestPropertiesCpusClass        `json:"class,omitempty"`
	Cores        float32                                                        `json:"cores"`
	Frequency    *struct {
		BaseMHz *float32 `json:"baseMHz,omitempty"`
		MaxMHz  *float32 `json:"maxMHz,omitempty"`
	} `json:"frequency,omitempty"`
	Type *clModels.DeviceCapabilitiesManifestPropertiesCpusType `json:"type,omitempty"`
}

type contractTestCache = struct {
	AllocationTypes []clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes `json:"allocationTypes"`
	Level           clModels.DeviceCapabilitiesManifestPropertiesCacheLevel             `json:"level"`
	Size            string                                                              `json:"size"`
}

func buildContractTestDevice(
	id, vendor, modelNumber, serialNumber string,
) *clModels.DeviceCapabilitiesManifest {
	d := &clModels.DeviceCapabilitiesManifest{}
	d.Properties.Id = id
	d.Properties.Vendor = vendor
	d.Properties.ModelNumber = modelNumber
	d.Properties.SerialNumber = serialNumber
	return d
}

func withContractCPU(
	device *clModels.DeviceCapabilitiesManifest,
	arch clModels.DeviceCapabilitiesManifestPropertiesCpusArchitecture,
	cpuClass clModels.DeviceCapabilitiesManifestPropertiesCpusClass,
	cpuType clModels.DeviceCapabilitiesManifestPropertiesCpusType,
	cores float32,
	baseMHz float32,
	maxMHz float32,
) *clModels.DeviceCapabilitiesManifest {
	cpu := contractTestCPU{
		Architecture: &arch,
		Class:        &cpuClass,
		Type:         &cpuType,
		Cores:        cores,
		Frequency: &struct {
			BaseMHz *float32 `json:"baseMHz,omitempty"`
			MaxMHz  *float32 `json:"maxMHz,omitempty"`
		}{
			BaseMHz: &baseMHz,
			MaxMHz:  &maxMHz,
		},
	}
	if device.Properties.Cpus == nil {
		device.Properties.Cpus = &[]contractTestCPU{}
	}
	*device.Properties.Cpus = append(*device.Properties.Cpus, cpu)
	return device
}

func withContractCache(
	device *clModels.DeviceCapabilitiesManifest,
	level clModels.DeviceCapabilitiesManifestPropertiesCacheLevel,
	allocations []clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes,
	size string,
) *clModels.DeviceCapabilitiesManifest {
	cacheItem := contractTestCache{
		Level:           level,
		AllocationTypes: allocations,
		Size:            size,
	}
	if device.Properties.Cache == nil {
		device.Properties.Cache = &[]contractTestCache{}
	}
	*device.Properties.Cache = append(*device.Properties.Cache, cacheItem)
	return device
}

func withContractMemory(
	device *clModels.DeviceCapabilitiesManifest,
	size string,
	bandwidthAllocationTypes []clModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes,
) *clModels.DeviceCapabilitiesManifest {
	device.Properties.Memory = &struct {
		BandwidthAllocationTypes *[]clModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes `json:"bandwidthAllocationTypes,omitempty"`
		Size                     string                                                                 `json:"size"`
	}{
		Size:                     size,
		BandwidthAllocationTypes: &bandwidthAllocationTypes,
	}
	return device
}

func withContractLabels(
	device *clModels.DeviceCapabilitiesManifest,
	labels map[string]interface{},
) *clModels.DeviceCapabilitiesManifest {
	result := make(
		map[string]clModels.DeviceCapabilitiesManifest_Labels_AdditionalProperties,
		len(labels),
	)
	for k, v := range labels {
		raw, err := json.Marshal(v)
		if err != nil {
			panic("withContractLabels: failed to marshal label value: " + err.Error())
		}
		var prop clModels.DeviceCapabilitiesManifest_Labels_AdditionalProperties
		if err := json.Unmarshal(raw, &prop); err != nil {
			panic("withContractLabels: failed to unmarshal label value: " + err.Error())
		}
		result[k] = prop
	}
	device.Labels = &result
	return device
}

func TestCapabilitiesContract_RTSerialization(t *testing.T) {
	device := buildContractTestDevice("test-device-rt-01", "Acme Industrial", "RT-Edge-9000", "SN-88234-A")

	withContractCPU(
		device,
		clModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64,
		clModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance,
		clModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated,
		4.0,
		2400.0,
		4800.0,
	)

	withContractCache(
		device,
		clModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
		[]clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
			clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
			clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
		},
		"9216Ki",
	)

	withContractMemory(
		device,
		"1024Mi",
		[]clModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
			clModels.PercentThrottling,
			clModels.RateLimiting,
		},
	)

	withContractLabels(
		device,
		map[string]interface{}{
			"env":       "production",
			"tier":      float32(1.0),
			"realtime":  true,
			"tags":      []string{"rt", "low-latency"},
			"priorities": []float32{1.0, 2.0},
		},
	)

	device.Properties.Storage = p.Ptr("64Gi")
	device.Properties.SupportedDeploymentTypes = &[]clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
		clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm,
		clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose,
	}
	device.Properties.SupportedRuntimes = &[]clModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
		clModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
	}

	payload, err := json.Marshal(device)
	require.NoError(t, err, "failed to marshal Sandbox client DeviceCapabilitiesManifest")
	require.NotEmpty(t, payload)

	var symphonyDevice clModels.DeviceCapabilitiesManifest
	err = json.Unmarshal(payload, &symphonyDevice)
	require.NoError(t, err, "failed to unmarshal into Symphony DeviceCapabilitiesManifest")

	assert.Equal(t, "test-device-rt-01", symphonyDevice.Properties.Id)
	assert.Equal(t, "Acme Industrial", symphonyDevice.Properties.Vendor)
	assert.Equal(t, "RT-Edge-9000", symphonyDevice.Properties.ModelNumber)
	assert.Equal(t, "SN-88234-A", symphonyDevice.Properties.SerialNumber)

	require.NotNil(t, symphonyDevice.Properties.Cpus)
	require.Len(t, *symphonyDevice.Properties.Cpus, 1)
	cpu := (*symphonyDevice.Properties.Cpus)[0]
	require.NotNil(t, cpu.Architecture)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64, *cpu.Architecture)
	require.NotNil(t, cpu.Class)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance, *cpu.Class)
	require.NotNil(t, cpu.Type)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated, *cpu.Type)
	assert.Equal(t, float32(4.0), cpu.Cores)
	require.NotNil(t, cpu.Frequency)
	require.NotNil(t, cpu.Frequency.BaseMHz)
	assert.Equal(t, float32(2400.0), *cpu.Frequency.BaseMHz)
	require.NotNil(t, cpu.Frequency.MaxMHz)
	assert.Equal(t, float32(4800.0), *cpu.Frequency.MaxMHz)

	require.NotNil(t, symphonyDevice.Properties.Cache)
	require.Len(t, *symphonyDevice.Properties.Cache, 1)
	cacheEntry := (*symphonyDevice.Properties.Cache)[0]
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3, cacheEntry.Level)
	assert.Equal(t, "9216Ki", cacheEntry.Size)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
		clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
		clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
	}, cacheEntry.AllocationTypes)

	require.NotNil(t, symphonyDevice.Properties.Memory)
	assert.Equal(t, "1024Mi", symphonyDevice.Properties.Memory.Size)
	require.NotNil(t, symphonyDevice.Properties.Memory.BandwidthAllocationTypes)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
		clModels.PercentThrottling,
		clModels.RateLimiting,
	}, *symphonyDevice.Properties.Memory.BandwidthAllocationTypes)

	require.NotNil(t, symphonyDevice.Properties.Storage)
	assert.Equal(t, "64Gi", *symphonyDevice.Properties.Storage)

	require.NotNil(t, symphonyDevice.Properties.SupportedDeploymentTypes)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
		clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm,
		clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose,
	}, *symphonyDevice.Properties.SupportedDeploymentTypes)

	require.NotNil(t, symphonyDevice.Properties.SupportedRuntimes)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
		clModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
	}, *symphonyDevice.Properties.SupportedRuntimes)

	require.NotNil(t, symphonyDevice.Labels)
	assert.Contains(t, *symphonyDevice.Labels, "env")
	assert.Contains(t, *symphonyDevice.Labels, "realtime")
	assert.Contains(t, *symphonyDevice.Labels, "tags")

	roundTripPayload, err := json.Marshal(symphonyDevice)
	require.NoError(t, err)
	assert.JSONEq(t, string(payload), string(roundTripPayload), "bidirectional JSON serialization must match exactly")
}

func TestCapabilitiesContract_CanonicalJSONFixture(t *testing.T) {
	canonicalJSON := `{
		"properties": {
			"id": "canon-rt-dev-01",
			"vendor": "Margo Spec Vendor",
			"modelNumber": "M-2026",
			"serialNumber": "SN-001-A",
			"storage": "128Gi",
			"cpus": [
				{
					"architecture": "amd64",
					"class": "performance",
					"type": "isolated",
					"cores": 4.0,
					"frequency": {
						"baseMHz": 2600.0,
						"maxMHz": 4900.0
					}
				}
			],
			"cache": [
				{
					"level": "L3",
					"allocationTypes": ["exclusive", "shared"],
					"size": "9216Ki"
				}
			],
			"memory": {
				"size": "1024Mi",
				"bandwidthAllocationTypes": ["percent-throttling", "rate-limiting"]
			},
			"supportedDeploymentTypes": ["helm", "compose"],
			"supportedRuntimes": ["oci"]
		},
		"labels": {
			"zone": "us-east-1"
		}
	}`

	var manifest clModels.DeviceCapabilitiesManifest
	err := json.Unmarshal([]byte(canonicalJSON), &manifest)
	require.NoError(t, err, "unmarshaling canonical JSON fixture must succeed")

	assert.Equal(t, "canon-rt-dev-01", manifest.Properties.Id)
	assert.Equal(t, "Margo Spec Vendor", manifest.Properties.Vendor)
	assert.Equal(t, "128Gi", *manifest.Properties.Storage)

	require.NotNil(t, manifest.Properties.Cpus)
	require.Len(t, *manifest.Properties.Cpus, 1)
	cpu := (*manifest.Properties.Cpus)[0]
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64, *cpu.Architecture)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance, *cpu.Class)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated, *cpu.Type)
	assert.Equal(t, float32(4.0), cpu.Cores)

	require.NotNil(t, manifest.Properties.Cache)
	require.Len(t, *manifest.Properties.Cache, 1)
	cache := (*manifest.Properties.Cache)[0]
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3, cache.Level)
	assert.Equal(t, "9216Ki", cache.Size)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
		clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
		clModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
	}, cache.AllocationTypes)

	require.NotNil(t, manifest.Properties.Memory)
	assert.Equal(t, "1024Mi", manifest.Properties.Memory.Size)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
		clModels.PercentThrottling,
		clModels.RateLimiting,
	}, *manifest.Properties.Memory.BandwidthAllocationTypes)

	require.NotNil(t, manifest.Properties.SupportedDeploymentTypes)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
		clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm,
		clModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose,
	}, *manifest.Properties.SupportedDeploymentTypes)

	require.NotNil(t, manifest.Labels)
	assert.Contains(t, *manifest.Labels, "zone")
}
