package margo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	nbiModels "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
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
		Size                     string                                                                         `json:"size"`
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

func vendorWorkspaceRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must succeed")
	// vendors/margo -> v1alpha1 -> apis -> pkg -> api -> symphony -> workspace root
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../../../"))
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
			"env":        "production",
			"tier":       float32(1.0),
			"realtime":   true,
			"tags":       []string{"rt", "low-latency"},
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

	// Exercise the production ConvertAtoB bridge used by DeviceMgmtVendor
	var rawCapabilities any
	require.NoError(t, json.Unmarshal(payload, &rawCapabilities))
	symphonyDevice, err := ConvertAtoB[any, clModels.DeviceCapabilitiesManifest](rawCapabilities)
	require.NoError(t, err, "ConvertAtoB into DeviceCapabilitiesManifest must succeed")
	require.NotNil(t, symphonyDevice)

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

	// Also verify cross-schema conversion between nbi.DeviceConstraints and sbi.DeviceConstraints
	nbiClass := nbiModels.DeploymentCpuRequirementClass("performance")
	nbiType := nbiModels.DeploymentCpuRequirementType("isolated")
	nbiConstraints := nbiModels.DeviceConstraints{
		CapacityRequirements: &nbiModels.CapacityRequirements{
			Cpu: &[]nbiModels.DeploymentCpuRequirement{
				{
					Name:  p.Ptr("caterpillar"),
					Cores: 1.0,
					Class: &nbiClass,
					Type:  &nbiType,
				},
			},
			Cache: &[]nbiModels.DeploymentCacheRequirement{
				{
					Level:      nbiModels.DeploymentCacheRequirementLevel("L3"),
					Allocation: nbiModels.DeploymentCacheRequirementAllocation("exclusive"),
					Size:       p.Ptr("9216Ki"),
				},
			},
			Memory: &nbiModels.DeploymentMemoryRequirement{
				Size: "1024Mi",
			},
			Storage: p.Ptr("2Gi"),
		},
	}

	sbiConstraints, err := ConvertAtoB[nbiModels.DeviceConstraints, clModels.DeviceConstraints](nbiConstraints)
	require.NoError(t, err, "ConvertAtoB[nbi.DeviceConstraints, sbi.DeviceConstraints] must succeed")
	require.NotNil(t, sbiConstraints)
	require.NotNil(t, sbiConstraints.CapacityRequirements)
	require.NotNil(t, sbiConstraints.CapacityRequirements.Cpu)
	require.Len(t, *sbiConstraints.CapacityRequirements.Cpu, 1)
	assert.Equal(t, clModels.DeploymentCpuRequirementTypeIsolated, *(*sbiConstraints.CapacityRequirements.Cpu)[0].Type)
	require.NotNil(t, sbiConstraints.CapacityRequirements.Cache)
	require.Len(t, *sbiConstraints.CapacityRequirements.Cache, 1)
	assert.Equal(t, clModels.DeploymentCacheRequirementLevelL3, (*sbiConstraints.CapacityRequirements.Cache)[0].Level)
	assert.Equal(t, clModels.DeploymentCacheRequirementAllocationExclusive, (*sbiConstraints.CapacityRequirements.Cache)[0].Allocation)
}

func TestCapabilitiesContract_CanonicalJSONFixture(t *testing.T) {
	canonicalBytes, err := os.ReadFile(filepath.Join("testdata", "canonical-device-capabilities.json"))
	require.NoError(t, err, "reading testdata/canonical-device-capabilities.json must succeed")

	var manifest clModels.DeviceCapabilitiesManifest
	dec := json.NewDecoder(bytes.NewReader(canonicalBytes))
	dec.DisallowUnknownFields()
	err = dec.Decode(&manifest)
	require.NoError(t, err, "unmarshaling canonical JSON fixture with DisallowUnknownFields must succeed")

	assert.Equal(t, "canon-rt-dev-01", manifest.Properties.Id)
	assert.Equal(t, "Margo Spec Vendor", manifest.Properties.Vendor)
	assert.Equal(t, "M-2026", manifest.Properties.ModelNumber)
	assert.Equal(t, "SN-001-A", manifest.Properties.SerialNumber)
	require.NotNil(t, manifest.Properties.Storage)
	assert.Equal(t, "128Gi", *manifest.Properties.Storage)

	require.NotNil(t, manifest.Properties.Cpus)
	require.Len(t, *manifest.Properties.Cpus, 1)
	cpu := (*manifest.Properties.Cpus)[0]
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64, *cpu.Architecture)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance, *cpu.Class)
	assert.Equal(t, clModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated, *cpu.Type)
	assert.Equal(t, float32(4.0), cpu.Cores)
	require.NotNil(t, cpu.Frequency)
	assert.Equal(t, float32(2600.0), *cpu.Frequency.BaseMHz)
	assert.Equal(t, float32(4900.0), *cpu.Frequency.MaxMHz)

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

	require.NotNil(t, manifest.Properties.SupportedRuntimes)
	assert.ElementsMatch(t, []clModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
		clModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
	}, *manifest.Properties.SupportedRuntimes)

	require.NotNil(t, manifest.Labels)
	assert.Contains(t, *manifest.Labels, "zone")
}

