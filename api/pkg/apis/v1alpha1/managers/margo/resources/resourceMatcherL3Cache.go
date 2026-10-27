package resources

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	margoStdAPI "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
)

// DeploymentAllocation holds the pre-computed resource reservations of an active deployment.
type DeploymentAllocation struct {
	DeploymentID                   string
	IsolatedCoresRequested         float32
	ExclusiveL3CacheBytesRequested int64
}

const (
	profileComponentName = "<profile>"

	kiB = 1 << 10
	miB = 1 << 20
	giB = 1 << 30
	tiB = 1 << 40

	kB = 1000
	mB = 1000 * kB
	gB = 1000 * mB
	tB = 1000 * gB
)

// checked in order of longest suffix first to prevent prefix collision
var binarySizeUnitMultipliers = []struct {
	suffix     string
	multiplier float64
}{
	// 3-letter suffixes
	{suffix: "TIB", multiplier: tiB},
	{suffix: "GIB", multiplier: giB},
	{suffix: "MIB", multiplier: miB},
	{suffix: "KIB", multiplier: kiB},
	// 2-letter suffixes
	{suffix: "TI", multiplier: tiB},
	{suffix: "GI", multiplier: giB},
	{suffix: "MI", multiplier: miB},
	{suffix: "KI", multiplier: kiB},
	{suffix: "TB", multiplier: tB},
	{suffix: "GB", multiplier: gB},
	{suffix: "MB", multiplier: mB},
	{suffix: "KB", multiplier: kB},
	// 1-letter suffix
	{suffix: "B", multiplier: 1},
}

// validates whether a device supports the cache requirements of the deployment
// (has sufficient l3 cache capacity), returning the total requested exclusive
// l3 cache in bytes or an error if not
func ValidateRequestedL3CacheCapacity(
	deviceCapabilities *margoStdAPI.DeviceCapabilitiesManifest,
	activeAllocations []DeploymentAllocation,
	profile *margoNonStdAPI.DeploymentExecutionProfile,
) (int64, error) {
	sharedL3Bytes, exclusiveL3Bytes, err := computeRequestedL3CacheRequirements(profile)
	if err != nil || (sharedL3Bytes == 0 && exclusiveL3Bytes == 0) {
		return 0, err
	}

	totalL3Bytes, err := validateDeviceL3CacheSupport(deviceCapabilities, sharedL3Bytes, exclusiveL3Bytes)
	if err != nil {
		return 0, err
	}

	allocatedExclusiveL3Bytes, err := computeAllocatedExclusiveL3Bytes(activeAllocations)
	if err != nil {
		return 0, err
	}

	availableExclusiveL3Bytes := totalL3Bytes - allocatedExclusiveL3Bytes
	if exclusiveL3Bytes > availableExclusiveL3Bytes {
		return 0, fmt.Errorf(
			"exclusive cache requested %d bytes exceeds available exclusive l3 cache %d bytes (total %d, already allocated %d)",
			exclusiveL3Bytes,
			availableExclusiveL3Bytes,
			totalL3Bytes,
			allocatedExclusiveL3Bytes,
		)
	}

	availableSharedBytes := availableExclusiveL3Bytes - exclusiveL3Bytes
	if sharedL3Bytes > availableSharedBytes {
		return 0, fmt.Errorf(
			"shared cache requested %d bytes exceeds total available l3 cache %d bytes",
			sharedL3Bytes,
			availableSharedBytes,
		)
	}

	return exclusiveL3Bytes, nil
}

// computes the requested l3 cache (sharedBytes, exclusiveBytes) from a deployment profile;
// component-level capacity requirements override profile-level requirements:
// if any component specifies l3 cache requirements, only component requirements
// are used; otherwise, profile-level requirements are used as a fallback
func computeRequestedL3CacheRequirements(profile *margoNonStdAPI.DeploymentExecutionProfile) (int64, int64, error) {
	if profile == nil {
		return 0, 0, nil
	}

	var totalSharedBytes, totalExclusiveBytes int64
	for _, component := range profile.Components {
		depComp, err := component.AsDeploymentProfileComponent()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse deployment profile component: %w", err)
		}
		compSharedBytes, compExclusiveBytes, err := processL3CacheRequirements(depComp.Name, depComp.DeviceConstraints)
		if err != nil {
			return 0, 0, err
		}
		if compSharedBytes > totalSharedBytes {
			totalSharedBytes = compSharedBytes
		}
		totalExclusiveBytes += compExclusiveBytes
	}

	if totalSharedBytes > 0 || totalExclusiveBytes > 0 {
		return totalSharedBytes, totalExclusiveBytes, nil
	}

	return processL3CacheRequirements(profileComponentName, profile.DeviceConstraints)
}

