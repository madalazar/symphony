package margo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	coa_ctx "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states/memorystate"
	powerfulYaml "github.com/ghodss/yaml"
	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	p "github.com/margo/sandbox/shared-lib/pointers"
	"github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/require"
)

func workspaceRootPath(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must succeed")
	// managers/margo -> v1alpha1 -> apis -> pkg -> api -> symphony -> workspace root
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../../../"))
}

func loadTestdataBytes(t *testing.T, fileName string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fileName))
	require.NoError(t, err, "must read testdata/%s", fileName)
	return data
}

func parseTestAppPackageFromFile(t *testing.T, relativeOrAbsPath string) ApplicationPackage {
	t.Helper()
	rawBytes, err := os.ReadFile(relativeOrAbsPath)
	require.NoError(t, err, "must read margo.yaml from %s", relativeOrAbsPath)
	return parseTestAppPackageFromYAML(t, string(rawBytes))
}

func parseTestAppPackageFromYAML(t *testing.T, yamlContent string) ApplicationPackage {
	t.Helper()

	tempDir := t.TempDir()
	margoPath := filepath.Join(tempDir, "margo.yaml")
	err := os.WriteFile(margoPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	appPkgMgr := &AppPkgManager{}
	appDesc, resources, err := appPkgMgr.parseApplicationDescription(tempDir)
	require.NoError(t, err)
	require.NotNil(t, appDesc)
	require.NoError(t, appPkgMgr.validateApplicationDescription(appDesc))
	require.NotNil(t, appDesc.Id)

	pkgId := *appDesc.Id
	return ApplicationPackage{
		Package: margoNonStdAPI.ApplicationPackageManifestResp{
			Id: &pkgId,
			Metadata: margoNonStdAPI.Metadata{
				Name: appDesc.Metadata.Name,
			},
			Spec: margoNonStdAPI.ApplicationPackageSpec{
				SourceType: margoNonStdAPI.OCIREPO,
			},
		},
		Description: appDesc,
		Resources:   resources,
	}
}

func setupTestAppPackage(t *testing.T) ApplicationPackage {
	t.Helper()
	return parseTestAppPackageFromFile(t, filepath.Join("testdata", "rt-benchmark-experiment-margo.yaml"))
}

func parseDeviceCapabilitiesFromFile(t *testing.T, fileName string) *sbi.DeviceCapabilitiesManifest {
	t.Helper()
	rawBytes := loadTestdataBytes(t, fileName)
	var caps sbi.DeviceCapabilitiesManifest
	require.NoError(t, json.Unmarshal(rawBytes, &caps))
	return &caps
}

func setupTestMargoEnvironment(t *testing.T, name PublishGroupName) (*DeploymentManager, *DeploymentBundleManager, *MargoDatabase) {
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

	db := NewMargoDatabase(mContext, name, stateProvider)
	stateMachine := NewDeploymentStateMachine(db, deploymentLogger)
	transformer := NewMargoTransformer()

	bundleMgr := &DeploymentBundleManager{
		Database:      db,
		StateProvider: stateProvider,
	}

	deployMgr := &DeploymentManager{
		database:      db,
		stateMachine:  stateMachine,
		tranformer:    transformer,
		bundleManager: nil, // set nil to avoid uncoordinated background goroutines in unit/integration tests
	}

	return deployMgr, bundleMgr, db
}

func setupTestDeploymentManager(t *testing.T) *DeploymentManager {
	t.Helper()
	deployMgr, _, db := setupTestMargoEnvironment(t, "deployment-manager")
	ctx := context.Background()

	require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
		DeviceClientId:   "helm-device-01",
		DeviceId:         "helm-device-01",
		OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
		Capabilities:     buildEligibilityDevice("helm-device-01", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm),
	}))
	require.NoError(t, db.UpsertDevice(ctx, DeviceDatabaseRow{
		DeviceClientId:   "compose-device-01",
		DeviceId:         "compose-device-01",
		OnboardingStatus: margoNonStdAPI.DeviceOnboardStatusONBOARDED,
		Capabilities:     buildEligibilityDevice("compose-device-01", sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose),
	}))

	return deployMgr
}

// BuildRTTestDevice constructs a DeviceCapabilitiesManifest with standard RT identity fields.
func BuildRTTestDevice(id, vendor, modelNumber, serialNumber string) *sbi.DeviceCapabilitiesManifest {
	d := &sbi.DeviceCapabilitiesManifest{}
	d.Properties.Id = id
	d.Properties.Vendor = vendor
	d.Properties.ModelNumber = modelNumber
	d.Properties.SerialNumber = serialNumber
	return d
}