func TestCapabilitiesContract_OpenAPISpecsAlignment(t *testing.T) {
	wsRoot := vendorWorkspaceRoot(t)

	nbiSpecPath := filepath.Join(wsRoot, "sandbox", "non-standard", "spec", "wfm-nbi.yaml")
	sbiSnapshotPath := filepath.Join(wsRoot, "sandbox", "standard", "snapshot.spec.yaml")
	sbiUpstreamSpecPath := filepath.Join(wsRoot, "specification", "system-design", "specification", "margo-management-interface", "workload-management-api-1.0.0-rc.3.yaml")

	nbiBytes, err := os.ReadFile(nbiSpecPath)
	require.NoError(t, err, "must read sandbox/non-standard/spec/wfm-nbi.yaml")

	sbiSnapshotBytes, err := os.ReadFile(sbiSnapshotPath)
	require.NoError(t, err, "must read sandbox/standard/snapshot.spec.yaml")

	var nbiDoc, sbiDoc map[string]interface{}
	require.NoError(t, yaml.Unmarshal(nbiBytes, &nbiDoc))
	require.NoError(t, yaml.Unmarshal(sbiSnapshotBytes, &sbiDoc))

	if upstreamBytes, err := os.ReadFile(sbiUpstreamSpecPath); err == nil {
		var upstreamDoc map[string]interface{}
		require.NoError(t, yaml.Unmarshal(upstreamBytes, &upstreamDoc))
		assert.Equal(t, upstreamDoc, sbiDoc,
			"sandbox/standard/snapshot.spec.yaml parsed OpenAPI structure must match workload-management-api-1.0.0-rc.3.yaml")
	} else {
		require.NoError(t, err, "must read upstream SBI spec %s", sbiUpstreamSpecPath)
	}

	// Validate RT capability, constraint, and workload schema structure in both SBI and NBI specs
	assertOpenAPISchemaPropertiesAndEnums(t, sbiDoc, nbiDoc)

	// Strictly validate all testdata JSON & YAML fixtures against the OpenAPI 3.0 schemas in SBI and NBI specs
	validateFixtureAgainstOpenAPISchema(t, sbiDoc, "DeviceCapabilitiesManifest",
		filepath.Join("testdata", "canonical-device-capabilities.json"))
	validateFixtureAgainstOpenAPISchema(t, sbiDoc, "DeviceCapabilitiesManifest",
		filepath.Join(wsRoot, "symphony", "api", "pkg", "apis", "v1alpha1", "managers", "margo", "testdata", "k3s-device-capabilities.json"))
	validateFixtureAgainstOpenAPISchema(t, sbiDoc, "DeviceCapabilitiesManifest",
		filepath.Join(wsRoot, "symphony", "api", "pkg", "apis", "v1alpha1", "managers", "margo", "testdata", "compose-device-capabilities.json"))

	for _, pkgFile := range []string{
		"rt-benchmark-experiment-margo.yaml",
		"rt-benchmark-icore-margo.yaml",
		"rt-benchmark-iecache-margo.yaml",
		"rt-benchmark-iscache-margo.yaml",
	} {
		validateMargoYAMLRTSubschemas(t, nbiDoc,
			filepath.Join(wsRoot, "symphony", "api", "pkg", "apis", "v1alpha1", "managers", "margo", "testdata", pkgFile))
	}

	// Verify SBI /api/v1/capabilities/{deviceId} PUT endpoint and RT capability schemas exist in spec
	sbiPaths, ok := sbiDoc["paths"].(map[string]interface{})
	require.True(t, ok)
	capPath, ok := sbiPaths["/api/v1/capabilities/{deviceId}"].(map[string]interface{})
	require.True(t, ok, "SBI spec must define /api/v1/capabilities/{deviceId}")
	assert.Contains(t, capPath, "put", "SBI spec must define PUT on /api/v1/capabilities/{deviceId}")

	sbiComponents, ok := sbiDoc["components"].(map[string]interface{})
	require.True(t, ok)
	sbiSchemas, ok := sbiComponents["schemas"].(map[string]interface{})
	require.True(t, ok)
	for _, schemaName := range []string{
		"DeviceCapabilitiesManifest",
		"DeviceConstraints",
		"CapacityRequirements",
		"DeploymentCpuRequirement",
		"DeploymentCacheRequirement",
		"DeploymentMemoryRequirement",
		"Workload",
	} {
		assert.Contains(t, sbiSchemas, schemaName, "SBI OpenAPI spec must contain schema %s", schemaName)
	}

	// Verify NBI OpenAPI spec contains matching RT deployment constraint schemas
	nbiComponents, ok := nbiDoc["components"].(map[string]interface{})
	require.True(t, ok)
	nbiSchemas, ok := nbiComponents["schemas"].(map[string]interface{})
	require.True(t, ok)
	for _, schemaName := range []string{
		"ApplicationDeploymentManifestRequest",
		"DeviceConstraints",
		"CapacityRequirements",
		"DeploymentCpuRequirement",
		"DeploymentCacheRequirement",
		"DeploymentMemoryRequirement",
		"Workload",
	} {
		assert.Contains(t, nbiSchemas, schemaName, "NBI OpenAPI spec must contain schema %s", schemaName)
	}
}

