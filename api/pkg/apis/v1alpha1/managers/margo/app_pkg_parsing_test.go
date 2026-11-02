package margo

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppPkgParsing_RTBenchmark(t *testing.T) {
	margoFixturePath := filepath.Join("testdata", "rt-benchmark-experiment-margo.yaml")
	appPkg := parseTestAppPackageFromFile(t, margoFixturePath)
	appDesc := appPkg.Description
	resources := appPkg.Resources
	require.NotNil(t, appDesc)
	assert.NotNil(t, resources)

	// Also verify the real workspace artefacts in sandbox/poc/tests/artefacts parse, validate, and match testdata copies
	for _, pkgPair := range []struct {
		fixtureName string
		artefactDir string
	}{
		{"rt-benchmark-experiment-margo.yaml", "rt-benchmark-experiment-app"},
		{"rt-benchmark-icore-margo.yaml", "rt-benchmark-icore-app"},
		{"rt-benchmark-iecache-margo.yaml", "rt-benchmark-iecache-app"},
		{"rt-benchmark-iscache-margo.yaml", "rt-benchmark-iscache-app"},
	} {
		fixturePkg := parseTestAppPackageFromFile(t, filepath.Join("testdata", pkgPair.fixtureName))
		artefactPath := filepath.Join(
			workspaceRootPath(t),
			"sandbox", "poc", "tests", "artefacts", pkgPair.artefactDir, "margo-package", "margo.yaml",
		)
		require.FileExists(t, artefactPath, "real %s margo.yaml artefact must exist", pkgPair.artefactDir)
		artefactPkg := parseTestAppPackageFromFile(t, artefactPath)
		assert.Equal(t, fixturePkg.Description, artefactPkg.Description,
			"testdata/%s must stay in sync with sandbox/poc/tests/artefacts/%s/margo-package/margo.yaml",
			pkgPair.fixtureName, pkgPair.artefactDir)
	}

	require.NotNil(t, appDesc.Id)
	assert.Equal(t, "rt-benchmark-experiment-suite", *appDesc.Id)
	assert.Equal(t, "1.0.0", appDesc.Metadata.Version)

	require.Len(t, appDesc.DeploymentProfiles, 2, "must parse both helm and compose profiles")

	// Profile 1: Helm
	helmProfile := appDesc.DeploymentProfiles[0]
	assert.Equal(t, margoNonStdAPI.AppDeploymentProfileTypeHelm, helmProfile.Type)
	require.Len(t, helmProfile.Components, 3)

	// Helm component 1: caterpillar
	catComp, err := helmProfile.Components[0].AsApplicationDeploymentProfileComponent()
	require.NoError(t, err)
	assert.Equal(t, "caterpillar", catComp.Name)
	require.NotNil(t, catComp.DeviceConstraints)
	require.NotNil(t, catComp.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, catComp.DeviceConstraints.CapacityRequirements.Cpu)
	require.Len(t, *catComp.DeviceConstraints.CapacityRequirements.Cpu, 1)
	catCPU := (*catComp.DeviceConstraints.CapacityRequirements.Cpu)[0]
	assert.Equal(t, float32(1), catCPU.Cores)
	require.NotNil(t, catCPU.Class)
	assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementClass("performance"), *catCPU.Class)
	require.NotNil(t, catCPU.Type)
	assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementType("isolated"), *catCPU.Type)

	require.NotNil(t, catComp.DeviceConstraints.CapacityRequirements.Cache)
	require.Len(t, *catComp.DeviceConstraints.CapacityRequirements.Cache, 1)
	catCache := (*catComp.DeviceConstraints.CapacityRequirements.Cache)[0]
	assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementLevel("L3"), catCache.Level)
	assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementAllocation("exclusive"), catCache.Allocation)
	require.NotNil(t, catCache.Size)
	assert.Equal(t, "9216Ki", *catCache.Size)

	// Verify caterpillar eligibilityRules
	require.NotNil(t, catComp.DeviceConstraints.EligibilityRules)
	require.Len(t, *catComp.DeviceConstraints.EligibilityRules, 1)
	catRule := (*catComp.DeviceConstraints.EligibilityRules)[0]
	require.NotNil(t, catRule.PropertySelector)
	require.Len(t, catRule.PropertySelector.MatchExpressions, 2)
	assert.Equal(t, "/supportedDeploymentTypes", catRule.PropertySelector.MatchExpressions[0].Key)

	// Helm component 2: cyclictest with RT workload attributes and isolated CPU
	cyclicComp, err := helmProfile.Components[1].AsApplicationDeploymentProfileComponent()
	require.NoError(t, err)
	assert.Equal(t, "cyclictest", cyclicComp.Name)
	require.NotNil(t, cyclicComp.Workload)
	require.NotNil(t, cyclicComp.Workload.Scheduling)
	assert.Equal(t, margoNonStdAPI.RealTime, *cyclicComp.Workload.Scheduling)
	require.NotNil(t, cyclicComp.Workload.Priority)
	assert.Equal(t, 95, *cyclicComp.Workload.Priority)
	require.NotNil(t, cyclicComp.Workload.Nice)
	assert.Equal(t, 10, *cyclicComp.Workload.Nice)
	require.NotNil(t, cyclicComp.Workload.Deadline)
	require.NotNil(t, cyclicComp.Workload.Deadline.Deadline)
	assert.Equal(t, 10, *cyclicComp.Workload.Deadline.Deadline)
	require.NotNil(t, cyclicComp.Workload.Deadline.Period)
	assert.Equal(t, 16, *cyclicComp.Workload.Deadline.Period)
	require.NotNil(t, cyclicComp.Workload.Deadline.Runtime)
	assert.Equal(t, 5, *cyclicComp.Workload.Deadline.Runtime)

	require.NotNil(t, cyclicComp.DeviceConstraints)
	require.NotNil(t, cyclicComp.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, cyclicComp.DeviceConstraints.CapacityRequirements.Cpu)
	require.Len(t, *cyclicComp.DeviceConstraints.CapacityRequirements.Cpu, 1)
	cyclicCPU := (*cyclicComp.DeviceConstraints.CapacityRequirements.Cpu)[0]
	assert.Equal(t, float32(1), cyclicCPU.Cores)
	assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementType("isolated"), *cyclicCPU.Type)

	// Profile deviceConstraints memory
	require.NotNil(t, helmProfile.DeviceConstraints)
	require.NotNil(t, helmProfile.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, helmProfile.DeviceConstraints.CapacityRequirements.Memory)
	assert.Equal(t, "1024Mi", helmProfile.DeviceConstraints.CapacityRequirements.Memory.Size)
	require.NotNil(t, helmProfile.DeviceConstraints.CapacityRequirements.Storage)
	assert.Equal(t, "2Gi", *helmProfile.DeviceConstraints.CapacityRequirements.Storage)

	// Profile 2: Compose
	composeProfile := appDesc.DeploymentProfiles[1]
	assert.Equal(t, margoNonStdAPI.AppDeploymentProfileTypeCompose, composeProfile.Type)
	require.Len(t, composeProfile.Components, 3)

	cyclicComposeComp, err := composeProfile.Components[0].AsApplicationDeploymentProfileComponent()
	require.NoError(t, err)
	assert.Equal(t, "cyclictest_compose", cyclicComposeComp.Name)
	require.NotNil(t, cyclicComposeComp.Workload)
	assert.Equal(t, margoNonStdAPI.RealTime, *cyclicComposeComp.Workload.Scheduling)
	require.NotNil(t, cyclicComposeComp.Workload.Priority)
	assert.Equal(t, 95, *cyclicComposeComp.Workload.Priority)
	require.NotNil(t, cyclicComposeComp.Workload.Deadline)
	require.NotNil(t, cyclicComposeComp.Workload.Deadline.Deadline)
	assert.Equal(t, 10, *cyclicComposeComp.Workload.Deadline.Deadline)
	require.NotNil(t, cyclicComposeComp.Workload.Deadline.Period)
	assert.Equal(t, 25, *cyclicComposeComp.Workload.Deadline.Period)
	require.NotNil(t, cyclicComposeComp.Workload.Deadline.Runtime)
	assert.Equal(t, 5, *cyclicComposeComp.Workload.Deadline.Runtime)

	catComposeComp, err := composeProfile.Components[2].AsApplicationDeploymentProfileComponent()
	require.NoError(t, err)
	assert.Equal(t, "caterpillar_compose", catComposeComp.Name)
	require.NotNil(t, catComposeComp.DeviceConstraints)
	require.NotNil(t, catComposeComp.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, catComposeComp.DeviceConstraints.CapacityRequirements.Cpu)
	require.Len(t, *catComposeComp.DeviceConstraints.CapacityRequirements.Cpu, 1)
	assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementType("isolated"), *(*catComposeComp.DeviceConstraints.CapacityRequirements.Cpu)[0].Type)

	require.NotNil(t, catComposeComp.DeviceConstraints.CapacityRequirements.Cache)
	require.Len(t, *catComposeComp.DeviceConstraints.CapacityRequirements.Cache, 1)
	catComposeCache := (*catComposeComp.DeviceConstraints.CapacityRequirements.Cache)[0]
	assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementLevel("L3"), catComposeCache.Level)
	assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementAllocation("exclusive"), catComposeCache.Allocation)
	require.NotNil(t, catComposeCache.Size)
	assert.Equal(t, "9216Ki", *catComposeCache.Size)

	require.NotNil(t, composeProfile.DeviceConstraints)
	require.NotNil(t, composeProfile.DeviceConstraints.CapacityRequirements)
	require.NotNil(t, composeProfile.DeviceConstraints.CapacityRequirements.Memory)
	assert.Equal(t, "1024Mi", composeProfile.DeviceConstraints.CapacityRequirements.Memory.Size)
	require.NotNil(t, composeProfile.DeviceConstraints.CapacityRequirements.Storage)
	assert.Equal(t, "1Gi", *composeProfile.DeviceConstraints.CapacityRequirements.Storage)

	// Transformation to Symphony objects
	transformer := NewMargoTransformer()
	pkgId := "rt-benchmark-experiment-suite"
	dbRow := AppPackageDatabaseRow{
		PackageRequest: margoNonStdAPI.ApplicationPackageManifestResp{
			Id: &pkgId,
			Metadata: margoNonStdAPI.Metadata{
				Name: "RT Benchmark Experiment Suite",
			},
			Spec: margoNonStdAPI.ApplicationPackageSpec{
				SourceType: margoNonStdAPI.OCIREPO,
			},
		},
		AppDescription: appDesc,
	}

	catalog, solution, solutionContainer, err := transformer.AppPackageToSymphonyObjects(
		context.Background(),
		dbRow,
		resources,
	)
	require.NoError(t, err, "AppPackageToSymphonyObjects must succeed")
	require.NotNil(t, catalog)
	require.NotNil(t, solution)
	require.NotNil(t, solutionContainer)

	assert.Equal(t, "rt-benchmark-experiment-suite-v-1.0.0", catalog.ObjectMeta.Name)
	assert.Equal(t, "rt-benchmark-experiment-suite-v-1.0.0", solution.ObjectMeta.Name)
	assert.Equal(t, "rt-benchmark-experiment-suite-v-1.0.0", solutionContainer.ObjectMeta.Name)
	assert.Equal(t, "2", solution.Spec.Metadata["profileCount"])
	require.Len(t, solution.Spec.Components, 6, "solution should contain components from all profiles")

	var helmCompSpec, composeCompSpec *model.ComponentSpec
	for i := range solution.Spec.Components {
		comp := &solution.Spec.Components[i]
		if comp.Name == "caterpillar" {
			helmCompSpec = comp
		}
		if comp.Name == "caterpillar_compose" {
			composeCompSpec = comp
		}
	}
	require.NotNil(t, helmCompSpec)
	assert.Equal(t, "helm.v3", helmCompSpec.Type)
	require.NotNil(t, composeCompSpec)
	assert.Equal(t, "compose", composeCompSpec.Type)
}