// WithRTTestCPU appends a CPU capability entry to a DeviceCapabilitiesManifest.
func WithRTTestCPU(
	device *sbi.DeviceCapabilitiesManifest,
	arch sbi.DeviceCapabilitiesManifestPropertiesCpusArchitecture,
	cpuClass sbi.DeviceCapabilitiesManifestPropertiesCpusClass,
	cpuType sbi.DeviceCapabilitiesManifestPropertiesCpusType,
	cores float32,
	baseMHz float32,
	maxMHz float32,
) *sbi.DeviceCapabilitiesManifest {
	type cpuItem = struct {
		Architecture *sbi.DeviceCapabilitiesManifestPropertiesCpusArchitecture `json:"architecture,omitempty"`
		Class        *sbi.DeviceCapabilitiesManifestPropertiesCpusClass        `json:"class,omitempty"`
		Cores        float32                                                   `json:"cores"`
		Frequency    *struct {
			BaseMHz *float32 `json:"baseMHz,omitempty"`
			MaxMHz  *float32 `json:"maxMHz,omitempty"`
		} `json:"frequency,omitempty"`
		Type *sbi.DeviceCapabilitiesManifestPropertiesCpusType `json:"type,omitempty"`
	}
	cpu := cpuItem{
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
		device.Properties.Cpus = &[]cpuItem{}
	}
	*device.Properties.Cpus = append(*device.Properties.Cpus, cpu)
	return device
}

// WithRTTestCache appends a cache capability entry to a DeviceCapabilitiesManifest.
func WithRTTestCache(
	device *sbi.DeviceCapabilitiesManifest,
	level sbi.DeviceCapabilitiesManifestPropertiesCacheLevel,
	allocations []sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes,
	size string,
) *sbi.DeviceCapabilitiesManifest {
	type cacheItem = struct {
		AllocationTypes []sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes `json:"allocationTypes"`
		Level           sbi.DeviceCapabilitiesManifestPropertiesCacheLevel             `json:"level"`
		Size            string                                                         `json:"size"`
	}
	entry := cacheItem{
		Level:           level,
		AllocationTypes: allocations,
		Size:            size,
	}
	if device.Properties.Cache == nil {
		device.Properties.Cache = &[]cacheItem{}
	}
	*device.Properties.Cache = append(*device.Properties.Cache, entry)
	return device
}

// WithRTTestMemory sets the memory capability on a DeviceCapabilitiesManifest.
func WithRTTestMemory(
	device *sbi.DeviceCapabilitiesManifest,
	size string,
	bandwidthAllocationTypes []sbi.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes,
) *sbi.DeviceCapabilitiesManifest {
	var bwPtr *[]sbi.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes
	if len(bandwidthAllocationTypes) > 0 {
		bwCopy := append([]sbi.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes(nil), bandwidthAllocationTypes...)
		bwPtr = &bwCopy
	}
	device.Properties.Memory = &struct {
		BandwidthAllocationTypes *[]sbi.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes `json:"bandwidthAllocationTypes,omitempty"`
		Size                     string                                                                    `json:"size"`
	}{
		Size:                     size,
		BandwidthAllocationTypes: bwPtr,
	}
	return device
}

// WithRTTestLabels attaches supplier-defined labels to a DeviceCapabilitiesManifest.
func WithRTTestLabels(
	device *sbi.DeviceCapabilitiesManifest,
	labels map[string]interface{},
) *sbi.DeviceCapabilitiesManifest {
	result := make(
		map[string]sbi.DeviceCapabilitiesManifest_Labels_AdditionalProperties,
		len(labels),
	)
	for k, v := range labels {
		raw, err := json.Marshal(v)
		if err != nil {
			panic("WithRTTestLabels: failed to marshal label value: " + err.Error())
		}
		var prop sbi.DeviceCapabilitiesManifest_Labels_AdditionalProperties
		if err := json.Unmarshal(raw, &prop); err != nil {
			panic("WithRTTestLabels: failed to unmarshal label value: " + err.Error())
		}
		result[k] = prop
	}
	device.Labels = &result
	return device
}