func assertOpenAPISchemaPropertiesAndEnums(t *testing.T, sbiDoc, nbiDoc map[string]interface{}) {
	t.Helper()

	sbiSchemas := sbiDoc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	nbiSchemas := nbiDoc["components"].(map[string]interface{})["schemas"].(map[string]interface{})

	// Shared RT schemas that must stay structurally aligned between SBI and NBI
	sharedSpecs := []struct {
		schemaName   string
		requiredKeys []string
		propTypes    map[string]string
		propEnums    map[string][]interface{}
	}{
		{
			schemaName:   "DeploymentCpuRequirement",
			requiredKeys: []string{"cores"},
			propTypes: map[string]string{
				"cores":         "number",
				"architectures": "array",
				"class":         "string",
				"type":          "string",
				"name":          "string",
			},
			propEnums: map[string][]interface{}{
				"class": {"performance", "efficiency", "low-power"},
				"type":  {"isolated", "shared"},
			},
		},
		{
			schemaName:   "DeploymentCacheRequirement",
			requiredKeys: []string{"level", "allocation"},
			propTypes: map[string]string{
				"level":      "string",
				"allocation": "string",
				"size":       "string",
			},
			propEnums: map[string][]interface{}{
				"level":      {"L3", "L2", "L1d", "L1i"},
				"allocation": {"exclusive", "shared"},
			},
		},
		{
			schemaName:   "DeploymentMemoryRequirement",
			requiredKeys: []string{"size"},
			propTypes: map[string]string{
				"size": "string",
			},
		},
		{
			schemaName: "Workload",
			propTypes: map[string]string{
				"priority": "integer",
				"nice":     "integer",
			},
		},
		{
			schemaName: "WorkloadDeadline",
			propTypes: map[string]string{
				"deadline": "integer",
				"period":   "integer",
				"runtime":  "integer",
			},
		},
	}

	for _, spec := range sharedSpecs {
		for docLabel, schemas := range map[string]map[string]interface{}{"SBI": sbiSchemas, "NBI": nbiSchemas} {
			schemaObj, ok := schemas[spec.schemaName].(map[string]interface{})
			require.True(t, ok, "%s schema %s must be an object", docLabel, spec.schemaName)

			if len(spec.requiredKeys) > 0 {
				reqSlice, ok := schemaObj["required"].([]interface{})
				require.True(t, ok, "%s.%s must declare required fields", docLabel, spec.schemaName)
				assert.ElementsMatch(t, toInterfaceSlice(spec.requiredKeys), reqSlice, "%s.%s required fields mismatch", docLabel, spec.schemaName)
			}

			props, ok := schemaObj["properties"].(map[string]interface{})
			require.True(t, ok, "%s.%s must have properties", docLabel, spec.schemaName)
			for propName, wantType := range spec.propTypes {
				propMap, ok := props[propName].(map[string]interface{})
				require.True(t, ok, "%s.%s must have property %s", docLabel, spec.schemaName, propName)
				assert.Equal(t, wantType, propMap["type"], "%s.%s.%s type mismatch", docLabel, spec.schemaName, propName)
			}
			for propName, wantEnums := range spec.propEnums {
				propMap := props[propName].(map[string]interface{})
				assert.ElementsMatch(t, wantEnums, propMap["enum"], "%s.%s.%s enum mismatch", docLabel, spec.schemaName, propName)
			}
		}
	}

	// Verify WorkloadSchedulingType enum in both SBI and NBI
	for docLabel, schemas := range map[string]map[string]interface{}{"SBI": sbiSchemas, "NBI": nbiSchemas} {
		schedType, ok := schemas["WorkloadSchedulingType"].(map[string]interface{})
		require.True(t, ok, "%s must define WorkloadSchedulingType", docLabel)
		assert.ElementsMatch(t,
			[]interface{}{"background", "best-effort", "deadline", "real-time"},
			schedType["enum"],
			"%s WorkloadSchedulingType enum mismatch", docLabel,
		)
	}
}

