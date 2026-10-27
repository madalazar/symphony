package margo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	coa_ctx "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states/memorystate"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	p "github.com/margo/sandbox/shared-lib/pointers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestAppPackage(t *testing.T) ApplicationPackage {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "margo-rt-deploy-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	margoPath := filepath.Join(tempDir, "margo.yaml")
	err = os.WriteFile(margoPath, []byte(rtBenchmarkMargoYAML), 0644)
	require.NoError(t, err)

	appPkgMgr := &AppPkgManager{}
	appDesc, resources, err := appPkgMgr.parseApplicationDescription(tempDir)
	require.NoError(t, err)
	require.NotNil(t, appDesc)

	pkgId := "rt-benchmark-experiment-suite"
	return ApplicationPackage{
		Package: margoNonStdAPI.ApplicationPackageManifestResp{
			Id: &pkgId,
			Metadata: margoNonStdAPI.Metadata{
				Name: "RT Benchmark Experiment Suite",
			},
			Spec: margoNonStdAPI.ApplicationPackageSpec{
				SourceType: margoNonStdAPI.OCIREPO,
			},
		},
		Description: appDesc,
		Resources:   resources,
	}
}

func setupTestDeploymentManager(t *testing.T) *DeploymentManager {
	t.Helper()

	stateProvider := &memorystate.MemoryStateProvider{}
	err := stateProvider.Init(memorystate.MemoryStateProviderConfig{})
	require.NoError(t, err)

	vContext := &coa_ctx.VendorContext{}
	err = vContext.Init(nil)
	require.NoError(t, err)

	mContext := &coa_ctx.ManagerContext{}
	err = mContext.Init(vContext, nil)
	require.NoError(t, err)

	db := NewMargoDatabase(mContext, "deployment-manager", stateProvider)
	stateMachine := NewDeploymentStateMachine(db, deploymentLogger)
	transformer := NewMargoTransformer()

	return &DeploymentManager{
		database:      db,
		stateMachine:  stateMachine,
		tranformer:    transformer,
		bundleManager: nil, // set nil to avoid uncoordinated background goroutines in tests
	}
}

