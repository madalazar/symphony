package margo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	margoNonStdAPI "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rtBenchmarkMargoYAML = `apiVersion: margo.org/v1-alpha1
kind: ApplicationDescription
id: rt-benchmark-experiment-suite
metadata:
  name: RT Benchmark Experiment Suite
  description: >
    Real-time benchmarking experiment suite combining Helm and Docker Compose
    deployment profiles for the Caterpillar, cyclictest, and stress workloads.
  version: 1.0.0
  catalog:
    application:
      tagline: Real-time benchmarking via Helm and Docker Compose workloads
      descriptionFile: ./resources/description.md
      releaseNotes: ./resources/release-notes.md
      licenseFile: ./resources/license.txt
      tags: ["benchmark", "real-time", "stress-testing", "performance", "caterpillar", "cyclictest", "docker-compose", "helm"]
    organization:
      - name: Industrial Edge Controls

deploymentProfiles:
  - type: helm
    id: rt-benchmark-helm
    description: >
      Deploys Caterpillar and cyclictest latency benchmarks plus stress-ng as
      independent Helm releases on the same edge device.
    components:
      - name: caterpillar
        properties:
          repository: oci://harbor.machine:8443/library/caterpillar
          revision: 0.1.0
          wait: false
          timeout: 4h
        deviceConstraints:
          capacityRequirements:
            cpu:
              - name: caterpillar
                cores: 1
                class: performance
                type: isolated
            cache:
              - level: L3
                allocation: exclusive
                size: 9216Ki
          eligibilityRules:
            - propertySelector:
                matchExpressions:
                  - key: /supportedDeploymentTypes
                    operator: In
                    values:
                      - "helm"
                  - key: /supportedRuntimes
                    operator: In
                    values:
                      - "oci"

      - name: cyclictest
        properties:
          repository: oci://harbor.machine:8443/library/cyclictest
          revision: 0.1.0
          wait: false
          timeout: 4h
        workload:
          scheduling: real-time
          priority: 95
          nice: 10
          deadline:
            deadline: 10
            period: 16
            runtime: 5
        deviceConstraints:
          capacityRequirements:
            cpu:
              - name: cyclictest
                cores: 1
                class: performance
                type: isolated
          eligibilityRules:
            - propertySelector:
                matchExpressions:
                  - key: /supportedDeploymentTypes
                    operator: In
                    values:
                      - "helm"
                  - key: /supportedRuntimes
                    operator: In
                    values:
                      - "oci"

      - name: stressng
        properties:
          repository: oci://harbor.machine:8443/library/stressng
          revision: 0.1.0
          wait: false
          timeout: 4h

    deviceConstraints:
      capacityRequirements:
        memory: 
          size: 1024Mi
        storage: 2Gi

  - type: compose
    id: rt-benchmark-compose
    description: Deploy cyclictest and stress as separate Docker Compose components
    components:
      - name: cyclictest_compose
        properties:
          repository: oci://harbor.machine:8443/library/cyclictest-compose-archive
          revision: 0.1.0
          wait: false
        workload:
          scheduling: real-time
          priority: 95
          nice: 10
          deadline:
            deadline: 10
            period: 25
            runtime: 5
        deviceConstraints:
          capacityRequirements:
            cpu:
              - name: cyclictest_compose
                cores: 1
                class: performance
                type: isolated
          eligibilityRules:
            - propertySelector:
                matchExpressions:
                  - key: /supportedDeploymentTypes
                    operator: In
                    values:
                      - "compose"
                  - key: /supportedRuntimes
                    operator: In
                    values:
                      - "oci"

      - name: stressng_compose
        properties:
          repository: oci://harbor.machine:8443/library/stressng-compose-archive
          revision: 0.1.0
      - name: caterpillar_compose
        properties:
          repository: oci://harbor.machine:8443/library/caterpillar-compose-archive
          revision: 0.1.0
        deviceConstraints:
          capacityRequirements:
            cpu:
              - name: caterpillar_compose
                cores: 1
                class: performance
                type: isolated
            cache:
              - level: L3
                allocation: exclusive
                size: 9216Ki
          eligibilityRules:
            - propertySelector:
                matchExpressions:
                  - key: /supportedDeploymentTypes
                    operator: In
                    values:
                      - "compose"
                  - key: /supportedRuntimes
                    operator: In
                    values:
                      - "oci"
    deviceConstraints:
      capacityRequirements:
        memory: 
          size: 1024Mi
        storage: 1Gi

