package margo

import (
	"context"
	"fmt"
	"os"
	"testing"

	powerfulYaml "github.com/ghodss/yaml"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/margo/sandbox/shared-lib/archive"
	"github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleSerializationAndExtraction(t *testing.T) {
	appPkg := setupTestAppPackage(t)
	ctx := context.Background()

	deployMgr, bundleMgr, db := setupTestMargoEnvironment(t, "bundle-test")

	deviceId := "helm-device-bundle-01"
	helmDev := buildEligibilityDevice(deviceId, sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm)
	require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
		DeviceClientId:   deviceId,
		DeviceId:         deviceId,
		OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
		Capabilities:     helmDev,
	}))

	helmReq, err := buildDeploymentRequestForDevice("rt-bundle-helm-deploy", appPkg, helmDev)
	require.NoError(t, err)

	createdDeployment, err := deployMgr.CreateDeployment(ctx, helmReq, appPkg)
	require.NoError(t, err)
	require.NotNil(t, createdDeployment)

	// Rebuild bundle synchronously
	err = bundleMgr.rebuildTheBundleForDevice(ctx, deviceId)
	require.NoError(t, err, "rebuildTheBundleForDevice should succeed")

	bundleRow, err := db.GetDeploymentBundle(ctx, deviceId)
	require.NoError(t, err)
	require.NotNil(t, bundleRow)
	require.NotEmpty(t, bundleRow.ArchivePath)
	require.NotNil(t, bundleRow.Manifest.Bundle)
	require.NotNil(t, bundleRow.Manifest.Bundle.Digest)

	// Register immediate cleanup of archive file in /tmp
	t.Cleanup(func() {
		_ = os.Remove(bundleRow.ArchivePath)
	})

	expectedDigest := *bundleRow.Manifest.Bundle.Digest
	require.NotEmpty(t, expectedDigest)

	// Read generated .tar.gz bundle bytes
	bundleBytes, err := os.ReadFile(bundleRow.ArchivePath)
	require.NoError(t, err)

	// Extract and verify digest using Sandbox's archive.NewExtractor
	extractor := archive.NewExtractor(bundleBytes)
	require.NoError(t, extractor.VerifyBundleDigest(expectedDigest),
		"bundle archive SHA256 digest must match manifest digest via archive.Extractor.VerifyBundleDigest")

	entries, err := extractor.Extract()
	require.NoError(t, err, "archive.NewExtractor must unpack .tar.gz without error")
	require.NotEmpty(t, entries)

	expectedFileName := fmt.Sprintf("%s.yaml", *createdDeployment.Id)
	yamlData, exists := entries[expectedFileName]
	require.True(t, exists, "archive must contain %s", expectedFileName)

	// Convert YAML to JSON directly using powerfulYaml
	jsonBytes, err := powerfulYaml.YAMLToJSON(yamlData)
	require.NoError(t, err, "powerfulYaml.YAMLToJSON must succeed")

	var sandboxDeployment sbi.AppDeploymentManifest
	err = powerfulYaml.Unmarshal(jsonBytes, &sandboxDeployment)
	require.NoError(t, err, "unmarshaling into sbi.AppDeploymentManifest must succeed")

	assert.Equal(t, "rt-bundle-helm-deploy", sandboxDeployment.Metadata.Name)
	assert.Equal(t, sbi.DeviceIdWithAsterisk(deviceId), sandboxDeployment.Metadata.DeviceId)

	// Verify profile-level memory & storage constraints
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints)
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory)
	assert.Equal(t, "1024Mi", sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory.Size)
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Storage)
	assert.Equal(t, "2Gi", *sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Storage)

	// Find all 3 components
	var extractedCat, extractedCyclic, extractedStress *sbi.ApplicationDeploymentProfileComponent
	for i := range sandboxDeployment.Spec.DeploymentProfile.Components {
		comp := &sandboxDeployment.Spec.DeploymentProfile.Components[i]
		switch comp.Name {
		case "caterpillar":
			extractedCat = comp
		case "cyclictest":
			extractedCyclic = comp
		case "stressng":
			extractedStress = comp
		}
	}

	require.NotNil(t, extractedCat, "caterpillar component must exist in extracted manifest")
	require.NotNil(t, extractedCat.DeviceConstraints)
	require.NotNil(t, extractedCat.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, extractedCat.DeviceConstraints.CapacityRequirements.Cpu)
	require.Len(t, *extractedCat.DeviceConstraints.CapacityRequirements.Cpu, 1)
	catCPU := (*extractedCat.DeviceConstraints.CapacityRequirements.Cpu)[0]
	assert.Equal(t, float32(1), catCPU.Cores)
	require.NotNil(t, catCPU.Type)
	assert.Equal(t, sbi.DeploymentCpuRequirementTypeIsolated, *catCPU.Type)

	require.NotNil(t, extractedCat.DeviceConstraints.CapacityRequirements.Cache)
	require.Len(t, *extractedCat.DeviceConstraints.CapacityRequirements.Cache, 1)
	catCache := (*extractedCat.DeviceConstraints.CapacityRequirements.Cache)[0]
	assert.Equal(t, sbi.DeploymentCacheRequirementLevelL3, catCache.Level)
	assert.Equal(t, sbi.DeploymentCacheRequirementAllocationExclusive, catCache.Allocation)
	require.NotNil(t, catCache.Size)
	assert.Equal(t, "9216Ki", *catCache.Size)

	// Verify caterpillar eligibilityRules survived into the bundle
	require.NotNil(t, extractedCat.DeviceConstraints.EligibilityRules, "caterpillar eligibilityRules must survive into extracted bundle")
	require.Len(t, *extractedCat.DeviceConstraints.EligibilityRules, 1)
	require.NotNil(t, (*extractedCat.DeviceConstraints.EligibilityRules)[0].PropertySelector)
	assert.Len(t, (*extractedCat.DeviceConstraints.EligibilityRules)[0].PropertySelector.MatchExpressions, 2)

	require.NotNil(t, extractedCyclic, "cyclictest component must exist in extracted manifest")
	require.NotNil(t, extractedCyclic.DeviceConstraints)
	require.NotNil(t, extractedCyclic.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, extractedCyclic.DeviceConstraints.CapacityRequirements.Cpu)
	require.Len(t, *extractedCyclic.DeviceConstraints.CapacityRequirements.Cpu, 1)
	assert.Equal(t, float32(1), (*extractedCyclic.DeviceConstraints.CapacityRequirements.Cpu)[0].Cores)
	require.NotNil(t, extractedCyclic.DeviceConstraints.EligibilityRules, "cyclictest eligibilityRules must survive into extracted bundle")

	require.NotNil(t, extractedCyclic.Workload)
	require.NotNil(t, extractedCyclic.Workload.Scheduling)
	assert.Equal(t, sbi.RealTime, *extractedCyclic.Workload.Scheduling)
	require.NotNil(t, extractedCyclic.Workload.Priority)
	assert.Equal(t, 95, *extractedCyclic.Workload.Priority)
	require.NotNil(t, extractedCyclic.Workload.Nice)
	assert.Equal(t, 10, *extractedCyclic.Workload.Nice)
	require.NotNil(t, extractedCyclic.Workload.Deadline)
	require.NotNil(t, extractedCyclic.Workload.Deadline.Deadline)
	assert.Equal(t, 10, *extractedCyclic.Workload.Deadline.Deadline)
	require.NotNil(t, extractedCyclic.Workload.Deadline.Period)
	assert.Equal(t, 16, *extractedCyclic.Workload.Deadline.Period)
	require.NotNil(t, extractedCyclic.Workload.Deadline.Runtime)
	assert.Equal(t, 5, *extractedCyclic.Workload.Deadline.Runtime)

	require.NotNil(t, extractedStress, "stressng component must exist in extracted manifest")
	assert.Equal(t, "oci://harbor.machine:8443/library/stressng", extractedStress.Properties.Repository)
	assert.Nil(t, extractedStress.DeviceConstraints, "stressng component should not have deviceConstraints")
	assert.Nil(t, extractedStress.Workload, "stressng component should not have RT workload")
}
