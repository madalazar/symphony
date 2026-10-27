package resources

import (
	"math"
	"testing"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/margo/sandbox/shared-lib/pointers"
	margoStdAPI "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestDeviceWithCPUs(isolatedCores float32, sharedCores float32) *margoStdAPI.DeviceCapabilitiesManifest {
	var caps margoStdAPI.DeviceCapabilitiesManifest
	caps.Properties.Id = "test-device-cpu"
	caps.Properties.Cpus = &[]struct {
		Architecture *margoStdAPI.DeviceCapabilitiesManifestPropertiesCpusArchitecture `json:"architecture,omitempty"`
		Class        *margoStdAPI.DeviceCapabilitiesManifestPropertiesCpusClass        `json:"class,omitempty"`
		Cores        float32                                                           `json:"cores"`
		Frequency    *struct {
			BaseMHz *float32 `json:"baseMHz,omitempty"`
			MaxMHz  *float32 `json:"maxMHz,omitempty"`
		} `json:"frequency,omitempty"`
		Type *margoStdAPI.DeviceCapabilitiesManifestPropertiesCpusType `json:"type,omitempty"`
	}{
		{
			Cores: isolatedCores,
			Type:  pointers.Ptr(margoStdAPI.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated),
		},
		{
			Cores: sharedCores,
			Type:  pointers.Ptr(margoStdAPI.DeviceCapabilitiesManifestPropertiesCpusTypeShared),
		},
	}

	return &caps
}

func createCPUConstraints(reqs ...margoNonStdAPI.DeploymentCpuRequirement) *margoNonStdAPI.DeviceConstraints {
	reqsCopy := append([]margoNonStdAPI.DeploymentCpuRequirement(nil), reqs...)
	return &margoNonStdAPI.DeviceConstraints{
		CapacityRequirements: &margoNonStdAPI.CapacityRequirements{
			Cpu: &reqsCopy,
		},
	}
}

func isolatedCPUReq(cores float32) margoNonStdAPI.DeploymentCpuRequirement {
	return margoNonStdAPI.DeploymentCpuRequirement{
		Type:  pointers.Ptr(margoNonStdAPI.DeploymentCpuRequirementTypeIsolated),
		Cores: cores,
	}
}

func sharedCPUReq(cores float32) margoNonStdAPI.DeploymentCpuRequirement {
	return margoNonStdAPI.DeploymentCpuRequirement{
		Type:  pointers.Ptr(margoNonStdAPI.DeploymentCpuRequirementTypeShared),
		Cores: cores,
	}
}

func createTestProfileWithCPU(t *testing.T, profileCores *float32, compCores *float32) margoNonStdAPI.DeploymentExecutionProfile {
	t.Helper()

	profile := margoNonStdAPI.DeploymentExecutionProfile{
		Type: margoNonStdAPI.DeploymentExecutionProfileTypeCompose,
	}

	if profileCores != nil {
		profile.DeviceConstraints = createCPUConstraints(isolatedCPUReq(*profileCores))
	}

	if compCores != nil {
		comp := margoNonStdAPI.DeploymentProfileComponent{
			Name:              "comp-cpu",
			DeviceConstraints: createCPUConstraints(isolatedCPUReq(*compCores)),
		}
		var item margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		require.NoError(t, item.FromDeploymentProfileComponent(comp))
		profile.Components = []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{item}
	}

	return profile
}

func createCPUAllocation(id string, isolatedCores float32) DeploymentAllocation {
	return DeploymentAllocation{
		DeploymentID:           id,
		IsolatedCoresRequested: isolatedCores,
	}
}

func TestValidateRequestedCpuCapacity_ComponentAndProfileResolution(t *testing.T) {
	device := createTestDeviceWithCPUs(4.0, 4.0)

	t.Run("component isolated CPU overrides profile isolated CPU", func(t *testing.T) {
		// Profile requests 8.0 cores (exceeds device's 4.0 isolated), but component requests 2.0 (fits)
		profile := createTestProfileWithCPU(t, pointers.Ptr(float32(8.0)), pointers.Ptr(float32(2.0)))

		requested, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &profile)
		require.NoError(t, err)
		assert.Equal(t, float32(2.0), requested)
	})

	t.Run("component shared CPU overrides profile isolated CPU", func(t *testing.T) {
		// Profile requests 8.0 isolated cores, but component overrides CPU requirements with 2.0 shared cores
		profile := margoNonStdAPI.DeploymentExecutionProfile{
			Type:              margoNonStdAPI.DeploymentExecutionProfileTypeCompose,
			DeviceConstraints: createCPUConstraints(isolatedCPUReq(8.0)),
		}
		comp := margoNonStdAPI.DeploymentProfileComponent{
			Name:              "comp-shared-only",
			DeviceConstraints: createCPUConstraints(sharedCPUReq(2.0)),
		}
		var item margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		require.NoError(t, item.FromDeploymentProfileComponent(comp))
		profile.Components = []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{item}

		requested, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &profile)
		require.NoError(t, err)
		assert.Equal(t, float32(0), requested)
	})

	t.Run("falls back to profile CPU when components specify no CPU requirements", func(t *testing.T) {
		profile := createTestProfileWithCPU(t, pointers.Ptr(float32(3.0)), nil)
		compWithoutCPU := margoNonStdAPI.DeploymentProfileComponent{Name: "comp-no-cpu"}
		var item margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		require.NoError(t, item.FromDeploymentProfileComponent(compWithoutCPU))
		profile.Components = []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{item}

		requested, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &profile)
		require.NoError(t, err)
		assert.Equal(t, float32(3.0), requested)
	})

	t.Run("sums isolated CPU across multiple components and entries", func(t *testing.T) {
		comp1 := margoNonStdAPI.DeploymentProfileComponent{
			Name:              "comp-1",
			DeviceConstraints: createCPUConstraints(isolatedCPUReq(1.5), sharedCPUReq(2.0), isolatedCPUReq(0.5)),
		}
		comp2 := margoNonStdAPI.DeploymentProfileComponent{
			Name:              "comp-2",
			DeviceConstraints: createCPUConstraints(isolatedCPUReq(2.0)),
		}
		var item1, item2 margoNonStdAPI.DeploymentExecutionProfile_Components_Item
		require.NoError(t, item1.FromDeploymentProfileComponent(comp1))
		require.NoError(t, item2.FromDeploymentProfileComponent(comp2))

		profile := margoNonStdAPI.DeploymentExecutionProfile{
			Type:       margoNonStdAPI.DeploymentExecutionProfileTypeCompose,
			Components: []margoNonStdAPI.DeploymentExecutionProfile_Components_Item{item1, item2},
		}

		requested, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &profile)
		require.NoError(t, err)
		assert.Equal(t, float32(4.0), requested)
	})
}