func toInterfaceSlice(strs []string) []interface{} {
	out := make([]interface{}, len(strs))
	for i, s := range strs {
		out[i] = s
	}
	return out
}

func validateFixtureAgainstOpenAPISchema(t *testing.T, doc map[string]interface{}, schemaName string, fixturePath string) {
	t.Helper()
	rawBytes, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "must read fixture %s", fixturePath)

	var jsonBytes []byte
	if strings.HasSuffix(fixturePath, ".yaml") || strings.HasSuffix(fixturePath, ".yml") {
		jsonBytes, err = yaml.YAMLToJSON(rawBytes)
		require.NoError(t, err, "must convert YAML fixture %s to JSON", fixturePath)
	} else {
		jsonBytes = rawBytes
	}

	var value interface{}
	require.NoError(t, json.Unmarshal(jsonBytes, &value))

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	rootSchema, ok := schemas[schemaName].(map[string]interface{})
	require.True(t, ok, "schema %s must exist in OpenAPI document", schemaName)

	err = validateOpenAPIValue(doc, rootSchema, value, schemaName)
	require.NoError(t, err, "fixture %s failed OpenAPI schema %s validation", filepath.Base(fixturePath), schemaName)
}

func validateMargoYAMLRTSubschemas(t *testing.T, nbiDoc map[string]interface{}, fixturePath string) {
	t.Helper()
	rawBytes, err := os.ReadFile(fixturePath)
	require.NoError(t, err, "must read margo.yaml fixture %s", fixturePath)

	jsonBytes, err := yaml.YAMLToJSON(rawBytes)
	require.NoError(t, err, "must convert %s to JSON", fixturePath)

	// Verify strict Go struct unmarshaling against generated nbiModels.AppDescription
	var appDesc nbiModels.AppDescription
	require.NoError(t, json.Unmarshal(jsonBytes, &appDesc), "unmarshaling %s into nbi.AppDescription must succeed", filepath.Base(fixturePath))

	var root map[string]interface{}
	require.NoError(t, json.Unmarshal(jsonBytes, &root))

	schemas := nbiDoc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	compSchema := schemas["ApplicationDeploymentProfileComponent"].(map[string]interface{})
	dcSchema := schemas["DeviceConstraints"].(map[string]interface{})
	paramMapSchema := schemas["AppDescriptionParametersMap"].(map[string]interface{})
	configSchema := schemas["AppConfigurationSchema"].(map[string]interface{})

	profiles, ok := root["deploymentProfiles"].([]interface{})
	require.True(t, ok && len(profiles) > 0, "%s must contain deploymentProfiles", filepath.Base(fixturePath))

	for pIdx, pRaw := range profiles {
		pMap, ok := pRaw.(map[string]interface{})
		require.True(t, ok)

		if dc, exists := pMap["deviceConstraints"]; exists {
			require.NoError(t, validateOpenAPIValue(nbiDoc, dcSchema, dc, fmt.Sprintf("%s.deploymentProfiles[%d].deviceConstraints", filepath.Base(fixturePath), pIdx)))
		}

		comps, ok := pMap["components"].([]interface{})
		require.True(t, ok && len(comps) > 0)
		for cIdx, cRaw := range comps {
			require.NoError(t, validateOpenAPIValue(nbiDoc, compSchema, cRaw, fmt.Sprintf("%s.deploymentProfiles[%d].components[%d]", filepath.Base(fixturePath), pIdx, cIdx)))
		}
	}

	if params, exists := root["parameters"]; exists {
		require.NoError(t, validateOpenAPIValue(nbiDoc, paramMapSchema, params, filepath.Base(fixturePath)+".parameters"))
	}
	if cfg, exists := root["configuration"]; exists {
		require.NoError(t, validateOpenAPIValue(nbiDoc, configSchema, cfg, filepath.Base(fixturePath)+".configuration"))
	}
}

