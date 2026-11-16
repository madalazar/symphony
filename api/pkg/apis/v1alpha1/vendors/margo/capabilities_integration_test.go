package margo

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sym_mgr "github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/margo"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2"
	coa_ctx "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states/memorystate"
	"github.com/ghodss/yaml"
	nbiModels "github.com/margo/sandbox/non-standard/generatedCode/wfm/nbi"
	wfmCli "github.com/margo/sandbox/poc/wfm/cli"
	p "github.com/margo/sandbox/shared-lib/pointers"
	sbiModels "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

type certAuthorityFixture struct {
	caCert          *x509.Certificate
	caKey           *ecdsa.PrivateKey
	caPool          *x509.CertPool
	serverTLSConfig *tls.Config
}

type clientCertFixture struct {
	clientTLSConfig *tls.Config
	clientSpiffeID  string
}

func generateMTLSAuthority(t *testing.T) certAuthorityFixture {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName:   "Margo Test Root CA",
			Organization: []string{"Margo Organization"},
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	require.NoError(t, err)

	serverTLSCert := tls.Certificate{
		Certificate: [][]byte{serverDER},
		PrivateKey:  serverKey,
	}

	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	return certAuthorityFixture{
		caCert:          caCert,
		caKey:           caKey,
		caPool:          caPool,
		serverTLSConfig: serverTLSConfig,
	}
}

func (ca certAuthorityFixture) issueClientCert(t *testing.T, serial int64, clientID string) clientCertFixture {
	t.Helper()

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	clientSpiffeID := "spiffe://margo.org/margo/wfm/symphony-01/client/" + clientID
	clientURI, err := url.Parse(clientSpiffeID)
	require.NoError(t, err)

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject: pkix.Name{
			CommonName: clientID,
		},
		URIs:                  []*url.URL{clientURI},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, ca.caCert, &clientKey.PublicKey, ca.caKey)
	require.NoError(t, err)

	clientTLSCert := tls.Certificate{
		Certificate: [][]byte{clientDER},
		PrivateKey:  clientKey,
	}

	clientTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{clientTLSCert},
		RootCAs:      ca.caPool,
	}

	return clientCertFixture{
		clientTLSConfig: clientTLSConfig,
		clientSpiffeID:  clientSpiffeID,
	}
}

type mockTLSConn struct {
	net.Conn
	state tls.ConnectionState
}

func (m *mockTLSConn) Handshake() error {
	return nil
}

func (m *mockTLSConn) ConnectionState() tls.ConnectionState {
	return m.state
}

func setupDeviceAgentVendor(t *testing.T) (*DeviceAgentVendor, *sym_mgr.DeviceManager) {
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

	db := sym_mgr.NewMargoDatabase(mContext, "device-manager", stateProvider)
	deviceManager := &sym_mgr.DeviceManager{
		Database:      db,
		StateProvider: stateProvider,
	}

	vendor := &DeviceAgentVendor{
		DeviceManager: deviceManager,
	}

	return vendor, deviceManager
}

func findCapabilitiesPutEndpoint(t *testing.T, vendor *DeviceAgentVendor) v1alpha2.Endpoint {
	t.Helper()
	endpoints := vendor.GetEndpoints()
	for _, ep := range endpoints {
		for _, m := range ep.Methods {
			if m == fasthttp.MethodPut && strings.HasSuffix(ep.Route, "/capabilities/{deviceId}") {
				return ep
			}
		}
	}
	t.Fatalf("PUT capabilities endpoint not registered on DeviceAgentVendor")
	return v1alpha2.Endpoint{}
}