func processL3CacheRequirements(componentName string, constraints *margoNonStdAPI.DeviceConstraints) (int64, int64, error) {
	if constraints == nil || constraints.CapacityRequirements == nil || constraints.CapacityRequirements.Cache == nil {
		return 0, 0, nil
	}

	var sharedBytes, exclusiveBytes int64
	for _, cacheReq := range *constraints.CapacityRequirements.Cache {
		if cacheReq.Size == nil {
			return 0, 0, fmt.Errorf("missing cache size for component %q", componentName)
		}
		if cacheReq.Level != margoNonStdAPI.L3 {
			return 0, 0, fmt.Errorf("component %q requests unsupported cache level %q (only l3 is supported)",
				componentName,
				cacheReq.Level,
			)
		}

		requiredBytes, err := parseBinarySizeBytes(*cacheReq.Size)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid cache size for component %q: %w",
				componentName,
				err,
			)
		}

		switch normalizeCacheAllocation(cacheReq.Allocation) {
		case margoNonStdAPI.DeploymentCacheRequirementAllocationShared:
			if requiredBytes > sharedBytes {
				sharedBytes = requiredBytes
			}
		case margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive:
			exclusiveBytes += requiredBytes
		default:
			return 0, 0, fmt.Errorf("component %q requests unsupported cache allocation %q (expected shared or exclusive)",
				componentName,
				cacheReq.Allocation,
			)
		}
	}
	return sharedBytes, exclusiveBytes, nil
}

func validateDeviceL3CacheSupport(capabilities *margoStdAPI.DeviceCapabilitiesManifest, sharedBytes, exclusiveBytes int64) (int64, error) {
	if !hasDeviceCacheCapabilities(capabilities) {
		return 0, fmt.Errorf("deployment requests cache but device has no l3 cache capability")
	}

	var totalBytes int64
	var supportsShared, supportsExclusive bool

	for _, cache := range *capabilities.Properties.Cache {
		if cache.Level != margoStdAPI.DeviceCapabilitiesManifestPropertiesCacheLevelL3 {
			continue
		}
		bytes, err := parseBinarySizeBytes(cache.Size)
		if err != nil {
			return 0, fmt.Errorf("invalid device cache size %q: %w", cache.Size, err)
		}
		totalBytes += bytes

		for _, a := range cache.AllocationTypes {
			if strings.EqualFold(string(a), string(margoNonStdAPI.DeploymentCacheRequirementAllocationShared)) {
				supportsShared = true
			}
			if strings.EqualFold(string(a), string(margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive)) {
				supportsExclusive = true
			}
		}
	}

	if totalBytes == 0 {
		return 0, fmt.Errorf("deployment requests cache but device has no l3 cache capability")
	}
	if sharedBytes > 0 && !supportsShared {
		return 0, fmt.Errorf("device does not support %q cache allocation on l3", margoNonStdAPI.DeploymentCacheRequirementAllocationShared)
	}
	if exclusiveBytes > 0 && !supportsExclusive {
		return 0, fmt.Errorf("device does not support %q cache allocation on l3", margoNonStdAPI.DeploymentCacheRequirementAllocationExclusive)
	}

	return totalBytes, nil
}

func hasDeviceCacheCapabilities(capabilities *margoStdAPI.DeviceCapabilitiesManifest) bool {
	return capabilities != nil && capabilities.Properties.Cache != nil
}

func computeAllocatedExclusiveL3Bytes(allocations []DeploymentAllocation) (int64, error) {
	var total int64
	for _, alloc := range allocations {
		if alloc.ExclusiveL3CacheBytesRequested < 0 {
			return 0, fmt.Errorf("invalid stored exclusive l3 cache bytes %d for deployment %q",
				alloc.ExclusiveL3CacheBytesRequested,
				alloc.DeploymentID,
			)
		}

		total += alloc.ExclusiveL3CacheBytesRequested
	}

	return total, nil
}

func normalizeCacheAllocation(allocation margoNonStdAPI.DeploymentCacheRequirementAllocation) margoNonStdAPI.DeploymentCacheRequirementAllocation {
	return margoNonStdAPI.DeploymentCacheRequirementAllocation(strings.ToLower(strings.TrimSpace(string(allocation))))
}

func parseBinarySizeBytes(raw string) (int64, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if normalized == "" {
		return 0, fmt.Errorf("size is empty")
	}

	upper := strings.ToUpper(normalized)

	for _, unit := range binarySizeUnitMultipliers {
		if !strings.HasSuffix(upper, unit.suffix) {
			continue
		}

		numberPart := normalized[:len(normalized)-len(unit.suffix)]
		if numberPart == "" {
			return 0, fmt.Errorf("missing numeric value in %q", raw)
		}
		value, err := strconv.ParseFloat(numberPart, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, fmt.Errorf("invalid numeric value %q", numberPart)
		}
		if value <= 0 {
			return 0, fmt.Errorf("size must be > 0 in %q", raw)
		}
		scaled := math.Ceil(value * unit.multiplier)
		if math.IsInf(scaled, 0) || scaled >= math.Exp2(63) {
			return 0, fmt.Errorf("size overflows int64 in %q", raw)
		}
		return int64(scaled), nil
	}

	return 0, fmt.Errorf("unsupported size unit in %q (expected B, KB, MB, GB, TB, Ki, Mi, Gi, Ti, KiB, MiB, GiB, or TiB)", raw)
}
