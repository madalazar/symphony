package margo

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	sym_mgr "github.com/eclipse-symphony/symphony/api/pkg/apis/v1alpha1/managers/margo"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2"
	coa_ctx "github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/contexts"
	"github.com/eclipse-symphony/symphony/coa/pkg/apis/v1alpha2/providers/states/memorystate"
	wfmCli "github.com/margo/sandbox/poc/wfm/cli"
	sbiModels "github.com/margo/sandbox/standard/generatedCode/wfm/sbi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

type certFixture struct {
	serverTLSConfig *tls.Config
	clientTLSConfig *tls.Config
	clientSpiffeID  string
}

func generateMTLSFixture(t *testing.T) certFixture {
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
		DNSNames:              []string{"localhost", "127.0.0.1"},
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

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	clientSpiffeID := "spiffe://margo.org/margo/wfm/symphony-01/client/device-client-01"
	clientURI, err := url.Parse(clientSpiffeID)
	require.NoError(t, err)

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "margo-client",
		},
		URIs:                  []*url.URL{clientURI},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	clientDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	clientTLSCert := tls.Certificate{
		Certificate: [][]byte{clientDER},
		PrivateKey:  clientKey,
	}

	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{serverTLSCert},
		ClientAuth:   tls.RequireAnyClientCert,
		ClientCAs:    caPool,
	}

	clientTLSConfig := &tls.Config{
		Certificates:       []tls.Certificate{clientTLSCert},
		RootCAs:            caPool,
		InsecureSkipVerify: true,
	}

	return certFixture{
		serverTLSConfig: serverTLSConfig,
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

func TestCapabilitiesHTTPIntegration_Loopback(t *testing.T) {
	fixture := generateMTLSFixture(t)
	vendor, deviceMgr := setupDeviceAgentVendor(t)

	// Create an HTTP handler bridge that runs the DeviceAgentVendor's updateDeviceCapabilities handler
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		const pathPrefix = "/api/v1/capabilities/"
		if !assert.True(t, len(path) > len(pathPrefix), "request path too short: %s", path) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		targetDeviceID := path[len(pathPrefix):]

		var fhCtx fasthttp.RequestCtx
		fhCtx.Request.Header.SetMethod(r.Method)
		fhCtx.Request.SetRequestURI(r.RequestURI)

		if r.TLS != nil {
			mConn := &mockTLSConn{state: *r.TLS}
			fhCtx.Init2(mConn, nil, false)
		}

		ctxWithFH := context.WithValue(r.Context(), v1alpha2.COAFastHTTPContextKey, &fhCtx)

		body := make([]byte, 0)
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(r.Body)
			require.NoError(t, err)
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

		coaResp := vendor.updateDeviceCapabilities(coaReq)

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
		w.Write(coaResp.Body)
	})

	server := httptest.NewUnstartedServer(httpHandler)
	server.TLS = fixture.serverTLSConfig
	server.StartTLS()
	defer server.Close()

	customHTTPClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: fixture.clientTLSConfig,
		},
	}

	sbiClient, err := wfmCli.NewSbiHTTPClient(
		server.URL,
		"compose-device-rt-01",
		sbiModels.WithHTTPClient(customHTTPClient),
	)
	require.NoError(t, err)

	t.Run("2.2.A Compose Device", func(t *testing.T) {
		deviceId := "compose-device-rt-01"

		deviceCaps := sbiModels.DeviceCapabilitiesManifest{}
		deviceCaps.Properties.Id = deviceId
		deviceCaps.Properties.Vendor = "Industrial Edge"
		deviceCaps.Properties.ModelNumber = "RT-Box-Compose"
		deviceCaps.Properties.SerialNumber = "SN-COMPOSE-123"

		arch := sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64
		cpuClass := sbiModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance
		cpuType := sbiModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated
		baseMHz := float32(2800.0)
		maxMHz := float32(5000.0)

		deviceCaps.Properties.Cpus = &[]struct {
			Architecture *sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitecture `json:"architecture,omitempty"`
			Class        *sbiModels.DeviceCapabilitiesManifestPropertiesCpusClass        `json:"class,omitempty"`
			Cores        float32                                                         `json:"cores"`
			Frequency    *struct {
				BaseMHz *float32 `json:"baseMHz,omitempty"`
				MaxMHz  *float32 `json:"maxMHz,omitempty"`
			} `json:"frequency,omitempty"`
			Type *sbiModels.DeviceCapabilitiesManifestPropertiesCpusType `json:"type,omitempty"`
		}{
			{
				Architecture: &arch,
				Class:        &cpuClass,
				Type:         &cpuType,
				Cores:        4.0,
				Frequency: &struct {
					BaseMHz *float32 `json:"baseMHz,omitempty"`
					MaxMHz  *float32 `json:"maxMHz,omitempty"`
				}{
					BaseMHz: &baseMHz,
					MaxMHz:  &maxMHz,
				},
			},
		}

		deviceCaps.Properties.Cache = &[]struct {
			AllocationTypes []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes `json:"allocationTypes"`
			Level           sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevel             `json:"level"`
			Size            string                                                               `json:"size"`
		}{
			{
				Level: sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
				AllocationTypes: []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
					sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
				},
				Size: "9216Ki",
			},
		}

		deviceCaps.Properties.Memory = &struct {
			BandwidthAllocationTypes *[]sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes `json:"bandwidthAllocationTypes,omitempty"`
			Size                     string                                                                  `json:"size"`
		}{
			Size: "1024Mi",
			BandwidthAllocationTypes: &[]sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
				sbiModels.PercentThrottling,
			},
		}

		deviceCaps.Properties.SupportedDeploymentTypes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
			sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose,
		}
		deviceCaps.Properties.SupportedRuntimes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
			sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
		}

		err = sbiClient.ReportCapabilities(context.Background(), deviceId, deviceCaps)
		require.NoError(t, err, "ReportCapabilities should return HTTP 201/200")

		storedCaps, err := deviceMgr.GetDeviceCapabilities(context.Background(), fixture.clientSpiffeID)
		require.NoError(t, err)
		require.NotNil(t, storedCaps)

		assert.Equal(t, deviceId, storedCaps.Properties.Id)
		assert.Equal(t, "RT-Box-Compose", storedCaps.Properties.ModelNumber)

		require.NotNil(t, storedCaps.Properties.Cpus)
		require.Len(t, *storedCaps.Properties.Cpus, 1)
		storedCPU := (*storedCaps.Properties.Cpus)[0]
		assert.Equal(t, sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64, *storedCPU.Architecture)
		assert.Equal(t, sbiModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated, *storedCPU.Type)
		assert.Equal(t, float32(4.0), storedCPU.Cores)

		require.NotNil(t, storedCaps.Properties.Cache)
		require.Len(t, *storedCaps.Properties.Cache, 1)
		storedCache := (*storedCaps.Properties.Cache)[0]
		assert.Equal(t, sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3, storedCache.Level)
		assert.Equal(t, "9216Ki", storedCache.Size)
		assert.Contains(t, storedCache.AllocationTypes, sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive)

		require.NotNil(t, storedCaps.Properties.Memory)
		assert.Equal(t, "1024Mi", storedCaps.Properties.Memory.Size)

		require.NotNil(t, storedCaps.Properties.SupportedDeploymentTypes)
		assert.Contains(t, *storedCaps.Properties.SupportedDeploymentTypes, sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesCompose)
	})

	t.Run("2.2.B Helm Device", func(t *testing.T) {
		deviceId := "helm-device-rt-01"

		deviceCaps := sbiModels.DeviceCapabilitiesManifest{}
		deviceCaps.Properties.Id = deviceId
		deviceCaps.Properties.Vendor = "Industrial Edge"
		deviceCaps.Properties.ModelNumber = "RT-Box-Helm"
		deviceCaps.Properties.SerialNumber = "SN-HELM-456"

		arch := sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitectureAmd64
		cpuClass := sbiModels.DeviceCapabilitiesManifestPropertiesCpusClassPerformance
		cpuType := sbiModels.DeviceCapabilitiesManifestPropertiesCpusTypeIsolated
		baseMHz := float32(3200.0)
		maxMHz := float32(5200.0)

		deviceCaps.Properties.Cpus = &[]struct {
			Architecture *sbiModels.DeviceCapabilitiesManifestPropertiesCpusArchitecture `json:"architecture,omitempty"`
			Class        *sbiModels.DeviceCapabilitiesManifestPropertiesCpusClass        `json:"class,omitempty"`
			Cores        float32                                                         `json:"cores"`
			Frequency    *struct {
				BaseMHz *float32 `json:"baseMHz,omitempty"`
				MaxMHz  *float32 `json:"maxMHz,omitempty"`
			} `json:"frequency,omitempty"`
			Type *sbiModels.DeviceCapabilitiesManifestPropertiesCpusType `json:"type,omitempty"`
		}{
			{
				Architecture: &arch,
				Class:        &cpuClass,
				Type:         &cpuType,
				Cores:        8.0,
				Frequency: &struct {
					BaseMHz *float32 `json:"baseMHz,omitempty"`
					MaxMHz  *float32 `json:"maxMHz,omitempty"`
				}{
					BaseMHz: &baseMHz,
					MaxMHz:  &maxMHz,
				},
			},
		}

		deviceCaps.Properties.Cache = &[]struct {
			AllocationTypes []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes `json:"allocationTypes"`
			Level           sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevel             `json:"level"`
			Size            string                                                               `json:"size"`
		}{
			{
				Level: sbiModels.DeviceCapabilitiesManifestPropertiesCacheLevelL3,
				AllocationTypes: []sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypes{
					sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive,
					sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesShared,
				},
				Size: "9216Ki",
			},
		}

		deviceCaps.Properties.Memory = &struct {
			BandwidthAllocationTypes *[]sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes `json:"bandwidthAllocationTypes,omitempty"`
			Size                     string                                                                  `json:"size"`
		}{
			Size: "1024Mi",
			BandwidthAllocationTypes: &[]sbiModels.DeviceCapabilitiesManifestPropertiesMemoryBandwidthAllocationTypes{
				sbiModels.RateLimiting,
			},
		}

		deviceCaps.Properties.SupportedDeploymentTypes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypes{
			sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm,
		}
		deviceCaps.Properties.SupportedRuntimes = &[]sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimes{
			sbiModels.DeviceCapabilitiesManifestPropertiesSupportedRuntimesOci,
		}

		err = sbiClient.ReportCapabilities(context.Background(), deviceId, deviceCaps)
		require.NoError(t, err, "ReportCapabilities should return HTTP 201/200")

		storedCaps, err := deviceMgr.GetDeviceCapabilities(context.Background(), fixture.clientSpiffeID)
		require.NoError(t, err)
		require.NotNil(t, storedCaps)

		assert.Equal(t, deviceId, storedCaps.Properties.Id)
		assert.Equal(t, "RT-Box-Helm", storedCaps.Properties.ModelNumber)

		require.NotNil(t, storedCaps.Properties.Cpus)
		require.Len(t, *storedCaps.Properties.Cpus, 1)
		storedCPU := (*storedCaps.Properties.Cpus)[0]
		assert.Equal(t, float32(8.0), storedCPU.Cores)

		require.NotNil(t, storedCaps.Properties.Cache)
		storedCache := (*storedCaps.Properties.Cache)[0]
		assert.Contains(t, storedCache.AllocationTypes, sbiModels.DeviceCapabilitiesManifestPropertiesCacheAllocationTypesExclusive)

		require.NotNil(t, storedCaps.Properties.Memory)
		assert.Equal(t, "1024Mi", storedCaps.Properties.Memory.Size)

		require.NotNil(t, storedCaps.Properties.SupportedDeploymentTypes)
		assert.Contains(t, *storedCaps.Properties.SupportedDeploymentTypes, sbiModels.DeviceCapabilitiesManifestPropertiesSupportedDeploymentTypesHelm)
	})
}
