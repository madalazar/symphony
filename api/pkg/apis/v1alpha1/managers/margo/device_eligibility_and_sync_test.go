package margo

import (
	"context"
	"encoding/json"
	"testing"

	powerfulYaml "github.com/ghodss/yaml"
	coa_ctx "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states/memorystate"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/margo/sandbox/shared-lib/constraints"
	p "github.com/margo/sandbox/shared-lib/pointers"
	"github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildEligibilityDevice creates a DeviceCapabilitiesManifest with specific deployment types and RT specs
func buildEligibilityDevice(
	id string,
	deploymentType sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes,
) *sbi.DeviceCapabilitiesManifest {
	dev := &sbi.DeviceCapabilitiesManifest{}
	dev.Properties.Id = id
	dev.Properties.Vendor = "Industrial Edge"
	dev.Properties.ModelNumber = "RT-Test-Node"
	dev.Properties.SerialNumber = "SN-RT-999"

	arch := sbi.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64
	cpuClass := sbi.DeviceCapabilitiesManifestPropertiesCpusClassPerformance
	cpuType := sbi.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated
	baseMHz := float32(3000.0)
	maxMHz := float32(5000.0)

	dev.Properties.Cpus = &[]struct {
		Architecture *sbi.DeviceCapabilitiesManifestPropertiesCpusArchitecture `json:"architecture,omitempty"`
		Class        *sbiModelsCpusClass                                        `json:"class,omitempty"`
		Cores        float32                                                    `json:"cores"`
		Frequency    *struct {
			BaseMHz *float32 `json:"baseMHz,omitempty"`
			MaxMHz  *float32 `json:"maxMHz,omitempty"`
		} `json:"frequency,omitempty"`
		Type *sbiModelsCpusType `json:"type,omitempty"`
	}{
		{
			Architecture: &arch,
			Class:        &cpuClass,
			Type:         &cpuType,
			Cores:        8.0,
			Frequency: &struct {
				BaseMHz *float32 `json:"baseMHz,omitempty"`
				MaxMHz  *float32 `json:"maxMHz,omitempty"`
			}{
				BaseMHz: &baseMHz,
				MaxMHz:  &maxMHz,
			},
		},
	}

	dev.Properties.Cache = &[]struct {
		AllocationTypes []sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes `json:"allocationTypes"`
		Level           sbi.DeviceCapabilitiesManifestPropertiesCacheLevel             `json:"level"`
		Size            string                                                         `json:"size"`
	}{
		{
			Level: sbi.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
			AllocationTypes: []sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
				sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
				sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
			},
			Size: "9216Ki",
		},
	}

	dev.Properties.Memory = &struct {
		BandwidthAllocationTypes *[]sbi.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes `json:"bandwidthAllocationTypes,omitempty"`
		Size                     string                                                              `json:"size"`
	}{
		Size: "1024Mi",
	}

	dev.Properties.Storage = p.Ptr("64Gi")

	dev.Properties.SupportedDeploymentTypes = &[]sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
		deploymentType,
	}
	dev.Properties.SupportedRuntimes = &[]sbi.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
		sbi.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
	}

	return dev
}

type sbiModelsCpusClass = sbi.DeviceCapabilitiesManifestPropertiesCpusClass
type sbiModelsCpusType = sbi.DeviceCapabilitiesManifestPropertiesCpusType

// extractComponentCapacityRequirements helper matching sandbox/poc/device/agent/deployment.go
func extractComponentCapacityRequirements(
	component sbi.ApplicationDeploymentProfileComponent,
) *sbi.CapacityRequirements {
	if component.DeviceConstraints != nil && component.DeviceConstraints.CapacityRequirements != nil {
		return component.DeviceConstraints.CapacityRequirements
	}
	return nil
}

