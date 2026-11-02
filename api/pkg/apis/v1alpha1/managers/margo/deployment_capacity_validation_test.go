package margo

import (
	"context"
	"path/filepath"
	"testing"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateDeployment_BenchmarkTestPackagesValidation(t *testing.T) {
	ctx := context.Background()

	icorePkg := parseTestAppPackageFromFile(t, filepath.Join("testdata", "rt-benchmark-icore-margo.yaml"))
	iecachePkg := parseTestAppPackageFromFile(t, filepath.Join("testdata", "rt-benchmark-iecache-margo.yaml"))
	iscachePkg := parseTestAppPackageFromFile(t, filepath.Join("testdata", "rt-benchmark-iscache-margo.yaml"))

	t.Run("2.7.A CLI profile-type matching (cli-yaml-gen.sh): helm-only packages match k3s device and are rejected for compose-only device", func(t *testing.T) {
		k3sDev := parseDeviceCapabilitiesFromFile(t, "k3s-device-capabilities.json")
		composeDev := parseDeviceCapabilitiesFromFile(t, "compose-device-capabilities.json")

		for _, tc := range []struct {
			pkg      ApplicationPackage
			fileName string
		}{
			{icorePkg, "rt-benchmark-icore-margo.yaml"},
			{iecachePkg, "rt-benchmark-iecache-margo.yaml"},
			{iscachePkg, "rt-benchmark-iscache-margo.yaml"},
		} {
			pkgPath := filepath.Join("testdata", tc.fileName)
			req, err := generateDeploymentRequestViaCLI(t, *tc.pkg.Description.Id, *tc.pkg.Description.Id, k3sDev.Properties.Id, "helm", pkgPath)
			require.NoError(t, err, "package %s (helm profile) must match k3s device (supportedDeploymentTypes: [helm]) via cli-yaml-gen.sh", *tc.pkg.Description.Id)
			assert.Equal(t, margoNonStdAPI.DeploymentExecutionProfileType("helm"), req.Spec.DeploymentProfile.Type)

			_, err = generateDeploymentRequestViaCLI(t, *tc.pkg.Description.Id, *tc.pkg.Description.Id, composeDev.Properties.Id, "compose", pkgPath)
			require.Error(t, err, "package %s (helm-only profile) must NOT match compose-only device in cli-yaml-gen.sh", *tc.pkg.Description.Id)
		}
	})

	negativeCases := []struct {
		name             string
		deploymentName   string
		pkg              ApplicationPackage
		wantErrSubstring string
	}{
		{
			name:             "2.7.B rt-benchmark-icore-app fails isolated CPU validation on its target k3s device",
			deploymentName:   "rt-icore-deploy",
			pkg:              icorePkg,
			wantErrSubstring: "insufficient isolated cpu cores: needs 11.0, device has 2.0 total, 0.0 already allocated, 2.0 remaining",
		},
		{
			name:             "2.7.C rt-benchmark-iecache-app fails exclusive L3 cache validation on its target k3s device",
			deploymentName:   "rt-iecache-deploy",
			pkg:              iecachePkg,
			wantErrSubstring: "exclusive cache requested 240500000 bytes exceeds available exclusive l3 cache 63438848 bytes (total 63438848, already allocated 0)",
		},
		{
			name:             "2.7.D rt-benchmark-iscache-app fails shared L3 cache validation on its target k3s device",
			deploymentName:   "rt-iscache-deploy",
			pkg:              iscachePkg,
			wantErrSubstring: "shared cache requested 100000000 bytes exceeds total available l3 cache 63438848 bytes",
		},
	}

	for _, tc := range negativeCases {
		t.Run(tc.name, func(t *testing.T) {
			deployMgr, _, db := setupTestMargoEnvironment(t, "deployment-manager")
			k3sDev := parseDeviceCapabilitiesFromFile(t, "k3s-device-capabilities.json")
			deviceID := k3sDev.Properties.Id

			require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
				DeviceClientId:   deviceID,
				DeviceId:         deviceID,
				OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
				Capabilities:     k3sDev,
			}))

			req, err := buildDeploymentRequestForDevice(tc.deploymentName, tc.pkg, k3sDev)
			require.NoError(t, err)

			resp, err := deployMgr.CreateDeployment(ctx, req, tc.pkg)
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.Contains(t, err.Error(), "resource matching failed for device "+deviceID)
			assert.Contains(t, err.Error(), tc.wantErrSubstring)

			deployments, listErr := db.GetDeploymentsByDevice(ctx, deviceID)
			require.NoError(t, listErr)
			assert.Empty(t, deployments, "rejected deployment must not be persisted in database")
		})
	}
}