func TestCapabilitiesHTTPIntegration_Loopback(t *testing.T) {
	// Ensure NewSbiHTTPClient's local "data/cache" directory is created inside a temporary directory
	t.Chdir(t.TempDir())

	caFixture := generateMTLSAuthority(t)
	vendor, deviceMgr := setupDeviceAgentVendor(t)
	putCapEndpoint := findCapabilitiesPutEndpoint(t, vendor)

	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const pathPrefix = "/api/v1/capabilities/"
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, pathPrefix) || len(r.URL.Path) <= len(pathPrefix) {
			http.Error(w, "method or path not allowed", http.StatusMethodNotAllowed)
			return
		}
		targetDeviceID := strings.TrimPrefix(r.URL.Path, pathPrefix)

		var fhCtx fasthttp.RequestCtx
		fhCtx.Request.Header.SetMethod(r.Method)
		fhCtx.Request.SetRequestURI(r.RequestURI)

		if r.TLS != nil {
			mConn := &mockTLSConn{state: *r.TLS}
			fhCtx.Init2(mConn, nil, false)
		}

		ctxWithFH := context.WithValue(r.Context(), v1alpha2.COAFastHTTPContextKey, &fhCtx)

		var body []byte
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		coaReq := v1alpha2.COARequest{
			Method:  r.Method,
			Route:   r.URL.Path,
			Context: ctxWithFH,
			Body:    body,
			Parameters: map[string]string{
				"__deviceId": targetDeviceID,
			},
		}

		coaResp := putCapEndpoint.Handler(coaReq)

		for k, v := range coaResp.Metadata {
			w.Header().Set(k, v)
		}
		if coaResp.ContentType != "" {
			w.Header().Set("Content-Type", coaResp.ContentType)
		}

		statusCode := http.StatusOK
		switch coaResp.State {
		case v1alpha2.OK:
			statusCode = http.StatusOK
		case v1alpha2.Created:
			statusCode = http.StatusCreated
		case v1alpha2.Accepted:
			statusCode = http.StatusAccepted
		case v1alpha2.BadRequest:
			statusCode = http.StatusBadRequest
		case v1alpha2.Unauthorized:
			statusCode = http.StatusUnauthorized
		case v1alpha2.NotFound:
			statusCode = http.StatusNotFound
		default:
			statusCode = http.StatusInternalServerError
		}

		w.WriteHeader(statusCode)
		_, _ = w.Write(coaResp.Body)
	})

	server := httptest.NewUnstartedServer(httpHandler)
	server.TLS = caFixture.serverTLSConfig
	server.StartTLS()
	defer server.Close()

	testCases := []struct {
		name           string
		serial         int64
		deviceID       string
		modelNumber    string
		serialNumber   string
		cores          float32
		baseMHz        float32
		maxMHz         float32
		cacheAllocs    []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes
		memBandwidth   []sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes
		deploymentType sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes
	}{
		{
			name:         "2.2.A Compose Device",
			serial:       3,
			deviceID:     "compose-device-rt-01",
			modelNumber:  "RT-Box-Compose",
			serialNumber: "SN-COMPOSE-123",
			cores:        4.0,
			baseMHz:      2800.0,
			maxMHz:       5000.0,
			cacheAllocs: []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
				sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
			},
			memBandwidth: []sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
				sbiModels.PercentThrottling,
			},
			deploymentType: sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose,
		},
		{
			name:         "2.2.B Helm Device",
			serial:       4,
			deviceID:     "helm-device-rt-01",
			modelNumber:  "RT-Box-Helm",
			serialNumber: "SN-HELM-456",
			cores:        8.0,
			baseMHz:      3200.0,
			maxMHz:       5200.0,
			cacheAllocs: []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
				sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
				sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
			},
			memBandwidth: []sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
				sbiModels.RateLimiting,
			},
			deploymentType: sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clientCert := caFixture.issueClientCert(t, tc.serial, tc.deviceID)
			customHTTPClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: clientCert.clientTLSConfig,
				},
			}

			sbiClient, err := wfmCli.NewSbiHTTPClient(
				server.URL,
				tc.deviceID,
				sbiModels.WithHTTPClient(customHTTPClient),
			)
			require.NoError(t, err)

			deviceCaps := buildContractTestDevice(tc.deviceID, "Industrial Edge", tc.modelNumber, tc.serialNumber)
			withContractCPU(
				deviceCaps,
				sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64,
				sbiModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance,
				sbiModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated,
				tc.cores,
				tc.baseMHz,
				tc.maxMHz,
			)
			withContractCache(
				deviceCaps,
				sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
				tc.cacheAllocs,
				"9216Ki",
			)
			withContractMemory(deviceCaps, "1024Mi", tc.memBandwidth)
			deviceCaps.Properties.SupportedDeploymentTypes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
				tc.deploymentType,
			}
			deviceCaps.Properties.SupportedRuntimes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
				sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
			}

			err = sbiClient.ReportCapabilities(context.Background(), tc.deviceID, *deviceCaps)
			require.NoError(t, err, "ReportCapabilities should return HTTP 201/200")

			storedCaps, err := deviceMgr.GetDeviceCapabilities(context.Background(), clientCert.clientSpiffeID)
			require.NoError(t, err)
			require.NotNil(t, storedCaps)

			assert.Equal(t, tc.deviceID, storedCaps.Properties.Id)
			assert.Equal(t, tc.modelNumber, storedCaps.Properties.ModelNumber)

			require.NotNil(t, storedCaps.Properties.Cpus)
			require.Len(t, *storedCaps.Properties.Cpus, 1)
			storedCPU := (*storedCaps.Properties.Cpus)[0]
			assert.Equal(t, sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64, *storedCPU.Architecture)
			assert.Equal(t, sbiModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated, *storedCPU.Type)
			assert.Equal(t, tc.cores, storedCPU.Cores)

			require.NotNil(t, storedCaps.Properties.Cache)
			require.Len(t, *storedCaps.Properties.Cache, 1)
			storedCache := (*storedCaps.Properties.Cache)[0]
			assert.Equal(t, sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3, storedCache.Level)
			assert.Equal(t, "9216Ki", storedCache.Size)
			assert.Contains(t, storedCache.AllocationTypes, sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive)

			require.NotNil(t, storedCaps.Properties.Memory)
			assert.Equal(t, "1024Mi", storedCaps.Properties.Memory.Size)

			require.NotNil(t, storedCaps.Properties.SupportedDeploymentTypes)
			assert.Contains(t, *storedCaps.Properties.SupportedDeploymentTypes, tc.deploymentType)
		})
	}
}

