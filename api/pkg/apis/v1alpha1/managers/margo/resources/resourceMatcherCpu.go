package resources

import (
	"fmt"
	"math"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	margoStdAPI "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
)

// isolatedCpuEpsilon prevents false rejections caused by IEEE-754 float32 rounding imprecision
const isolatedCpuEpsilon float32 = 0.0001

// determines whether a device has enough remaining isolated cpu cores for a deployment,
// accounting for active allocations
// returns the requested core count or error if not enough iso cores are available
func ValidateRequestedIsoCpuCapacity(
	device *margoStdAPI.DeviceCapabilitiesManifest,
	activeAllocations []DeploymentAllocation,
	profile *margoNonStdAPI.DeploymentExecutionProfile,
) (float32, error) {
	requestedIsoCores, err := countRequestedIsolatedCores(profile)
	if err != nil || requestedIsoCores == 0 {
		return 0, err
	}

	availableIsoCores, err := countDeviceIsolatedCores(device)
	if err != nil {
		return 0, err
	}

	currentlyAllocatedIsoCores, err := countAllocatedIsolatedCores(activeAllocations)
	if err != nil {
		return 0, err
	}

	remaining := availableIsoCores - currentlyAllocatedIsoCores
	if remaining+isolatedCpuEpsilon < requestedIsoCores {
		return requestedIsoCores, fmt.Errorf(
			"insufficient isolated cpu cores: needs %.1f, device has %.1f total, %.1f already allocated, %.1f remaining",
			requestedIsoCores, availableIsoCores, currentlyAllocatedIsoCores, remaining,
		)
	}

	return requestedIsoCores, nil
}

// sums all isolated cpu cores advertised in the device capabilities manifest
func countDeviceIsolatedCores(device *margoStdAPI.DeviceCapabilitiesManifest) (float32, error) {
	if device == nil || device.Properties.Cpus == nil {
		return 0, nil
	}

	var total float32
	for _, cpu := range *device.Properties.Cpus {
		if cpu.Type == nil || *cpu.Type != margoStdAPI.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated {
			continue
		}
		if err := validateCoreCount(cpu.Cores); err != nil {
			return 0, fmt.Errorf("invalid device isolated cpu cores %v: %w", cpu.Cores, err)
		}
		total += cpu.Cores
	}

	return total, nil
}

// sums the isolated cores reserved by active deployments using pre-computed allocations
func countAllocatedIsolatedCores(allocations []DeploymentAllocation) (float32, error) {
	var total float32
	for _, allocation := range allocations {
		if err := validateCoreCount(allocation.IsolatedCoresRequested); err != nil {
			return 0, fmt.Errorf("invalid stored isolatedCoresRequested %v for deployment %q: %w",
				allocation.IsolatedCoresRequested, allocation.DeploymentID, err,
			)
		}
		total += allocation.IsolatedCoresRequested
	}

	return total, nil
}

// sums the isolated cpu cores requested by a deployment profile
// component-level cpu capacity requirements override profile-level cpu requirements:
// if any component specifies cpu requirements, only component cpu requirements are counted;
// otherwise, profile-level cpu requirements are used.
func countRequestedIsolatedCores(profile *margoNonStdAPI.DeploymentExecutionProfile) (float32, error) {
	if profile == nil {
		return 0, nil
	}

	var componentTotal float32
	hasComponentCpu := false

	for _, comp := range profile.Components {
		depComp, err := comp.AsDeploymentProfileComponent()
		if err != nil {
			return 0, fmt.Errorf("failed to parse deployment profile component: %w", err)
		}
		if depComp.DeviceConstraints == nil || depComp.DeviceConstraints.CapacityRequirements == nil || depComp.DeviceConstraints.CapacityRequirements.Cpu == nil {
			continue
		}

		var isolatedCores, sharedCores float32
		for _, cpu := range *depComp.DeviceConstraints.CapacityRequirements.Cpu {
			if cpu.Type == nil {
				continue
			}
			switch *cpu.Type {
			case margoNonStdAPI.DeploymentCpuRequirementTypeIsolated:
				if err := validateCoreCount(cpu.Cores); err != nil {
					return 0, fmt.Errorf("invalid isolated cpu requirement for component %q: %w", depComp.Name, err)
				}
				isolatedCores += cpu.Cores
			case margoNonStdAPI.DeploymentCpuRequirementTypeShared:
				if err := validateCoreCount(cpu.Cores); err != nil {
					return 0, fmt.Errorf("invalid shared cpu requirement for component %q: %w", depComp.Name, err)
				}
				sharedCores += cpu.Cores
			}
		}
		if isolatedCores > 0 || sharedCores > 0 {
			hasComponentCpu = true
			componentTotal += isolatedCores
		}
	}

	if hasComponentCpu {
		return componentTotal, nil
	}

	if profile.DeviceConstraints == nil || profile.DeviceConstraints.CapacityRequirements == nil || profile.DeviceConstraints.CapacityRequirements.Cpu == nil {
		return 0, nil
	}

	var profileTotal float32
	for _, cpu := range *profile.DeviceConstraints.CapacityRequirements.Cpu {
		if cpu.Type != nil && *cpu.Type == margoNonStdAPI.DeploymentCpuRequirementTypeIsolated {
			if err := validateCoreCount(cpu.Cores); err != nil {
				return 0, fmt.Errorf("invalid cpu requirement for profile: %w", err)
			}
			profileTotal += cpu.Cores

		}
	}

	return profileTotal, nil
}

// validateCoreCount ensures a core count is finite and non-negative.
func validateCoreCount(cores float32) error {
	val := float64(cores)
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return fmt.Errorf("core count must be a finite number, got %v", cores)
	}
	if cores < 0 {
		return fmt.Errorf("core count must not be negative, got %v", cores)
	}

	return nil
}
