package margo

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/model"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

type rawMargoYAML struct {
	ID       string `yaml:"id"`
	Metadata struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"metadata"`
	DeploymentProfiles []rawDeploymentProfile `yaml:"deploymentProfiles"`
}

type rawDeploymentProfile struct {
	Type              string                `yaml:"type"`
	ID                string                `yaml:"id"`
	Components        []rawComponent        `yaml:"components"`
	DeviceConstraints *rawDeviceConstraints `yaml:"deviceConstraints"`
}

type rawComponent struct {
	Name              string                `yaml:"name"`
	Workload          *rawWorkload          `yaml:"workload"`
	DeviceConstraints *rawDeviceConstraints `yaml:"deviceConstraints"`
}

type rawWorkload struct {
	Scheduling *string      `yaml:"scheduling"`
	Priority   *int         `yaml:"priority"`
	Nice       *int         `yaml:"nice"`
	Deadline   *rawDeadline `yaml:"deadline"`
}

type rawDeadline struct {
	Deadline *int `yaml:"deadline"`
	Period   *int `yaml:"period"`
	Runtime  *int `yaml:"runtime"`
}

type rawDeviceConstraints struct {
	CapacityRequirements *rawCapacityRequirements `yaml:"capacityRequirements"`
	EligibilityRules     []rawEligibilityRule     `yaml:"eligibilityRules"`
}

type rawCapacityRequirements struct {
	CPU     []rawCPU   `yaml:"cpu"`
	Cache   []rawCache `yaml:"cache"`
	Memory  *rawMemory `yaml:"memory"`
	Storage *string    `yaml:"storage"`
}

type rawCPU struct {
	Cores float32 `yaml:"cores"`
	Class *string `yaml:"class"`
	Type  *string `yaml:"type"`
}

type rawCache struct {
	Level      string  `yaml:"level"`
	Allocation string  `yaml:"allocation"`
	Size       *string `yaml:"size"`
}

type rawMemory struct {
	Size string `yaml:"size"`
}

type rawEligibilityRule struct {
	PropertySelector *rawPropertySelector `yaml:"propertySelector"`
}

type rawPropertySelector struct {
	MatchExpressions []rawMatchExpression `yaml:"matchExpressions"`
}

type rawMatchExpression struct {
	Key      string `yaml:"key"`
	Operator string `yaml:"operator"`
	Values   []any  `yaml:"values"`
}

func loadRawMargoYAML(t *testing.T, path string) rawMargoYAML {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "must read %s", path)

	var raw rawMargoYAML
	require.NoError(t, yaml.Unmarshal(data, &raw), "must unmarshal raw YAML from %s", path)
	return raw
}

func discoverTestdataYAMLs(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "testdata must contain at least one YAML file")
	return files
}

func TestTestdataMatchesWorkspaceArtefacts(t *testing.T) {
	pkgPairs := []struct {
		fixtureName string
		artefactDir string
	}{
		{"rt-benchmark-experiment-margo.yaml", "rt-benchmark-experiment-app"},
		{"rt-benchmark-icore-margo.yaml", "rt-benchmark-icore-app"},
		{"rt-benchmark-iecache-margo.yaml", "rt-benchmark-iecache-app"},
		{"rt-benchmark-iscache-margo.yaml", "rt-benchmark-iscache-app"},
	}

	for _, pkgPair := range pkgPairs {
		t.Run(pkgPair.artefactDir, func(t *testing.T) {
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
		})
	}
}

func TestAppPkgParsing_AllTestdata(t *testing.T) {
	for _, yamlPath := range discoverTestdataYAMLs(t) {
		t.Run(filepath.Base(yamlPath), func(t *testing.T) {
			expected := loadRawMargoYAML(t, yamlPath)
			appPkg := parseTestAppPackageFromFile(t, yamlPath)
			appDesc := appPkg.Description
			require.NotNil(t, appDesc)
			assert.NotNil(t, appPkg.Resources)

			require.NotNil(t, appDesc.Id)
			assert.Equal(t, expected.ID, *appDesc.Id)
			assert.Equal(t, expected.Metadata.Name, appDesc.Metadata.Name)
			assert.Equal(t, expected.Metadata.Version, appDesc.Metadata.Version)
			require.Len(t, appDesc.DeploymentProfiles, len(expected.DeploymentProfiles))

			for _, expProfile := range expected.DeploymentProfiles {
				t.Run(expProfile.Type, func(t *testing.T) {
					profile := requireProfileByType(t, appDesc, margoNonStdAPI.AppDeploymentProfileType(expProfile.Type))
					require.Len(t, profile.Components, len(expProfile.Components))
					assertDeviceConstraintsMatch(t, profile.DeviceConstraints, expProfile.DeviceConstraints)

					for _, expComp := range expProfile.Components {
						comp := requireComponentByName(t, profile, expComp.Name)
						assertWorkloadMatch(t, comp.Workload, expComp.Workload)
						assertDeviceConstraintsMatch(t, comp.DeviceConstraints, expComp.DeviceConstraints)
					}
				})
			}
		})
	}
}

func TestMargoTransformer_AllTestdata(t *testing.T) {
	transformer := NewMargoTransformer()

	for _, yamlPath := range discoverTestdataYAMLs(t) {
		t.Run(filepath.Base(yamlPath), func(t *testing.T) {
			appPkg := parseTestAppPackageFromFile(t, yamlPath)
			appDesc := appPkg.Description
			require.NotNil(t, appDesc)
			require.NotNil(t, appDesc.Id)

			catalog, solution, solutionContainer, err := transformer.AppPackageToSymphonyObjects(
				context.Background(),
				AppPackageDatabaseRow{
					PackageRequest: appPkg.Package,
					AppDescription: appDesc,
				},
				appPkg.Resources,
			)
			require.NoError(t, err, "AppPackageToSymphonyObjects must succeed")
			require.NotNil(t, catalog)
			require.NotNil(t, solution)
			require.NotNil(t, solutionContainer)

			expectedName := *appDesc.Id + "-v-" + appDesc.Metadata.Version
			assert.Equal(t, expectedName, catalog.ObjectMeta.Name)
			assert.Equal(t, expectedName, solution.ObjectMeta.Name)
			assert.Equal(t, expectedName, solutionContainer.ObjectMeta.Name)
			assert.Equal(t, strconv.Itoa(len(appDesc.DeploymentProfiles)), solution.Spec.Metadata["profileCount"])

			expectedTotalComponents := 0
			for _, profile := range appDesc.DeploymentProfiles {
				expectedTotalComponents += len(profile.Components)
				expectedType := expectedSymphonyComponentType(t, profile.Type)

				for _, rawComp := range profile.Components {
					comp, err := rawComp.AsApplicationDeploymentProfileComponent()
					require.NoError(t, err)
					solComp := requireSolutionComponentByName(t, solution.Spec.Components, comp.Name)
					assert.Equal(t, expectedType, solComp.Type)
				}
			}
			require.Len(t, solution.Spec.Components, expectedTotalComponents)
		})
	}
}

func expectedSymphonyComponentType(t *testing.T, profileType margoNonStdAPI.AppDeploymentProfileType) string {
	t.Helper()
	switch profileType {
	case margoNonStdAPI.AppDeploymentProfileTypeHelm:
		return "helm.v3"
	case margoNonStdAPI.AppDeploymentProfileTypeCompose:
		return "compose"
	default:
		t.Fatalf("unexpected deployment profile type %q", profileType)
		return ""
	}
}

func requireProfileByType(t *testing.T, appDesc *margoNonStdAPI.AppDescription, profileType margoNonStdAPI.AppDeploymentProfileType) margoNonStdAPI.AppDeploymentProfile {
	t.Helper()
	for _, profile := range appDesc.DeploymentProfiles {
		if profile.Type == profileType {
			return profile
		}
	}
	t.Fatalf("deployment profile of type %q not found", profileType)
	return margoNonStdAPI.AppDeploymentProfile{}
}

func requireComponentByName(t *testing.T, profile margoNonStdAPI.AppDeploymentProfile, name string) margoNonStdAPI.ApplicationDeploymentProfileComponent {
	t.Helper()
	for _, raw := range profile.Components {
		comp, err := raw.AsApplicationDeploymentProfileComponent()
		require.NoError(t, err)
		if comp.Name == name {
			return comp
		}
	}
	t.Fatalf("component %q not found in profile %s", name, profile.Type)
	return margoNonStdAPI.ApplicationDeploymentProfileComponent{}
}

func requireSolutionComponentByName(t *testing.T, components []model.ComponentSpec, name string) model.ComponentSpec {
	t.Helper()
	for _, comp := range components {
		if comp.Name == name {
			return comp
		}
	}
	t.Fatalf("solution component %q not found", name)
	return model.ComponentSpec{}
}

func assertWorkloadMatch(t *testing.T, actual *margoNonStdAPI.Workload, expected *rawWorkload) {
	t.Helper()
	if expected == nil {
		assert.Nil(t, actual)
		return
	}
	require.NotNil(t, actual)

	if expected.Scheduling != nil {
		require.NotNil(t, actual.Scheduling)
		assert.Equal(t, margoNonStdAPI.WorkloadSchedulingType(*expected.Scheduling), *actual.Scheduling)
	}
	if expected.Priority != nil {
		require.NotNil(t, actual.Priority)
		assert.Equal(t, *expected.Priority, *actual.Priority)
	}
	if expected.Nice != nil {
		require.NotNil(t, actual.Nice)
		assert.Equal(t, *expected.Nice, *actual.Nice)
	}
	if expected.Deadline != nil {
		require.NotNil(t, actual.Deadline)
		if expected.Deadline.Deadline != nil {
			require.NotNil(t, actual.Deadline.Deadline)
			assert.Equal(t, *expected.Deadline.Deadline, *actual.Deadline.Deadline)
		}
		if expected.Deadline.Period != nil {
			require.NotNil(t, actual.Deadline.Period)
			assert.Equal(t, *expected.Deadline.Period, *actual.Deadline.Period)
		}
		if expected.Deadline.Runtime != nil {
			require.NotNil(t, actual.Deadline.Runtime)
			assert.Equal(t, *expected.Deadline.Runtime, *actual.Deadline.Runtime)
		}
	}
}

func assertDeviceConstraintsMatch(t *testing.T, actual *margoNonStdAPI.DeviceConstraints, expected *rawDeviceConstraints) {
	t.Helper()
	if expected == nil {
		assert.Nil(t, actual)
		return
	}
	require.NotNil(t, actual)

	if expected.CapacityRequirements != nil {
		require.NotNil(t, actual.CapacityRequirements)
		assertCPUsMatch(t, actual.CapacityRequirements.Cpu, expected.CapacityRequirements.CPU)
		assertCachesMatch(t, actual.CapacityRequirements.Cache, expected.CapacityRequirements.Cache)

		if expected.CapacityRequirements.Memory != nil {
			require.NotNil(t, actual.CapacityRequirements.Memory)
			assert.Equal(t, expected.CapacityRequirements.Memory.Size, actual.CapacityRequirements.Memory.Size)
		}
		if expected.CapacityRequirements.Storage != nil {
			require.NotNil(t, actual.CapacityRequirements.Storage)
			assert.Equal(t, *expected.CapacityRequirements.Storage, *actual.CapacityRequirements.Storage)
		}
	}

	if len(expected.EligibilityRules) > 0 {
		require.NotNil(t, actual.EligibilityRules)
		require.Len(t, *actual.EligibilityRules, len(expected.EligibilityRules))
		for i, expRule := range expected.EligibilityRules {
			actRule := (*actual.EligibilityRules)[i]
			if expRule.PropertySelector != nil {
				require.NotNil(t, actRule.PropertySelector)
				require.Len(t, actRule.PropertySelector.MatchExpressions, len(expRule.PropertySelector.MatchExpressions))
				for j, expExpr := range expRule.PropertySelector.MatchExpressions {
					actExpr := actRule.PropertySelector.MatchExpressions[j]
					assert.Equal(t, expExpr.Key, actExpr.Key)
					assert.Equal(t, margoNonStdAPI.MatchExpressionOperator(expExpr.Operator), actExpr.Operator)
					if expExpr.Values != nil {
						require.NotNil(t, actExpr.Values)
						assert.Equal(t, expExpr.Values, *actExpr.Values)
					}
				}
			}
		}
	}
}

func assertCPUsMatch(t *testing.T, actual *[]margoNonStdAPI.DeploymentCpuRequirement, expected []rawCPU) {
	t.Helper()
	if len(expected) == 0 {
		return
	}
	require.NotNil(t, actual)
	require.Len(t, *actual, len(expected))
	for i, expCPU := range expected {
		actCPU := (*actual)[i]
		assert.Equal(t, expCPU.Cores, actCPU.Cores)
		if expCPU.Class != nil {
			require.NotNil(t, actCPU.Class)
			assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementClass(*expCPU.Class), *actCPU.Class)
		}
		if expCPU.Type != nil {
			require.NotNil(t, actCPU.Type)
			assert.Equal(t, margoNonStdAPI.DeploymentCpuRequirementType(*expCPU.Type), *actCPU.Type)
		}
	}
}

func assertCachesMatch(t *testing.T, actual *[]margoNonStdAPI.DeploymentCacheRequirement, expected []rawCache) {
	t.Helper()
	if len(expected) == 0 {
		return
	}
	require.NotNil(t, actual)
	require.Len(t, *actual, len(expected))
	for i, expCache := range expected {
		actCache := (*actual)[i]
		assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementLevel(expCache.Level), actCache.Level)
		assert.Equal(t, margoNonStdAPI.DeploymentCacheRequirementAllocation(expCache.Allocation), actCache.Allocation)
		if expCache.Size != nil {
			require.NotNil(t, actCache.Size)
			assert.Equal(t, *expCache.Size, *actCache.Size)
		}
	}
}