// buildEligibilityDevice creates a DeviceCapabilitiesManifest with specific deployment types and RT specs.
func buildEligibilityDevice(
	id string,
	deploymentType sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes,
) *sbi.DeviceCapabilitiesManifest {
	dev := BuildRTTestDevice(id, "Industrial Edge", "RT-Test-Node", "SN-RT-999")
	WithRTTestCPU(
		dev,
		sbi.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64,
		sbi.DeviceCapabilitiesManifestPropertiesCpusClassPerformance,
		sbi.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated,
		8.0,
		3000.0,
		5000.0,
	)
	WithRTTestCache(
		dev,
		sbi.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
		[]sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
			sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
			sbi.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
		},
		"9216Ki",
	)
	WithRTTestMemory(dev, "1024Mi", nil)
	dev.Properties.Storage = p.Ptr("64Gi")
	dev.Properties.SupportedDeploymentTypes = &[]sbi.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
		deploymentType,
	}
	dev.Properties.SupportedRuntimes = &[]sbi.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
		sbi.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
	}
	return dev
}

// resolveCompatibleProfileType replicates the strict profile-type matching in
// sandbox/scripts/easy-cli/cli-yaml-gen.sh (resolve_device_profile_type / generate_instance_yaml_from_oci):
// it normalizes supportedDeploymentTypes to lowercase and matches against appPkg.Description.DeploymentProfiles.
func resolveCompatibleProfileType(
	device *sbi.DeviceCapabilitiesManifest,
	appPkg ApplicationPackage,
) (margoNonStdAPI.DeploymentExecutionProfileType, error) {
	if device == nil || device.Properties.SupportedDeploymentTypes == nil || len(*device.Properties.SupportedDeploymentTypes) == 0 {
		return "", fmt.Errorf("device has no supportedDeploymentTypes")
	}
	if appPkg.Description == nil || len(appPkg.Description.DeploymentProfiles) == 0 {
		return "", fmt.Errorf("application package has no deploymentProfiles")
	}

	for _, supported := range *device.Properties.SupportedDeploymentTypes {
		normalizedSupported := strings.ToLower(string(supported))
		for _, profile := range appPkg.Description.DeploymentProfiles {
			if strings.ToLower(string(profile.Type)) == normalizedSupported {
				return margoNonStdAPI.DeploymentExecutionProfileType(profile.Type), nil
			}
		}
	}

	return "", fmt.Errorf("no deployment profile in package %s matches device supportedDeploymentTypes", *appPkg.Description.Id)
}