func TestDeviceEligibility_HelmAndCompose(t *testing.T) {
	appPkg := setupTestAppPackage(t)
	ctx := context.Background()

	stateProvider := &memorystate.MemoryStateProvider{}
	err := stateProvider.Init(memorystate.MemoryStateProviderConfig{})
	require.NoError(t, err)

	vContext := &coa_ctx.VendorContext{}
	err = vContext.Init(nil)
	require.NoError(t, err)

	mContext := &coa_ctx.ManagerContext{}
	err = mContext.Init(vContext, nil)
	require.NoError(t, err)

	db := NewMargoDatabase(mContext, "eligibility-test", stateProvider)
	stateMachine := NewDeploymentStateMachine(db, deploymentLogger)
	transformer := NewMargoTransformer()

	// Set bundleManager to nil to prevent background goroutines from spawning and leaking archive files in /tmp
	deployMgr := &DeploymentManager{
		database:      db,
		stateMachine:  stateMachine,
		tranformer:    transformer,
		bundleManager: nil,
	}

	// 1. Create Helm deployment
	helmReq := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
	helmReq.Metadata.Name = "rt-benchmark-helm-instance"
	helmReq.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
		AppPackageRef: struct {
			Id string `json:"id"`
		}{
			Id: "rt-benchmark-experiment-suite",
		},
		DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
			Id: p.Ptr("helm-target-device"),
		},
		DeploymentProfile: margoNonStdAPI.DeploymentExecutionProfile{
			Type: margoNonStdAPI.DeploymentExecutionProfileType("helm"),
			Components: []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{
				{}, // caterpillar
				{}, // cyclictest
				{}, // stressng
			},
		},
	}
	require.NoError(t, helmReq.Spec.DeploymentProfile.Components[0].FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: "caterpillar"}))
	require.NoError(t, helmReq.Spec.DeploymentProfile.Components[1].FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: "cyclictest"}))
	require.NoError(t, helmReq.Spec.DeploymentProfile.Components[2].FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: "stressng"}))

	helmResp, err := deployMgr.CreateDeployment(ctx, helmReq, appPkg)
	require.NoError(t, err)

	// 2. Create Compose deployment
	composeReq := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
	composeReq.Metadata.Name = "rt-benchmark-compose-instance"
	composeReq.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
		AppPackageRef: struct {
			Id string `json:"id"`
		}{
			Id: "rt-benchmark-experiment-suite",
		},
		DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
			Id: p.Ptr("compose-target-device"),
		},
		DeploymentProfile: margoNonStdAPI.DeploymentExecutionProfile{
			Type: margoNonStdAPI.DeploymentExecutionProfileType("compose"),
			Components: []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{
				{}, // cyclictest_compose
				{}, // stressng_compose
				{}, // caterpillar_compose
			},
		},
	}
	require.NoError(t, composeReq.Spec.DeploymentProfile.Components[0].FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: "cyclictest_compose"}))
	require.NoError(t, composeReq.Spec.DeploymentProfile.Components[1].FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: "stressng_compose"}))
	require.NoError(t, composeReq.Spec.DeploymentProfile.Components[2].FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: "caterpillar_compose"}))

	composeResp, err := deployMgr.CreateDeployment(ctx, composeReq, appPkg)
	require.NoError(t, err)

	// 3. Obtain the extracted deployments as sbi.AppDeploymentManifest (client representation)
	helmRow, err := db.GetDeployment(ctx, *helmResp.Id)
	require.NoError(t, err)
	composeRow, err := db.GetDeployment(ctx, *composeResp.Id)
	require.NoError(t, err)

	helmDeploymentManifest := convertToSbiManifest(t, helmRow.DesiredState.AppDeploymentManifest)
	composeDeploymentManifest := convertToSbiManifest(t, composeRow.DesiredState.AppDeploymentManifest)

	// Build test devices
	helmDevice := buildEligibilityDevice("helm-device-01", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm)
	composeDevice := buildEligibilityDevice("compose-device-01", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose)

	engine := constraints.New()

	// --- 2.6 Eligibility Matrix Evaluation ---

	t.Run("2.6.A Helm Profile on Helm Device -> Eligible", func(t *testing.T) {
		// Component-level eligibility check against device capabilities
		catComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[0]
		eligible, reason, err := engine.IsDeviceEligible(helmDevice, catComp.DeviceConstraints)
		require.NoError(t, err)
		assert.True(t, eligible, "caterpillar component must be eligible on helm device, reason: %s", reason)

		// Profile-level eligibility check
		eligible, reason, err = engine.IsDeviceEligible(helmDevice, helmDeploymentManifest.Spec.DeploymentProfile.DeviceConstraints)
		require.NoError(t, err)
		assert.True(t, eligible, "helm profile must be eligible on helm device, reason: %s", reason)
	})

	t.Run("2.6.B Compose Profile on Helm Device -> Ineligible", func(t *testing.T) {
		cyclicComp := composeDeploymentManifest.Spec.DeploymentProfile.Components[0]
		eligible, reason, err := engine.IsDeviceEligible(helmDevice, cyclicComp.DeviceConstraints)
		require.NoError(t, err)
		assert.False(t, eligible, "compose component must NOT be eligible on helm device")
		assert.NotEmpty(t, reason, "ineligibility reason should be populated")
	})

	t.Run("2.6.C Compose Profile on Compose Device -> Eligible", func(t *testing.T) {
		cyclicComp := composeDeploymentManifest.Spec.DeploymentProfile.Components[0]
		eligible, reason, err := engine.IsDeviceEligible(composeDevice, cyclicComp.DeviceConstraints)
		require.NoError(t, err)
		assert.True(t, eligible, "cyclictest_compose component must be eligible on compose device, reason: %s", reason)

		catComp := composeDeploymentManifest.Spec.DeploymentProfile.Components[2]
		eligible, reason, err = engine.IsDeviceEligible(composeDevice, catComp.DeviceConstraints)
		require.NoError(t, err)
		assert.True(t, eligible, "caterpillar_compose component must be eligible on compose device, reason: %s", reason)
	})

	t.Run("2.6.D Helm Profile on Compose Device -> Ineligible", func(t *testing.T) {
		catComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[0]
		eligible, reason, err := engine.IsDeviceEligible(composeDevice, catComp.DeviceConstraints)
		require.NoError(t, err)
		assert.False(t, eligible, "helm component must NOT be eligible on compose device")
		assert.NotEmpty(t, reason, "ineligibility reason should be populated")
	})

	// --- Component Constraint Extraction Validation ---

	t.Run("2.6.E Component Capacity Extraction (Helm Caterpillar & Cyclictest)", func(t *testing.T) {
		catComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[0]
		catCaps := extractComponentCapacityRequirements(catComp)
		require.NotNil(t, catCaps, "caterpillar capacity requirements must be extracted")
		require.NotNil(t, catCaps.Cpu)
		require.Len(t, *catCaps.Cpu, 1)
		assert.Equal(t, float32(1), (*catCaps.Cpu)[0].Cores)
		assert.Equal(t, sbi.DeploymentCpuRequirementTypeIsolated, *(*catCaps.Cpu)[0].Type)

		require.NotNil(t, catCaps.Cache)
		require.Len(t, *catCaps.Cache, 1)
		assert.Equal(t, sbi.DeploymentCacheRequirementLevelL3, (*catCaps.Cache)[0].Level)
		assert.Equal(t, sbi.DeploymentCacheRequirementAllocationExclusive, (*catCaps.Cache)[0].Allocation)
		assert.Equal(t, "9216Ki", *(*catCaps.Cache)[0].Size)

		// Also extract cyclictest capacity requirements
		cyclicComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[1]
		cyclicCaps := extractComponentCapacityRequirements(cyclicComp)
		require.NotNil(t, cyclicCaps, "cyclictest capacity requirements must be extracted")
		require.NotNil(t, cyclicCaps.Cpu)
		require.Len(t, *cyclicCaps.Cpu, 1)
		assert.Equal(t, float32(1), (*cyclicCaps.Cpu)[0].Cores)
		assert.Equal(t, sbi.DeploymentCpuRequirementTypeIsolated, *(*cyclicCaps.Cpu)[0].Type)
	})

	t.Run("2.6.F Component Workload & Cache Extraction (Compose Cyclictest & Caterpillar)", func(t *testing.T) {
		cyclicComp := composeDeploymentManifest.Spec.DeploymentProfile.Components[0]
		require.NotNil(t, cyclicComp.Workload, "cyclictest_compose workload must be extracted")
		assert.Equal(t, sbi.RealTime, *cyclicComp.Workload.Scheduling)
		assert.Equal(t, 95, *cyclicComp.Workload.Priority)
		require.NotNil(t, cyclicComp.Workload.Deadline)
		assert.Equal(t, 10, *cyclicComp.Workload.Deadline.Deadline)
		assert.Equal(t, 25, *cyclicComp.Workload.Deadline.Period)
		assert.Equal(t, 5, *cyclicComp.Workload.Deadline.Runtime)

		catComp := composeDeploymentManifest.Spec.DeploymentProfile.Components[2]
		catCaps := extractComponentCapacityRequirements(catComp)
		require.NotNil(t, catCaps, "caterpillar_compose capacity requirements must be extracted")
		require.NotNil(t, catCaps.Cache)
		assert.Equal(t, sbi.DeploymentCacheRequirementLevelL3, (*catCaps.Cache)[0].Level)
		assert.Equal(t, sbi.DeploymentCacheRequirementAllocationExclusive, (*catCaps.Cache)[0].Allocation)
	})
}

func convertToSbiManifest(t *testing.T, m sbi.AppDeploymentManifest) sbi.AppDeploymentManifest {
	t.Helper()
	data, err := json.Marshal(m)
	require.NoError(t, err)

	jsonBytes, err := powerfulYaml.YAMLToJSON(data)
	require.NoError(t, err)

	var res sbi.AppDeploymentManifest
	require.NoError(t, powerfulYaml.Unmarshal(jsonBytes, &res))
	return res
}