func TestCreateDeployment_RTBenchmarkExperimentApp_DeployOnceSucceedsSecondFails(t *testing.T) {
	ctx := context.Background()

	// Load from testdata/rt-benchmark-experiment-margo.yaml (and verify real workspace artefact exists)
	experimentFixturePath := filepath.Join("testdata", "rt-benchmark-experiment-margo.yaml")
	experimentAppPkg := parseTestAppPackageFromFile(t, experimentFixturePath)

	lifecycleCases := []struct {
		name           string
		deviceJSONFile string
		wantProfile    margoNonStdAPI.DeploymentExecutionProfileType
		deploy1Name    string
		deploy2Name    string
	}{
		{
			name:           "2.7.E K3s device: first Helm deployment of rt-benchmark-experiment-app succeeds, second deployment fails validation",
			deviceJSONFile: "k3s-device-capabilities.json",
			wantProfile:    margoNonStdAPI.DeploymentExecutionProfileType("helm"),
			deploy1Name:    "rt-experiment-helm-1",
			deploy2Name:    "rt-experiment-helm-2",
		},
		{
			name:           "2.7.F Compose device: first Compose deployment of rt-benchmark-experiment-app succeeds, second deployment fails validation",
			deviceJSONFile: "compose-device-capabilities.json",
			wantProfile:    margoNonStdAPI.DeploymentExecutionProfileType("compose"),
			deploy1Name:    "rt-experiment-compose-1",
			deploy2Name:    "rt-experiment-compose-2",
		},
	}

	for _, tc := range lifecycleCases {
		t.Run(tc.name, func(t *testing.T) {
			deployMgr, _, db := setupTestMargoEnvironment(t, "deployment-manager")
			dev := parseDeviceCapabilitiesFromFile(t, tc.deviceJSONFile)
			deviceID := dev.Properties.Id

			require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
				DeviceClientId:   deviceID,
				DeviceId:         deviceID,
				OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
				Capabilities:     dev,
			}))

			// 1st deployment: matches device -> profile
			req1, err := buildDeploymentRequestForDevice(tc.deploy1Name, experimentAppPkg, dev)
			require.NoError(t, err)
			assert.Equal(t, tc.wantProfile, req1.Spec.DeploymentProfile.Type)

			resp1, err := deployMgr.CreateDeployment(ctx, req1, experimentAppPkg)
			require.NoError(t, err, "first deployment of rt-benchmark-experiment-app must succeed")
			require.NotNil(t, resp1)
			require.NotNil(t, resp1.Id)

			storedRow1, err := db.GetDeployment(ctx, *resp1.Id)
			require.NoError(t, err)
			assert.Equal(t, float32(2.0), storedRow1.IsolatedCoresRequested)
			assert.Equal(t, int64(9216*1024), storedRow1.ExclusiveL3CacheBytesRequested)

			// 2nd deployment of the same package on the same device must fail validation (0.0 isolated cores remaining)
			req2, err := buildDeploymentRequestForDevice(tc.deploy2Name, experimentAppPkg, dev)
			require.NoError(t, err)

			resp2, err := deployMgr.CreateDeployment(ctx, req2, experimentAppPkg)
			require.Error(t, err, "second deployment of rt-benchmark-experiment-app must fail")
			assert.Nil(t, resp2)
			assert.Contains(t, err.Error(), "resource matching failed for device "+deviceID)
			assert.Contains(t, err.Error(), "insufficient isolated cpu cores: needs 2.0, device has 2.0 total, 2.0 already allocated, 0.0 remaining")

			// Verify only the first deployment is active in the database
			activeDeps, err := db.GetActiveDeploymentsByDevice(ctx, deviceID)
			require.NoError(t, err)
			require.Len(t, activeDeps, 1)
			assert.Equal(t, *resp1.Id, *activeDeps[0].DeploymentRequest.Id)

			// Verify that after deleting the first deployment (transitioning to REMOVING), deploying again succeeds
			deletedResp, err := deployMgr.DeleteDeployment(ctx, *resp1.Id)
			require.NoError(t, err)
			require.NotNil(t, deletedResp.Status)
			require.NotNil(t, deletedResp.Status.State)
			assert.Equal(t, margoNonStdAPI.ApplicationDeploymentStatusState(DeploymentStateRemoving), *deletedResp.Status.State)

			resp2Retry, err := deployMgr.CreateDeployment(ctx, req2, experimentAppPkg)
			require.NoError(t, err, "re-deploying after removing the first instance must succeed")
			require.NotNil(t, resp2Retry)
		})
	}
}