// buildDeploymentRequestForDevice replicates the CLI instance generation flow from
// sandbox/scripts/easy-cli/cli-instances.sh and cli-yaml-gen.sh:
//  1. Queries the target device's supportedDeploymentTypes ("helm" or "compose").
//  2. Finds the matching profile in appPkg.Description.DeploymentProfiles.
//  3. Copies full components (properties, deviceConstraints, workload), profile-level deviceConstraints,
//     and filtered parameters targeting the selected profile's components.
func buildDeploymentRequestForDevice(
	name string,
	appPkg ApplicationPackage,
	device *sbi.DeviceCapabilitiesManifest,
) (margoNonStdAPI.ApplicationDeploymentManifestRequest, error) {
	profileType, err := resolveCompatibleProfileType(device, appPkg)
	if err != nil {
		return margoNonStdAPI.ApplicationDeploymentManifestRequest{}, err
	}

	var matchedProfile *margoNonStdAPI.AppDeploymentProfile
	for i := range appPkg.Description.DeploymentProfiles {
		if margoNonStdAPI.DeploymentExecutionProfileType(appPkg.Description.DeploymentProfiles[i].Type) == profileType {
			matchedProfile = &appPkg.Description.DeploymentProfiles[i]
			break
		}
	}
	if matchedProfile == nil {
		return margoNonStdAPI.ApplicationDeploymentManifestRequest{}, fmt.Errorf("profile %s not found", profileType)
	}

	componentNames := make(map[string]bool, len(matchedProfile.Components))
	components := make([]margoNonStdAPI.DeploymentExecutionProfile_Components_Item, len(matchedProfile.Components))
	for i, comp := range matchedProfile.Components {
		appComp, err := comp.AsApplicationDeploymentProfileComponent()
		if err != nil {
			return margoNonStdAPI.ApplicationDeploymentManifestRequest{}, err
		}
		componentNames[appComp.Name] = true

		deployComp := margoNonStdAPI.DeploymentProfileComponent{
			Name: appComp.Name,
			Properties: struct {
				Repository string  `json:"repository"`
				Revision   string  `json:"revision"`
				Timeout    *string `json:"timeout,omitempty"`
				Wait       *bool   `json:"wait,omitempty"`
			}{
				Repository: appComp.Properties.Repository,
				Revision:   appComp.Properties.Revision,
				Timeout:    appComp.Properties.Timeout,
				Wait:       appComp.Properties.Wait,
			},
			DeviceConstraints: appComp.DeviceConstraints,
			Workload:          appComp.Workload,
		}
		var item margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		if err := item.FromDeploymentProfileComponent(deployComp); err != nil {
			return margoNonStdAPI.ApplicationDeploymentManifestRequest{}, err
		}
		components[i] = item
	}

	var filteredParams *margoNonStdAPI.DeploymentParameters
	if appPkg.Description.Parameters != nil {
		paramsMap := make(margoNonStdAPI.DeploymentParameters)
		for paramKey, paramDef := range *appPkg.Description.Parameters {
			var matchedTargets []margoNonStdAPI.DeploymentParameterTarget
			for _, target := range paramDef.Targets {
				var matchedComps []string
				for _, targetComp := range target.Components {
					if componentNames[targetComp] {
						matchedComps = append(matchedComps, targetComp)
					}
				}
				if len(matchedComps) > 0 {
					matchedTargets = append(matchedTargets, margoNonStdAPI.DeploymentParameterTarget{
						Components: matchedComps,
						Pointer:    target.Pointer,
					})
				}
			}
			if len(matchedTargets) > 0 {
				paramsMap[paramKey] = margoNonStdAPI.DeploymentParameterValue{
					Targets: matchedTargets,
					Value:   paramDef.Value,
				}
			}
		}
		if len(paramsMap) > 0 {
			filteredParams = &paramsMap
		}
	}

	req := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
	req.Metadata.Name = name
	req.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
		AppPackageRef: struct {
			Id string `json:"id"`
		}{
			Id: *appPkg.Description.Id,
		},
		DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
			Id: p.Ptr(device.Properties.Id),
		},
		DeploymentProfile: margoNonStdAPI.DeploymentExecutionProfile{
			Type:              profileType,
			Components:        components,
			DeviceConstraints: matchedProfile.DeviceConstraints,
		},
		Parameters: filteredParams,
	}

	return req, nil
}

// buildSkeletonDeploymentRequest builds a minimal name-only component deployment request
// to specifically test MargoTransformer's fallback merging from the application package.
func buildSkeletonDeploymentRequest(
	name, appPkgID, deviceID string,
	profileType margoNonStdAPI.DeploymentExecutionProfileType,
	componentNames []string,
) (margoNonStdAPI.ApplicationDeploymentManifestRequest, error) {
	components := make([]margoNonStdAPI.DeploymentExecutionProfile_Components_Item, len(componentNames))
	for i, compName := range componentNames {
		var item margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		if err := item.FromDeploymentProfileComponent(margoNonStdAPI.DeploymentProfileComponent{Name: compName}); err != nil {
			return margoNonStdAPI.ApplicationDeploymentManifestRequest{}, err
		}
		components[i] = item
	}

	req := margoNonStdAPI.ApplicationDeploymentManifestRequest{}
	req.Metadata.Name = name
	req.Spec = margoNonStdAPI.ApplicationDeploymentSpec{
		AppPackageRef: struct {
			Id string `json:"id"`
		}{
			Id: appPkgID,
		},
		DeviceRef: &margoNonStdAPI.ApplicationDeploymentSpec_DeviceRef{
			Id: p.Ptr(deviceID),
		},
		DeploymentProfile: margoNonStdAPI.DeploymentExecutionProfile{
			Type:       profileType,
			Components: components,
		},
	}
	return req, nil
}

func ensureCLIToolsAvailable(t *testing.T, tools ...string) string {
	t.Helper()
	homeDir, _ := os.UserHomeDir()
	augmentedPath := fmt.Sprintf("%s:%s/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", os.Getenv("PATH"), homeDir)
	t.Setenv("PATH", augmentedPath)
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("skipping CLI script integration test: required tool %q not found on PATH (%v)", tool, err)
		}
	}
	return augmentedPath
}