parameters:
  cyclictestThreads:
    value: 1
    targets:
      - pointer: CYCLICTEST_THREADS
        components: ["cyclictest_compose"]
      - pointer: cyclictest.threads
        components: ["cyclictest"]
  cyclictestPriority:
    value: 99
    targets:
      - pointer: CYCLICTEST_PRIORITY
        components: ["cyclictest_compose"]
      - pointer: cyclictest.priority
        components: ["cyclictest"]
  cyclictestInterval:
    value: 9000
    targets:
      - pointer: CYCLICTEST_INTERVAL
        components: ["cyclictest_compose"]
      - pointer: cyclictest.interval
        components: ["cyclictest"]
  cyclictestLoops:
    value: 1000000
    targets:
      - pointer: CYCLICTEST_LOOPS
        components: ["cyclictest_compose"]
      - pointer: cyclictest.loops
        components: ["cyclictest"]
  cyclictestHistogram:
    value: 600
    targets:
      - pointer: CYCLICTEST_HISTOGRAM
        components: ["cyclictest_compose"]
      - pointer: cyclictest.histogramMax
        components: ["cyclictest"]
  cyclictestCustom:
    value: -m
    targets:
      - pointer: CYCLICTEST_CUSTOM
        components: ["cyclictest_compose"]
      - pointer: cyclictest.custom
        components: ["cyclictest"]

  caterpillarSamples:
    value: 900
    targets:
      - pointer: caterpillar.samples
        components: ["caterpillar"]

  stressngMemcpy:
    value: 8
    targets:
      - pointer: STRESS_MEMCPY_WORKERS
        components: ["stressng_compose"]
      - pointer: stressng.memcpy
        components: ["stressng"]
  stressDuration:
    value: 1899
    targets:
      - pointer: STRESS_DURATION
        components: ["stressng_compose"]
      - pointer: stressng.duration
        components: ["stressng"]

configuration:
  sections:
    - name: Cyclictest Configuration
      settings:
        - parameter: cyclictestThreads
          name: Cyclictest Threads
          description: Number of cyclictest worker threads
          schema: positiveInteger
        - parameter: cyclictestPriority
          name: Cyclictest Priority
          description: Real-time priority used by cyclictest
          schema: positiveInteger
        - parameter: cyclictestInterval
          name: Cyclictest Interval (us)
          description: Sampling interval in microseconds
          schema: positiveInteger
        - parameter: cyclictestLoops
          name: Cyclictest Loops
          description: Number of benchmark loops
          schema: positiveInteger
        - parameter: cyclictestHistogram
          name: Cyclictest Histogram
          description: Histogram bucket count
          schema: positiveInteger
        - parameter: cyclictestCustom
          name: Cyclictest Extra Flags
          description: Additional flags passed through --custom
          schema: optionalText

    - name: Stress Configuration
      settings:
        - parameter: caterpillarSamples
          name: Caterpillar Samples
          description: Number of latency samples to collect for Caterpillar
          schema: positiveInteger
        - parameter: stressngMemcpy
          name: Memcpy Stressor Workers
          description: Number of --memcpy stressor worker threads
          schema: positiveInteger
        - parameter: stressDuration
          name: Stress Duration
          description: Duration for stress run (for example 300s)
          schema: requiredText

  schema:
    - name: requiredText
      dataType: string
      allowEmpty: false
      maxLength: 256

    - name: optionalText
      dataType: string
      allowEmpty: true
      maxLength: 256

    - name: positiveInteger
      dataType: integer
      allowEmpty: false
      minValue: 1

    - name: booleanValue
      dataType: boolean
      allowEmpty: false
`

func TestAppPkgParsing_RTBenchmark(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "margo-rt-app-pkg-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	margoPath := filepath.Join(tempDir, "margo.yaml")
	err = os.WriteFile(margoPath, []byte(rtBenchmarkMargoYAML), 0644)
	require.NoError(t, err)

	appPkgMgr := &AppPkgManager{}

	appDesc, resources, err := appPkgMgr.parseApplicationDescription(tempDir)
	require.NoError(t, err, "parseApplicationDescription should succeed with RT margo.yaml")
	require.NotNil(t, appDesc)
	assert.NotNil(t, resources)

	err = appPkgMgr.validateApplicationDescription(appDesc)
	require.NoError(t, err, "validateApplicationDescription should succeed")

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

	// Validate Symphony ComponentSpec types
	var helmCompSpec, composeCompSpec *ComponentSpecHelper
	for _, comp := range solution.Spec.Components {
		if comp.Name == "caterpillar" {
			helmCompSpec = &ComponentSpecHelper{Type: comp.Type, Properties: comp.Properties}
		}
		if comp.Name == "caterpillar_compose" {
			composeCompSpec = &ComponentSpecHelper{Type: comp.Type, Properties: comp.Properties}
		}
	}
	require.NotNil(t, helmCompSpec)
	assert.Equal(t, "helm.v3", helmCompSpec.Type)
	require.NotNil(t, composeCompSpec)
	assert.Equal(t, "compose", composeCompSpec.Type)
}

type ComponentSpecHelper struct {
	Type       string
	Properties map[string]interface{}
}