func resolveOpenAPISchemaRef(doc, schema map[string]interface{}) map[string]interface{} {
	if ref, ok := schema["$ref"].(string); ok {
		const prefix = "#/components/schemas/"
		if strings.HasPrefix(ref, prefix) {
			name := strings.TrimPrefix(ref, prefix)
			schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
			if target, exists := schemas[name].(map[string]interface{}); exists {
				return target
			}
		}
	}
	return schema
}

func validateOpenAPIValue(doc, schema map[string]interface{}, val interface{}, path string) error {
	schema = resolveOpenAPISchemaRef(doc, schema)
	if val == nil {
		return nil
	}

	if oneOf, ok := schema["oneOf"].([]interface{}); ok && len(oneOf) > 0 {
		for _, candidateRaw := range oneOf {
			if candidate, ok := candidateRaw.(map[string]interface{}); ok {
				if validateOpenAPIValue(doc, candidate, val, path) == nil {
					return nil
				}
			}
		}
		return fmt.Errorf("%s: value %T did not match any oneOf branch", path, val)
	}

	if enumVals, ok := schema["enum"].([]interface{}); ok {
		matched := false
		for _, ev := range enumVals {
			if fmt.Sprintf("%v", ev) == fmt.Sprintf("%v", val) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value %v not in enum %v", path, val, enumVals)
		}
	}

	schemaType, _ := schema["type"].(string)
	// Skip strict object/map check when x-go-type is interface{} (e.g., parameter values)
	if xGoType, ok := schema["x-go-type"].(string); ok && xGoType == "interface{}" {
		return nil
	}

	switch schemaType {
	case "object", "":
		objVal, ok := val.(map[string]interface{})
		if !ok {
			if schemaType == "object" {
				return fmt.Errorf("%s: expected object, got %T", path, val)
			}
			return nil
		}

		if reqList, ok := schema["required"].([]interface{}); ok {
			for _, reqFieldRaw := range reqList {
				reqField, _ := reqFieldRaw.(string)
				if strings.Contains(reqField, ".") {
					continue
				}
				if _, exists := objVal[reqField]; !exists {
					return fmt.Errorf("%s: missing required field %q", path, reqField)
				}
			}
		}

		props, hasProps := schema["properties"].(map[string]interface{})
		addlProps := schema["additionalProperties"]

		for k, v := range objVal {
			if hasProps {
				if propSchemaRaw, exists := props[k]; exists {
					if propSchema, ok := propSchemaRaw.(map[string]interface{}); ok {
						if err := validateOpenAPIValue(doc, propSchema, v, path+"."+k); err != nil {
							return err
						}
					}
					continue
				}
			}
			if strings.HasPrefix(k, "x-") && strings.HasSuffix(k, "-extensions") {
				continue
			}
			if addlSchema, ok := addlProps.(map[string]interface{}); ok {
				if err := validateOpenAPIValue(doc, addlSchema, v, path+"."+k); err != nil {
					return err
				}
				continue
			}
			if addlBool, ok := addlProps.(bool); ok && addlBool {
				continue
			}
			if hasProps {
				return fmt.Errorf("%s: unexpected property %q not defined in OpenAPI schema", path, k)
			}
		}

	case "array":
		arrVal, ok := val.([]interface{})
		if !ok {
			return fmt.Errorf("%s: expected array, got %T", path, val)
		}
		if itemsSchema, ok := schema["items"].(map[string]interface{}); ok {
			for idx, item := range arrVal {
				if err := validateOpenAPIValue(doc, itemsSchema, item, fmt.Sprintf("%s[%d]", path, idx)); err != nil {
					return err
				}
			}
		}

	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("%s: expected string, got %T (%v)", path, val, val)
		}

	case "number":
		if _, ok := val.(float64); !ok {
			return fmt.Errorf("%s: expected number, got %T (%v)", path, val, val)
		}

	case "integer":
		num, ok := val.(float64)
		if !ok || num != float64(int64(num)) {
			return fmt.Errorf("%s: expected integer, got %T (%v)", path, val, val)
		}

	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("%s: expected boolean, got %T (%v)", path, val, val)
		}
	}

	return nil
}

