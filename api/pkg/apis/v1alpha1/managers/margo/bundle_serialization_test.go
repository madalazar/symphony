package margo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	powerfulYaml "github.com/ghodss/yaml"
	coa_ctx "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states/memorystate"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/margo/sandbox/shared-lib/archive"
	p "github.com/margo/sandbox/shared-lib/pointers"
	"github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleSerializationAndExtraction(t *testing.T) {
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

	db := NewMargoDatabase(mContext, "bundle-test", stateProvider)
	stateMachine := NewDeploymentStateMachine(db, deploymentLogger)
	transformer := NewMargoTransformer()

	bundleMgr := &DeploymentBundleManager{
		Database:      db,
		StateProvider: stateProvider,
	}

	// Set bundleManager to nil on deployMgr to avoid asynchronous background generation clashing
	deployMgr := &DeploymentManager{
		database:      db,
		stateMachine:  stateMachine,
		tranformer:    transformer,
		bundleManager: nil,
	}

	deviceId := "helm-device-bundle-01"

	helmReq := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
	helmReq.Metadata.Name = "rt-bundle-helm-deploy"
	helmReq.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
		AppPackageRef: struct {
			Id string `json:"id"`
		}{
			Id: "rt-benchmark-experiment-suite",
		},
		DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
			Id: p.Ptr(deviceId),
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

	catComp := margoNonStdAPI.DeploymentProfileComponent{Name: "caterpillar"}
	require.NoError(t, helmReq.Spec.DeploymentProfile.Components[0].FromDeploymentProfileComponent(catComp))
	cyclicComp := margoNonStdAPI.DeploymentProfileComponent{Name: "cyclictest"}
	require.NoError(t, helmReq.Spec.DeploymentProfile.Components[1].FromDeploymentProfileComponent(cyclicComp))
	stressComp := margoNonStdAPI.DeploymentProfileComponent{Name: "stressng"}
	require.NoError(t, helmReq.Spec.DeploymentProfile.Components[2].FromDeploymentProfileComponent(stressComp))

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

	// Verify SHA256 matches
	hash := sha256.Sum256(bundleBytes)
	actualDigest := fmt.Sprintf("sha256:%x", hash)
	assert.Equal(t, expectedDigest, actualDigest, "bundle archive SHA256 digest must match manifest digest")

	// Extract using Sandbox's archive.NewExtractor
	extractor := archive.NewExtractor(bundleBytes)
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

	// Verify profile-level memory struct
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints)
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory)
	assert.Equal(t, "1024Mi", sandboxDeployment.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory.Size)

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

	require.NotNil(t, extractedCyclic, "cyclictest component must exist in extracted manifest")
	require.NotNil(t, extractedCyclic.DeviceConstraints)
	require.NotNil(t, extractedCyclic.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, extractedCyclic.DeviceConstraints.CapacityRequirements.Cpu)
	require.Len(t, *extractedCyclic.DeviceConstraints.CapacityRequirements.Cpu, 1)
	assert.Equal(t, float32(1), (*extractedCyclic.DeviceConstraints.CapacityRequirements.Cpu)[0].Cores)

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
}
