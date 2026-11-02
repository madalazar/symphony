package margo

import (
	"context"
	"testing"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/margo/sandbox/shared-lib/constraints"
	"github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceEligibility_HelmAndCompose(t *testing.T) {
	appPkg := setupTestAppPackage(t)
	ctx := context.Background()

	deployMgr, _, db := setupTestMargoEnvironment(t, "eligibility-test")

	helmTargetDevice := buildEligibilityDevice("helm-target-device", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm)
	composeTargetDevice := buildEligibilityDevice("compose-target-device", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose)

	require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
		DeviceClientId:   "helm-target-device",
		DeviceId:         "helm-target-device",
		OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
		Capabilities:     helmTargetDevice,
	}))
	require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
		DeviceClientId:   "compose-target-device",
		DeviceId:         "compose-target-device",
		OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
		Capabilities:     composeTargetDevice,
	}))

	// 1. Create Helm deployment
	helmReq, err := buildDeploymentRequestForDevice("rt-benchmark-helm-instance", appPkg, helmTargetDevice)
	require.NoError(t, err)
	helmResp, err := deployMgr.CreateDeployment(ctx, helmReq, appPkg)
	require.NoError(t, err)

	// 2. Create Compose deployment
	composeReq, err := buildDeploymentRequestForDevice("rt-benchmark-compose-instance", appPkg, composeTargetDevice)
	require.NoError(t, err)
	composeResp, err := deployMgr.CreateDeployment(ctx, composeReq, appPkg)
	require.NoError(t, err)

	// 3. Obtain the stored desired states (already sbi.AppDeploymentManifest)
	helmRow, err := db.GetDeployment(ctx, *helmResp.Id)
	require.NoError(t, err)
	composeRow, err := db.GetDeployment(ctx, *composeResp.Id)
	require.NoError(t, err)

	helmDeploymentManifest := helmRow.DesiredState.AppDeploymentManifest
	composeDeploymentManifest := composeRow.DesiredState.AppDeploymentManifest

	// Build test devices for eligibility matrix evaluation
	helmDevice := buildEligibilityDevice("helm-device-01", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm)
	composeDevice := buildEligibilityDevice("compose-device-01", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose)

	engine := constraints.New()

	// --- 2.6 Eligibility Matrix Evaluation ---

	t.Run("2.6.A Helm Profile on Helm Device -> Eligible", func(t *testing.T) {
		catComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[0]
		eligible, reason, err := engine.IsDeviceEligible(helmDevice, catComp.DeviceConstraints)
		require.NoError(t, err)
		assert.True(t, eligible, "caterpillar component must be eligible on helm device, reason: %s", reason)

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

	// --- Component Constraint Extraction Validation (using shared-lib constraints.ExtractComponentCapacityRequirements) ---

	t.Run("2.6.E Component Capacity Extraction (Helm Caterpillar & Cyclictest)", func(t *testing.T) {
		catComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[0]
		catCaps := constraints.ExtractComponentCapacityRequirements(catComp)
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

		cyclicComp := helmDeploymentManifest.Spec.DeploymentProfile.Components[1]
		cyclicCaps := constraints.ExtractComponentCapacityRequirements(cyclicComp)
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
		catCaps := constraints.ExtractComponentCapacityRequirements(catComp)
		require.NotNil(t, catCaps, "caterpillar_compose capacity requirements must be extracted")
		require.NotNil(t, catCaps.Cache)
		assert.Equal(t, sbi.DeploymentCacheRequirementLevelL3, (*catCaps.Cache)[0].Level)
		assert.Equal(t, sbi.DeploymentCacheRequirementAllocationExclusive, (*catCaps.Cache)[0].Allocation)
	})
}