func TestCLIYamlGenScriptIntegration_HelmAndCompose(t *testing.T) {
	ctx := context.Background()
	margoFixturePath := filepath.Join("testdata", "rt-benchmark-experiment-margo.yaml")
	appPkg := parseTestAppPackageFromFile(t, margoFixturePath)

	cliCases := []struct {
		name              string
		deviceJSONFile    string
		profileType       string
		wantComponents    int
		wantCatName       string
		wantCyclicName    string
		wantCyclicPeriod  int
		wantCyclicPointer string
	}{
		{
			name:              "2.7.G cli-yaml-gen.sh generate_instance_yaml_from_oci (helm) produces valid request deployed by DeploymentManager",
			deviceJSONFile:    "k3s-device-capabilities.json",
			profileType:       "helm",
			wantComponents:    3,
			wantCatName:       "caterpillar",
			wantCyclicName:    "cyclictest",
			wantCyclicPeriod:  16,
			wantCyclicPointer: "cyclictest.threads",
		},
		{
			name:              "2.7.H cli-yaml-gen.sh generate_instance_yaml_from_oci (compose) produces valid request deployed by DeploymentManager",
			deviceJSONFile:    "compose-device-capabilities.json",
			profileType:       "compose",
			wantComponents:    3,
			wantCatName:       "caterpillar_compose",
			wantCyclicName:    "cyclictest_compose",
			wantCyclicPeriod:  25,
			wantCyclicPointer: "CYCLICTEST_THREADS",
		},
	}

	for _, tc := range cliCases {
		t.Run(tc.name, func(t *testing.T) {
			deployMgr, _, db := setupTestMargoEnvironment(t, "cli-yaml-gen-test")
			dev := parseDeviceCapabilitiesFromFile(t, tc.deviceJSONFile)
			deviceID := dev.Properties.Id

			require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
				DeviceClientId:   deviceID,
				DeviceId:         deviceID,
				OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
				Capabilities:     dev,
			}))

			// Exercise get_supported_deployments_for_device from cli-instances.sh against DeviceListResp
			deviceList := margoNonStdAPI.DeviceListResp{
				Items: []margoNonStdAPI.DeviceManifestResp{
					{
						Id:       &deviceID,
						Metadata: margoNonStdAPI.Metadata{Name: deviceID},
						Spec:     margoNonStdAPI.DeviceSpec{Capabilities: dev},
						State:    margoNonStdAPI.DeviceState{Onboard: margoNonStdAPI.DeviceOnboardStatusONBOARDED},
					},
				},
			}
			supportedCSV, err := resolveSupportedDeploymentsViaCLIScript(t, deviceID, deviceList)
			require.NoError(t, err, "get_supported_deployments_for_device in cli-instances.sh must succeed")
			assert.Equal(t, tc.profileType, supportedCSV)

			// Exercise generate_instance_yaml_from_oci from cli-yaml-gen.sh
			cliReq, err := generateDeploymentRequestViaCLI(
				t,
				"rt-benchmark-experiment-suite",
				*appPkg.Description.Id,
				deviceID,
				supportedCSV,
				margoFixturePath,
			)
			require.NoError(t, err)

			// Verify raw CLI-generated request BEFORE CreateDeployment merges package defaults
			assert.Equal(t, "rt-benchmark-experiment-suite-instance", cliReq.Metadata.Name)
			assert.Equal(t, margoNonStdAPI.DeploymentExecutionProfileType(tc.profileType), cliReq.Spec.DeploymentProfile.Type)
			require.Len(t, cliReq.Spec.DeploymentProfile.Components, tc.wantComponents)
			require.NotNil(t, cliReq.Spec.DeploymentProfile.DeviceConstraints, "cli-yaml-gen.sh must include profile deviceConstraints")
			require.NotNil(t, cliReq.Spec.Parameters, "cli-yaml-gen.sh must include filtered parameters")

			// Verify raw CLI output matches Go reference builder semantically (comparing typed components, constraints, and parameters)
			expectedReq, err := buildDeploymentRequestForDevice(cliReq.Metadata.Name, appPkg, dev)
			require.NoError(t, err)
			assert.Equal(t, expectedReq.Metadata, cliReq.Metadata)
			assert.Equal(t, expectedReq.Spec.AppPackageRef, cliReq.Spec.AppPackageRef)
			require.NotNil(t, cliReq.Spec.DeviceRef)
			require.NotNil(t, cliReq.Spec.DeviceRef.Id)
			assert.Equal(t, *expectedReq.Spec.DeviceRef.Id, *cliReq.Spec.DeviceRef.Id)
			assert.Equal(t, expectedReq.Spec.DeploymentProfile.Type, cliReq.Spec.DeploymentProfile.Type)
			assert.Equal(t, expectedReq.Spec.DeploymentProfile.DeviceConstraints, cliReq.Spec.DeploymentProfile.DeviceConstraints)
			assert.Equal(t, expectedReq.Spec.Parameters, cliReq.Spec.Parameters)
			require.Len(t, cliReq.Spec.DeploymentProfile.Components, len(expectedReq.Spec.DeploymentProfile.Components))
			for i := range cliReq.Spec.DeploymentProfile.Components {
				cliComp, cErr := cliReq.Spec.DeploymentProfile.Components[i].AsDeploymentProfileComponent()
				require.NoError(t, cErr)
				expComp, eErr := expectedReq.Spec.DeploymentProfile.Components[i].AsDeploymentProfileComponent()
				require.NoError(t, eErr)
				assert.Equal(t, expComp, cliComp, "raw cli-yaml-gen.sh component[%d] must match expected component before MergeWithAppPackage", i)
			}

			// Explicitly assert raw CLI component constraints, eligibilityRules, workload, and filtered parameter targets
			var rawCatComp, rawCyclicComp *margoNonStdAPI.DeploymentProfileComponent
			for i := range cliReq.Spec.DeploymentProfile.Components {
				comp, compErr := cliReq.Spec.DeploymentProfile.Components[i].AsDeploymentProfileComponent()
				require.NoError(t, compErr)
				switch comp.Name {
				case tc.wantCatName:
					rawCatComp = &comp
				case tc.wantCyclicName:
					rawCyclicComp = &comp
				}
			}
			require.NotNil(t, rawCatComp, "raw CLI output must contain %s component", tc.wantCatName)
			require.NotNil(t, rawCatComp.DeviceConstraints, "raw CLI output must preserve component deviceConstraints")
			require.NotNil(t, rawCatComp.DeviceConstraints.EligibilityRules, "raw CLI output must preserve component eligibilityRules")
			require.NotEmpty(t, *rawCatComp.DeviceConstraints.EligibilityRules)

			require.NotNil(t, rawCyclicComp, "raw CLI output must contain %s component", tc.wantCyclicName)
			require.NotNil(t, rawCyclicComp.Workload, "raw CLI output must preserve component RT workload")
			require.NotNil(t, rawCyclicComp.Workload.Scheduling)
			assert.Equal(t, margoNonStdAPI.RealTime, *rawCyclicComp.Workload.Scheduling)
			require.NotNil(t, rawCyclicComp.Workload.Deadline)
			assert.Equal(t, tc.wantCyclicPeriod, *rawCyclicComp.Workload.Deadline.Period)

			threadsParam, exists := (*cliReq.Spec.Parameters)["cyclictestThreads"]
			require.True(t, exists, "raw CLI output must include cyclictestThreads parameter")
			require.Len(t, threadsParam.Targets, 1, "raw CLI output must filter parameter targets to matching profile components")
			assert.Equal(t, tc.wantCyclicPointer, threadsParam.Targets[0].Pointer)
			assert.Equal(t, []string{tc.wantCyclicName}, threadsParam.Targets[0].Components)

			resp, err := deployMgr.CreateDeployment(ctx, cliReq, appPkg)
			require.NoError(t, err, "CreateDeployment with real cli-yaml-gen.sh output must succeed")
			require.NotNil(t, resp)
			require.NotNil(t, resp.Id)

			storedRow, err := db.GetDeployment(ctx, *resp.Id)
			require.NoError(t, err)
			assert.Equal(t, float32(2.0), storedRow.IsolatedCoresRequested)
			assert.Equal(t, int64(9216*1024), storedRow.ExclusiveL3CacheBytesRequested)

			// Verify desired state SBI manifest has eligibilityRules and workload preserved
			sbiManifest := storedRow.DesiredState.AppDeploymentManifest
			assert.Equal(t, sbi.DeviceIdWithAsterisk(deviceID), sbiManifest.Metadata.DeviceId)
			require.Len(t, sbiManifest.Spec.DeploymentProfile.Components, tc.wantComponents)

			var sbiCat, sbiCyclic *sbi.ApplicationDeploymentProfileComponent
			for i := range sbiManifest.Spec.DeploymentProfile.Components {
				c := &sbiManifest.Spec.DeploymentProfile.Components[i]
				switch c.Name {
				case tc.wantCatName:
					sbiCat = c
				case tc.wantCyclicName:
					sbiCyclic = c
				}
			}
			require.NotNil(t, sbiCat)
			require.NotNil(t, sbiCat.DeviceConstraints)
			require.NotNil(t, sbiCat.DeviceConstraints.EligibilityRules, "SBI manifest must preserve component eligibilityRules")
			require.NotNil(t, sbiCyclic)
			require.NotNil(t, sbiCyclic.Workload, "SBI manifest must preserve component workload")
			assert.Equal(t, sbi.RealTime, *sbiCyclic.Workload.Scheduling)
		})
	}
}