func TestDeviceMgmtVendor_ListDevices_AppPackageEligibility(t *testing.T) {
	ctx := context.Background()
	_, deviceMgr := setupDeviceAgentVendor(t)
	mgmtVendor := &DeviceMgmtVendor{
		DeviceManager: deviceMgr,
	}

	wsRoot := vendorWorkspaceRoot(t)
	loadAppDesc := func(fileName string) *nbiModels.AppDescription {
		rawYAML, err := os.ReadFile(filepath.Join(wsRoot, "symphony", "api", "pkg", "apis", "v1alpha1", "managers", "margo", "testdata", fileName))
		require.NoError(t, err)
		jsonBytes, err := yaml.YAMLToJSON(rawYAML)
		require.NoError(t, err)
		var desc nbiModels.AppDescription
		require.NoError(t, json.Unmarshal(jsonBytes, &desc))
		return &desc
	}

	expDesc := loadAppDesc("rt-benchmark-experiment-margo.yaml")
	icoreDesc := loadAppDesc("rt-benchmark-icore-margo.yaml")

	require.NoError(t, deviceMgr.Database.UpsertAppPackage(ctx, sym_mgr.AppPackageDatabaseRow{
		PackageRequest: nbiModels.ApplicationPackageManifestResp{
			Id:       expDesc.Id,
			Metadata: nbiModels.Metadata{Name: expDesc.Metadata.Name},
		},
		AppDescription: expDesc,
	}))
	require.NoError(t, deviceMgr.Database.UpsertAppPackage(ctx, sym_mgr.AppPackageDatabaseRow{
		PackageRequest: nbiModels.ApplicationPackageManifestResp{
			Id:       icoreDesc.Id,
			Metadata: nbiModels.Metadata{Name: icoreDesc.Metadata.Name},
		},
		AppDescription: icoreDesc,
	}))

	buildDev := func(id string, depType sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes, memSize string) *sbiModels.DeviceCapabilitiesManifest {
		d := buildContractTestDevice(id, "Industrial Edge", "RT-Node", "SN-"+id)
		withContractCPU(
			d,
			sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64,
			sbiModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance,
			sbiModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated,
			4.0,
			2400.0,
			4800.0,
		)
		withContractCache(
			d,
			sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
			[]sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
				sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
				sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
			},
			"9216Ki",
		)
		withContractMemory(d, memSize, []sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{sbiModels.PercentThrottling})
		d.Properties.Storage = p.Ptr("64Gi")
		d.Properties.SupportedDeploymentTypes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{depType}
		d.Properties.SupportedRuntimes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
			sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
		}
		return d
	}

	require.NoError(t, deviceMgr.Database.UpsertDevice(ctx, sym_mgr.DeviceDatabaseRow{
		DeviceClientId:   "helm-dev-01",
		DeviceId:         "helm-dev-01",
		OnboardingStatus: nbiModels.DeviceOnboardStatusONBOARDED,
		Capabilities:     buildDev("helm-dev-01", sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm, "2048Mi"),
	}))
	require.NoError(t, deviceMgr.Database.UpsertDevice(ctx, sym_mgr.DeviceDatabaseRow{
		DeviceClientId:   "compose-dev-01",
		DeviceId:         "compose-dev-01",
		OnboardingStatus: nbiModels.DeviceOnboardStatusONBOARDED,
		Capabilities:     buildDev("compose-dev-01", sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose, "2048Mi"),
	}))
	require.NoError(t, deviceMgr.Database.UpsertDevice(ctx, sym_mgr.DeviceDatabaseRow{
		DeviceClientId:   "lowmem-dev-01",
		DeviceId:         "lowmem-dev-01",
		OnboardingStatus: nbiModels.DeviceOnboardStatusONBOARDED,
		Capabilities:     buildDev("lowmem-dev-01", sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm, "256Mi"),
	}))

	decodeDeviceMap := func(resp v1alpha2.COAResponse) map[string]nbiModels.DeviceManifestResp {
		require.Equal(t, v1alpha2.OK, resp.State)
		var listResp nbiModels.DeviceListResp
		require.NoError(t, json.Unmarshal(resp.Body, &listResp))
		byID := make(map[string]nbiModels.DeviceManifestResp, len(listResp.Items))
		for _, item := range listResp.Items {
			require.NotNil(t, item.Id)
			byID[*item.Id] = item
		}
		return byID
	}

	t.Run("Multi-profile package (rt-benchmark-experiment-suite) marks both helm and compose devices eligible", func(t *testing.T) {
		resp := mgmtVendor.listDevices(v1alpha2.COARequest{
			Method:  fasthttp.MethodGet,
			Route:   "/devices",
			Context: ctx,
			Parameters: map[string]string{
				"appPackageId": *expDesc.Id,
			},
		})
		byID := decodeDeviceMap(resp)
		require.NotNil(t, byID["helm-dev-01"].Eligible)
		assert.Equal(t, nbiModels.True, *byID["helm-dev-01"].Eligible, "helm device must be eligible for multi-profile experiment suite")
		require.NotNil(t, byID["compose-dev-01"].Eligible)
		assert.Equal(t, nbiModels.True, *byID["compose-dev-01"].Eligible, "compose device must be eligible for multi-profile experiment suite")
		require.NotNil(t, byID["lowmem-dev-01"].Eligible)
		assert.Equal(t, nbiModels.False, *byID["lowmem-dev-01"].Eligible, "insufficient memory device must be ineligible")
	})

	t.Run("Helm-only package (rt-benchmark-icore-suite) marks compose-only device ineligible", func(t *testing.T) {
		resp := mgmtVendor.listDevices(v1alpha2.COARequest{
			Method:  fasthttp.MethodGet,
			Route:   "/devices",
			Context: ctx,
			Parameters: map[string]string{
				"appPackageId": *icoreDesc.Id,
			},
		})
		byID := decodeDeviceMap(resp)
		require.NotNil(t, byID["compose-dev-01"].Eligible)
		assert.Equal(t, nbiModels.False, *byID["compose-dev-01"].Eligible, "compose-only device must be ineligible for helm-only package")
	})
}