func TestValidateRequestedCpuCapacity_CapacityLimitsAndEpsilon(t *testing.T) {
	t.Run("capacity exceeded returns requested cores and error", func(t *testing.T) {
		device := createTestDeviceWithCPUs(4.0, 0.0)
		existingDep := createCPUAllocation("existing-dep", 3.0)

		// Needs 2.0 cores -> 3.0 allocated + 2.0 = 5.0 > 4.0 -> should fail and still return requested 2.0
		oversizedProfile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(2.0)))
		req, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, []DeploymentAllocation{existingDep}, &oversizedProfile)
		require.Error(t, err)
		assert.Equal(t, float32(2.0), req)
		assert.Contains(t, err.Error(), "insufficient isolated cpu cores")
	})

	t.Run("exact remaining capacity fits", func(t *testing.T) {
		device := createTestDeviceWithCPUs(4.0, 0.0)
		existingDep := createCPUAllocation("existing-dep", 3.0)

		fittingProfile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(1.0)))
		req, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, []DeploymentAllocation{existingDep}, &fittingProfile)
		require.NoError(t, err)
		assert.Equal(t, float32(1.0), req)
	})

	t.Run("float32 rounding imprecision fits within epsilon", func(t *testing.T) {
		// In IEEE-754 float32, 1.0 - (0.3 + 0.3 + 0.3) is 0.099999964, which is slightly less than float32(0.1)
		device := createTestDeviceWithCPUs(1.0, 0.0)
		allocations := []DeploymentAllocation{
			createCPUAllocation("dep-1", 0.3),
			createCPUAllocation("dep-2", 0.3),
			createCPUAllocation("dep-3", 0.3),
		}

		profile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(0.1)))
		req, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, allocations, &profile)
		require.NoError(t, err)
		assert.InDelta(t, 0.1, req, 1e-6)
	})
}

func TestValidateRequestedCpuCapacity_InactiveDeploymentsAndEdgeCases(t *testing.T) {
	t.Run("only active deployment allocations are counted", func(t *testing.T) {
		device := createTestDeviceWithCPUs(4.0, 0.0)
		// Inactive records are filtered by the database caller before passing allocations here.
		profile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(4.0)))
		req, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &profile)
		require.NoError(t, err)
		assert.Equal(t, float32(4.0), req)

		activeAllocation := createCPUAllocation("active-dep", 4.0)
		_, err = ValidateRequestedIsoCpuCapacity("test-device-cpu", device, []DeploymentAllocation{activeAllocation}, &profile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient isolated cpu cores")
	})

	t.Run("zero requested isolated cores succeeds even if device is nil or over-committed", func(t *testing.T) {
		overcommittedDep := createCPUAllocation("overcommitted-dep", 8.0)
		device := createTestDeviceWithCPUs(4.0, 0.0)
		emptyProfile := createTestProfileWithCPU(t, nil, nil)

		req, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, []DeploymentAllocation{overcommittedDep}, &emptyProfile)
		require.NoError(t, err)
		assert.Equal(t, float32(0), req)

		req, err = ValidateRequestedIsoCpuCapacity("test-device-cpu", nil, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, float32(0), req)
	})

	t.Run("nil device fails cleanly when isolated cores are requested", func(t *testing.T) {
		profile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(1.0)))
		req, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", nil, nil, &profile)
		require.Error(t, err)
		assert.Equal(t, float32(1.0), req)
		assert.Contains(t, err.Error(), "insufficient isolated cpu cores")
	})

	t.Run("rejects negative, NaN, and Inf core counts", func(t *testing.T) {
		device := createTestDeviceWithCPUs(4.0, 0.0)

		// Component-level negative core count
		negativeCompProfile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(-1.0)))
		_, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &negativeCompProfile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "core count must not be negative")

		// Profile-level negative, NaN, and Inf core counts
		for _, invalidCores := range []float32{-1.0, float32(math.NaN()), float32(math.Inf(1))} {
			profile := createTestProfileWithCPU(t, pointers.Ptr(invalidCores), nil)
			_, err := ValidateRequestedIsoCpuCapacity("test-device-cpu", device, nil, &profile)
			require.Error(t, err)
		}

		// Negative stored deployment cores
		badDep := createCPUAllocation("bad-dep", -2.0)
		validProfile := createTestProfileWithCPU(t, nil, pointers.Ptr(float32(1.0)))
		_, err = ValidateRequestedIsoCpuCapacity("test-device-cpu", device, []DeploymentAllocation{badDep}, &validProfile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid stored isolatedCoresRequested")
	})
}
