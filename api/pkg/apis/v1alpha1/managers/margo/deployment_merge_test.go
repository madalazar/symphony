package margo

import (
	"context"
	"testing"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentMerge_HelmAndCompose(t *testing.T) {
	appPkg := setupTestAppPackage(t)
	deployMgr := setupTestDeploymentManager(t)
	ctx := context.Background()

	testCases := []struct {
		name              string
		deploymentName    string
		deviceID          string
		profileType       margoNonStdAPI.DeploymentExecutionProfileType
		componentNames    []string
		catIndex          int
		catName           string
		wantCatRepo       string
		cyclicIndex       int
		cyclicName        string
		wantCyclicPeriod  int
		wantProfileMemory string
		wantProfileStore  string
	}{
		{
			name:              "2.4.A Helm Device Deployment",
			deploymentName:    "rt-helm-deployment-01",
			deviceID:          "helm-device-01",
			profileType:       margoNonStdAPI.DeploymentExecutionProfileType("helm"),
			componentNames:    []string{"caterpillar", "cyclictest", "stressng"},
			catIndex:          0,
			catName:           "caterpillar",
			wantCatRepo:       "oci://harbor.machine:8443/library/caterpillar",
			cyclicIndex:       1,
			cyclicName:        "cyclictest",
			wantCyclicPeriod:  16,
			wantProfileMemory: "1024Mi",
			wantProfileStore:  "2Gi",
		},
		{
			name:              "2.4.B Compose Device Deployment",
			deploymentName:    "rt-compose-deployment-01",
			deviceID:          "compose-device-01",
			profileType:       margoNonStdAPI.DeploymentExecutionProfileType("compose"),
			componentNames:    []string{"cyclictest_compose", "stressng_compose", "caterpillar_compose"},
			catIndex:          2,
			catName:           "caterpillar_compose",
			wantCatRepo:       "oci://harbor.machine:8443/library/caterpillar-compose-archive",
			cyclicIndex:       0,
			cyclicName:        "cyclictest_compose",
			wantCyclicPeriod:  25,
			wantProfileMemory: "1024Mi",
			wantProfileStore:  "1Gi",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := buildSkeletonDeploymentRequest(
				tc.deploymentName,
				"rt-benchmark-experiment-suite",
				tc.deviceID,
				tc.profileType,
				tc.componentNames,
			)
			require.NoError(t, err)

			resp, err := deployMgr.CreateDeployment(ctx, req, appPkg)
			require.NoError(t, err, "CreateDeployment for %s profile must succeed", tc.profileType)
			require.NotNil(t, resp)
			t.Cleanup(func() {
				if resp != nil && resp.Id != nil {
					_, _ = deployMgr.DeleteDeployment(ctx, *resp.Id)
				}
			})

			// Assert profile-level memory & storage constraints survived
			require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints)
			require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements)
			require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory)
			assert.Equal(t, tc.wantProfileMemory, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory.Size)
			require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Storage)
			assert.Equal(t, tc.wantProfileStore, *resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Storage)

			// Assert caterpillar component has CPU isolated, L3 cache exclusive, and eligibilityRules
			mergedCat, err := resp.Spec.DeploymentProfile.Components[tc.catIndex].AsDeploymentProfileComponent()
			require.NoError(t, err)
			assert.Equal(t, tc.catName, mergedCat.Name)
			assert.Equal(t, tc.wantCatRepo, mergedCat.Properties.Repository)
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
			require.NotNil(t, mergedCat.DeviceConstraints.EligibilityRules)
			require.Len(t, *mergedCat.DeviceConstraints.EligibilityRules, 1)

			// Assert cyclictest component has RT workload scheduling attributes
			mergedCyclic, err := resp.Spec.DeploymentProfile.Components[tc.cyclicIndex].AsDeploymentProfileComponent()
			require.NoError(t, err)
			assert.Equal(t, tc.cyclicName, mergedCyclic.Name)
			require.NotNil(t, mergedCyclic.Workload)
			require.NotNil(t, mergedCyclic.Workload.Scheduling)
			assert.Equal(t, margoNonStdAPI.RealTime, *mergedCyclic.Workload.Scheduling)
			assert.Equal(t, 95, *mergedCyclic.Workload.Priority)
			assert.Equal(t, 10, *mergedCyclic.Workload.Nice)
			require.NotNil(t, mergedCyclic.Workload.Deadline)
			assert.Equal(t, 10, *mergedCyclic.Workload.Deadline.Deadline)
			assert.Equal(t, tc.wantCyclicPeriod, *mergedCyclic.Workload.Deadline.Period)
			assert.Equal(t, 5, *mergedCyclic.Workload.Deadline.Runtime)
		})
	}

	t.Run("2.4.C Partial CapacityRequirements Override Preserves Package Storage And Parameter Overrides", func(t *testing.T) {
		req, err := buildSkeletonDeploymentRequest(
			"rt-helm-override-deployment",
			"rt-benchmark-experiment-suite",
			"helm-device-01",
			margoNonStdAPI.DeploymentExecutionProfileType("helm"),
			[]string{"caterpillar", "cyclictest", "stressng"},
		)
		require.NoError(t, err)

		// Override only Memory at profile level; Storage ("2Gi") from package must be preserved via deep merge
		req.Spec.DeploymentProfile.DeviceConstraints = &margoNonStdAPI.DeviceConstraints{
			CapacityRequirements: &margoNonStdAPI.CapacityRequirements{
				Memory: &margoNonStdAPI.DeploymentMemoryRequirement{
					Size: "2048Mi",
				},
			},
		}
		params := margoNonStdAPI.DeploymentParameters{
			"cyclictestThreads": margoNonStdAPI.DeploymentParameterValue{
				Value: 2,
				Targets: []margoNonStdAPI.DeploymentParameterTarget{
					{
						Pointer:    "cyclictest.threads",
						Components: []string{"cyclictest"},
					},
				},
			},
		}
		req.Spec.Parameters = &params

		resp, err := deployMgr.CreateDeployment(ctx, req, appPkg)
		require.NoError(t, err)
		require.NotNil(t, resp)
		t.Cleanup(func() {
			if resp != nil && resp.Id != nil {
				_, _ = deployMgr.DeleteDeployment(ctx, *resp.Id)
			}
		})

		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory)
		assert.Equal(t, "2048Mi", resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Memory.Size)
		require.NotNil(t, resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Storage,
			"package storage requirement must be preserved when request overrides only memory")
		assert.Equal(t, "2Gi", *resp.Spec.DeploymentProfile.DeviceConstraints.CapacityRequirements.Storage)

		// Verify parameter override and non-overridden package default parameters are merged and preserved
		require.NotNil(t, resp.Spec.Parameters, "merged parameters must be present in deployment response")
		overriddenParam, exists := (*resp.Spec.Parameters)["cyclictestThreads"]
		require.True(t, exists, "overridden cyclictestThreads parameter must be present")
		assert.EqualValues(t, 2, overriddenParam.Value, "cyclictestThreads parameter value must equal overridden scalar value 2")
		require.Len(t, overriddenParam.Targets, 1)
		assert.Equal(t, "cyclictest.threads", overriddenParam.Targets[0].Pointer)

		defaultPriority, exists := (*resp.Spec.Parameters)["cyclictestPriority"]
		require.True(t, exists, "non-overridden package parameter cyclictestPriority must be preserved from appPkg")
		assert.EqualValues(t, 99, defaultPriority.Value)
		require.Len(t, defaultPriority.Targets, 1, "targets should be filtered to the helm profile component")
		assert.Equal(t, "cyclictest.priority", defaultPriority.Targets[0].Pointer)

		// Verify appPkg.Description.Parameters was not mutated in-place
		origThreads := (*appPkg.Description.Parameters)["cyclictestThreads"]
		assert.EqualValues(t, 1, origThreads.Value, "appPkg.Description.Parameters must not be mutated in-place by MergeWithAppPackage")

		// Verify unknown parameter override is rejected
		badReq, err := buildSkeletonDeploymentRequest(
			"rt-helm-bad-param",
			"rt-benchmark-experiment-suite",
			"helm-device-01",
			margoNonStdAPI.DeploymentExecutionProfileType("helm"),
			[]string{"caterpillar", "cyclictest", "stressng"},
		)
		require.NoError(t, err)
		badParams := margoNonStdAPI.DeploymentParameters{
			"nonExistentParameter": margoNonStdAPI.DeploymentParameterValue{
				Value: "invalid",
			},
		}
		badReq.Spec.Parameters = &badParams

		_, err = deployMgr.CreateDeployment(ctx, badReq, appPkg)
		require.Error(t, err, "unknown parameter override must fail MergeWithAppPackage")
	})
}