func TestDeploymentMerge_HelmAndCompose(t *testing.T) {
	appPkg := setupTestAppPackage(t)
	deployMgr := setupTestDeploymentManager(t)
	ctx := context.Background()

	t.Run("2.4.A Helm Device Deployment", func(t *testing.T) {
		helmReq := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
		helmReq.Metadata.Name = "rt-helm-deployment-01"
		helmReq.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
			AppPackageRef: struct {
				Id string `json:"id"`
			}{
				Id: "rt-benchmark-experiment-suite",
			},
			DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
				Id: p.Ptr("helm-device-01"),
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

		// Fill components with minimal requested skeleton
		catComp := margoNonStdAPI.DeploymentProfileComponent{Name: "caterpillar"}
		require.NoError(t, helmReq.Spec.DeploymentProfile.Components[0].FromDeploymentProfileComponent(catComp))
		cyclicComp := margoNonStdAPI.DeploymentProfileComponent{Name: "cyclictest"}
		require.NoError(t, helmReq.Spec.DeploymentProfile.Components[1].FromDeploymentProfileComponent(cyclicComp))
		stressComp := margoNonStdAPI.DeploymentProfileComponent{Name: "stressng"}
		require.NoError(t, helmReq.Spec.DeploymentProfile.Components[2].FromDeploymentProfileComponent(stressComp))

		resp, err := deployMgr.CreateDeployment(ctx, helmReq, appPkg)
		require.NoError(t, err, "CreateDeployment for helm profile must succeed")
		require.NotNil(t, resp)

		// Assert profile-level memory constraints survived
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory)
		assert.Equal(t, "1024Mi", resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory.Size)

		// Assert caterpillar component has CPU isolated and L3 cache exclusive
		mergedCat, err := resp.Spec.DeploymentProfile.Components[0].AsDeploymentProfileComponent()
		require.NoError(t, err)
		assert.Equal(t, "caterpillar", mergedCat.Name)
		assert.Equal(t, "oci://harbor.machine:8443/library/caterpillar", mergedCat.Properties.Repository)
		require.NotNil(t, mergedCat.DeviceConstraints)
		require.NotNil(t, mergedCat.DeviceConstraints.CapacityRequirements)
		require.NotNil(t, mergedCat.DeviceConstraints.CapacityRequirements.Cpu)
		require.Len(t, *mergedCat.DeviceConstraints.CapacityRequirements.Cpu, 1)
		catCPU := (*mergedCat.DeviceConstraints.CapacityRequirements.Cpu)[0]
		assert.Equal(t, float32(1), catCPU.Cores)
		assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementType("isolated"), *catCPU.Type)

		require.NotNil(t, mergedCat.DeviceConstraints.CapacityRequirements.Cache)
		require.Len(t, *mergedCat.DeviceConstraints.CapacityRequirements.Cache, 1)
		catCache := (*mergedCat.DeviceConstraints.CapacityRequirements.Cache)[0]
		assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementLevel("L3"), catCache.Level)
		assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementAllocation("exclusive"), catCache.Allocation)
		assert.Equal(t, "9216Ki", *catCache.Size)

		// Assert cyclictest component has RT workload scheduling attributes
		mergedCyclic, err := resp.Spec.DeploymentProfile.Components[1].AsDeploymentProfileComponent()
		require.NoError(t, err)
		assert.Equal(t, "cyclictest", mergedCyclic.Name)
		require.NotNil(t, mergedCyclic.Workload)
		require.NotNil(t, mergedCyclic.Workload.Scheduling)
		assert.Equal(t, margoNonStdAPI.RealTime, *mergedCyclic.Workload.Scheduling)
		assert.Equal(t, 95, *mergedCyclic.Workload.Priority)
		assert.Equal(t, 10, *mergedCyclic.Workload.Nice)
		require.NotNil(t, mergedCyclic.Workload.Deadline)
		assert.Equal(t, 10, *mergedCyclic.Workload.Deadline.Deadline)
		assert.Equal(t, 16, *mergedCyclic.Workload.Deadline.Period)
		assert.Equal(t, 5, *mergedCyclic.Workload.Deadline.Runtime)
	})

	t.Run("2.4.B Compose Device Deployment", func(t *testing.T) {
		composeReq := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
		composeReq.Metadata.Name = "rt-compose-deployment-01"
		composeReq.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
			AppPackageRef: struct {
				Id string `json:"id"`
			}{
				Id: "rt-benchmark-experiment-suite",
			},
			DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
				Id: p.Ptr("compose-device-01"),
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

		cyclicComp := margoNonStdAPI.DeploymentProfileComponent{Name: "cyclictest_compose"}
		require.NoError(t, composeReq.Spec.DeploymentProfile.Components[0].FromDeploymentProfileComponent(cyclicComp))
		stressComp := margoNonStdAPI.DeploymentProfileComponent{Name: "stressng_compose"}
		require.NoError(t, composeReq.Spec.DeploymentProfile.Components[1].FromDeploymentProfileComponent(stressComp))
		catComp := margoNonStdAPI.DeploymentProfileComponent{Name: "caterpillar_compose"}
		require.NoError(t, composeReq.Spec.DeploymentProfile.Components[2].FromDeploymentProfileComponent(catComp))

		resp, err := deployMgr.CreateDeployment(ctx, composeReq, appPkg)
		require.NoError(t, err, "CreateDeployment for compose profile must succeed")
		require.NotNil(t, resp)

		// Assert profile-level memory constraints
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory)
		assert.Equal(t, "1024Mi", resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory.Size)

		// Assert cyclictest_compose component has RT workload scheduling attributes
		mergedCyclic, err := resp.Spec.DeploymentProfile.Components[0].AsDeploymentProfileComponent()
		require.NoError(t, err)
		assert.Equal(t, "cyclictest_compose", mergedCyclic.Name)
		require.NotNil(t, mergedCyclic.Workload)
		assert.Equal(t, margoNonStdAPI.RealTime, *mergedCyclic.Workload.Scheduling)
		assert.Equal(t, 95, *mergedCyclic.Workload.Priority)
		require.NotNil(t, mergedCyclic.Workload.Deadline)
		assert.Equal(t, 10, *mergedCyclic.Workload.Deadline.Deadline)
		assert.Equal(t, 25, *mergedCyclic.Workload.Deadline.Period)
		assert.Equal(t, 5, *mergedCyclic.Workload.Deadline.Runtime)

		// Assert caterpillar_compose component has L3 exclusive cache and isolated CPU
		mergedCat, err := resp.Spec.DeploymentProfile.Components[2].AsDeploymentProfileComponent()
		require.NoError(t, err)
		assert.Equal(t, "caterpillar_compose", mergedCat.Name)
		require.NotNil(t, mergedCat.DeviceConstraints)
		require.NotNil(t, mergedCat.DeviceConstraints.CapacityRequirements)

		require.NotNil(t, mergedCat.DeviceConstraints.CapacityRequirements.Cpu)
		require.Len(t, *mergedCat.DeviceConstraints.CapacityRequirements.Cpu, 1)
		catCPU := (*mergedCat.DeviceConstraints.CapacityRequirements.Cpu)[0]
		assert.Equal(t, float32(1), catCPU.Cores)
		assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementType("isolated"), *catCPU.Type)

		require.NotNil(t, mergedCat.DeviceConstraints.CapacityRequirements.Cache)
		require.Len(t, *mergedCat.DeviceConstraints.CapacityRequirements.Cache, 1)
		catCache := (*mergedCat.DeviceConstraints.CapacityRequirements.Cache)[0]
		assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementLevel("L3"), catCache.Level)
		assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementAllocation("exclusive"), catCache.Allocation)
		assert.Equal(t, "9216Ki", *catCache.Size)
	})
}