// generateDeploymentRequestViaCLI executes the real generate_instance_yaml_from_oci in
// sandbox/scripts/easy-cli/cli-yaml-gen.sh (with a stubbed oras binary that copies the local margo.yaml)
// and parses the resulting instance.yaml into an ApplicationDeploymentManifestRequest.
func generateDeploymentRequestViaCLI(
	t *testing.T,
	packageName, packageID, deviceID, supportedDeploymentsCSV, margoFilePath string,
) (margoNonStdAPI.ApplicationDeploymentManifestRequest, error) {
	t.Helper()

	augmentedPath := ensureCLIToolsAvailable(t, "bash", "yq")

	wsRoot := workspaceRootPath(t)
	scriptPath := filepath.Join(wsRoot, "sandbox", "scripts", "easy-cli", "cli-yaml-gen.sh")
	require.FileExists(t, scriptPath, "cli-yaml-gen.sh must exist in sandbox/scripts/easy-cli")

	absMargoPath, err := filepath.Abs(margoFilePath)
	require.NoError(t, err)

	outDir := t.TempDir()
	outFile := filepath.Join(outDir, "instance.yaml")

	// Create a stubbed `oras` binary in outDir/bin so generate_instance_yaml_from_oci can run offline
	binDir := filepath.Join(outDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	orasStubPath := filepath.Join(binDir, "oras")
	orasStubScript := fmt.Sprintf("#!/bin/bash\ncp %q ./margo.yaml\n", absMargoPath)
	require.NoError(t, os.WriteFile(orasStubPath, []byte(orasStubScript), 0755))

	bashCmd := fmt.Sprintf(
		"get_oci_repository_path() { echo \"oci://harbor.machine:8443/library/$1\"; }; source %q && generate_instance_yaml_from_oci %q %q %q %q %q",
		scriptPath,
		packageName,
		packageID,
		deviceID,
		outFile,
		supportedDeploymentsCSV,
	)

	cmd := exec.Command("bash", "-c", bashCmd)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PATH=%s:%s", binDir, augmentedPath),
		"EXPOSED_HARBOR_HOST=harbor.machine",
		"EXPOSED_HARBOR_PORT=8443",
		"OCI_ORGANIZATION=library",
		"REGISTRY_USER=admin",
		"REGISTRY_PASS=Harbor12345",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return margoNonStdAPI.ApplicationDeploymentManifestRequest{}, fmt.Errorf("generate_instance_yaml_from_oci failed: %w: %s", err, string(output))
	}

	rawYAML, err := os.ReadFile(outFile)
	require.NoError(t, err)

	jsonBytes, err := powerfulYaml.YAMLToJSON(rawYAML)
	require.NoError(t, err)

	var req margoNonStdAPI.ApplicationDeploymentManifestRequest
	require.NoError(t, json.Unmarshal(jsonBytes, &req))
	return req, nil
}

// resolveSupportedDeploymentsViaCLIScript executes get_supported_deployments_for_device from
// sandbox/scripts/easy-cli/cli-instances.sh against a stubbed maestro CLI returning the given DeviceListResp JSON.
func resolveSupportedDeploymentsViaCLIScript(
	t *testing.T,
	deviceID string,
	deviceList margoNonStdAPI.DeviceListResp,
) (string, error) {
	t.Helper()

	augmentedPath := ensureCLIToolsAvailable(t, "bash", "jq")

	wsRoot := workspaceRootPath(t)
	scriptPath := filepath.Join(wsRoot, "sandbox", "scripts", "easy-cli", "cli-instances.sh")
	require.FileExists(t, scriptPath, "cli-instances.sh must exist in sandbox/scripts/easy-cli")

	envelope := map[string]interface{}{
		"Data": []interface{}{deviceList},
	}
	payloadBytes, err := json.Marshal(envelope)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	jsonFile := filepath.Join(tmpDir, "devices.json")
	require.NoError(t, os.WriteFile(jsonFile, payloadBytes, 0644))

	maestroStub := filepath.Join(tmpDir, "maestro")
	stubScript := fmt.Sprintf("#!/bin/bash\ncat %q\n", jsonFile)
	require.NoError(t, os.WriteFile(maestroStub, []byte(stubScript), 0755))

	bashCmd := fmt.Sprintf(
		"check_maestro_cli() { return 0; }; MAESTRO_CLI_PATH=%q EXPOSED_SYMPHONY_HOST=localhost EXPOSED_SYMPHONY_PORT=8080; source %q && get_supported_deployments_for_device %q",
		tmpDir,
		scriptPath,
		deviceID,
	)

	cmd := exec.Command("bash", "-c", bashCmd)
	cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", augmentedPath))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get_supported_deployments_for_device failed: %w: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}
