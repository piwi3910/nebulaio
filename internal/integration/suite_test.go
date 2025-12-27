// Package integration provides end-to-end integration tests for NebulaIO.
// These tests verify that all components work together correctly.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// IntegrationTestSuite is the base test suite for integration tests.
type IntegrationTestSuite struct {
	suite.Suite
	ctx       context.Context
	cancel    context.CancelFunc
	dataDir   string
	adminURL  string
	s3URL     string
	authToken string
}

// SetupSuite runs before all tests in the suite.
func (s *IntegrationTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 5*time.Minute)

	// Create temporary data directory
	var err error
	s.dataDir, err = os.MkdirTemp("", "nebulaio-integration-*")
	require.NoError(s.T(), err)

	// Note: In a full implementation, we would start the server here
	// For now, these tests are structured for running against a live server
	s.adminURL = getEnvOrDefault("NEBULAIO_ADMIN_URL", "http://localhost:9001")
	s.s3URL = getEnvOrDefault("NEBULAIO_S3_URL", "http://localhost:9000")
}

// TearDownSuite runs after all tests in the suite.
func (s *IntegrationTestSuite) TearDownSuite() {
	s.cancel()
	if s.dataDir != "" {
		os.RemoveAll(s.dataDir)
	}
}

// SetupTest runs before each test.
func (s *IntegrationTestSuite) SetupTest() {
	// Authenticate before each test if not already authenticated
	if s.authToken == "" {
		s.authenticate()
	}
}

func (s *IntegrationTestSuite) authenticate() {
	body := map[string]string{
		"username": getEnvOrDefault("NEBULAIO_TEST_USER", "admin"),
		"password": getEnvOrDefault("NEBULAIO_TEST_PASSWORD", "Admin123"),
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(
		s.adminURL+"/api/v1/admin/auth/login",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.T().Skipf("Authentication failed with status %d", resp.StatusCode)
		return
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	s.authToken = result.AccessToken
}

func (s *IntegrationTestSuite) makeAuthenticatedRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(s.ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	return http.DefaultClient.Do(req)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// TestIntegrationSuite runs the integration test suite.
func TestIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(IntegrationTestSuite))
}

// AuthTestSuite tests authentication flows.
type AuthTestSuite struct {
	IntegrationTestSuite
}

func (s *AuthTestSuite) TestLoginWithValidCredentials() {
	body := map[string]string{
		"username": "admin",
		"password": "Admin123",
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(
		s.adminURL+"/api/v1/admin/auth/login",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	s.Contains(result, "access_token")
	s.Contains(result, "refresh_token")
}

func (s *AuthTestSuite) TestLoginWithInvalidCredentials() {
	body := map[string]string{
		"username": "admin",
		"password": "wrongpassword",
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(
		s.adminURL+"/api/v1/admin/auth/login",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestProtectedEndpointWithoutToken() {
	resp, err := http.Get(s.adminURL + "/api/v1/admin/users")
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusUnauthorized, resp.StatusCode)
}

func (s *AuthTestSuite) TestProtectedEndpointWithToken() {
	resp, err := s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/users", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
}

func TestAuthSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(AuthTestSuite))
}

// BucketTestSuite tests bucket operations.
type BucketTestSuite struct {
	IntegrationTestSuite
}

func (s *BucketTestSuite) TestBucketLifecycle() {
	bucketName := fmt.Sprintf("test-bucket-%d", time.Now().UnixNano())

	// Create bucket
	createBody := map[string]string{"name": bucketName}
	jsonBody, _ := json.Marshal(createBody)

	resp, err := s.makeAuthenticatedRequest(
		"POST",
		s.adminURL+"/api/v1/admin/buckets",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	resp.Body.Close()
	s.Equal(http.StatusCreated, resp.StatusCode)

	// List buckets and verify
	resp, err = s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/buckets", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()
	s.Equal(http.StatusOK, resp.StatusCode)

	var listResult struct {
		Buckets []struct {
			Name string `json:"name"`
		} `json:"buckets"`
	}
	json.NewDecoder(resp.Body).Decode(&listResult)

	found := false
	for _, b := range listResult.Buckets {
		if b.Name == bucketName {
			found = true
			break
		}
	}
	s.True(found, "Created bucket should appear in list")

	// Delete bucket
	resp, err = s.makeAuthenticatedRequest(
		"DELETE",
		s.adminURL+"/api/v1/admin/buckets/"+bucketName,
		nil,
	)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	resp.Body.Close()
	s.Equal(http.StatusNoContent, resp.StatusCode)
}

func TestBucketSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(BucketTestSuite))
}

// ClusterTestSuite tests cluster operations.
type ClusterTestSuite struct {
	IntegrationTestSuite
}

func (s *ClusterTestSuite) TestClusterStatus() {
	resp, err := s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/cluster/status", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	s.Contains(result, "status")
}

func (s *ClusterTestSuite) TestListNodes() {
	resp, err := s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/cluster/nodes", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result struct {
		Nodes []interface{} `json:"nodes"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	s.NotNil(result.Nodes)
}

func TestClusterSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(ClusterTestSuite))
}

// AIMLTestSuite tests AI/ML feature operations.
type AIMLTestSuite struct {
	IntegrationTestSuite
}

func (s *AIMLTestSuite) TestGetAIMLStatus() {
	resp, err := s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/aiml/status", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result struct {
		Features []struct {
			Name      string `json:"name"`
			Enabled   bool   `json:"enabled"`
			Available bool   `json:"available"`
		} `json:"features"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	s.NotEmpty(result.Features)

	// Verify expected features exist
	expectedFeatures := []string{"s3_express", "iceberg", "mcp", "gpudirect", "dpu", "rdma", "nim"}
	featureNames := make(map[string]bool)
	for _, f := range result.Features {
		featureNames[f.Name] = true
	}

	for _, expected := range expectedFeatures {
		s.True(featureNames[expected], "Feature %s should be present", expected)
	}
}

func (s *AIMLTestSuite) TestGetAIMLMetrics() {
	resp, err := s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/aiml/metrics", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	// Verify all feature metrics are present
	s.Contains(result, "s3_express")
	s.Contains(result, "iceberg")
	s.Contains(result, "mcp")
	s.Contains(result, "gpudirect")
	s.Contains(result, "dpu")
	s.Contains(result, "rdma")
	s.Contains(result, "nim")
}

func (s *AIMLTestSuite) TestGetConfig() {
	resp, err := s.makeAuthenticatedRequest("GET", s.adminURL+"/api/v1/admin/config", nil)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	// Verify configuration structure
	s.Contains(result, "s3_express")
	s.Contains(result, "iceberg")
	s.Contains(result, "mcp")
}

func (s *AIMLTestSuite) TestUpdateConfig() {
	config := map[string]interface{}{
		"s3_express": map[string]interface{}{
			"enabled":      true,
			"default_zone": "use1-az1",
		},
	}
	jsonBody, _ := json.Marshal(config)

	resp, err := s.makeAuthenticatedRequest(
		"PUT",
		s.adminURL+"/api/v1/admin/config",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	s.Equal("ok", result["status"])
}

func TestAIMLSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(AIMLTestSuite))
}

// HealthTestSuite tests health check endpoints.
type HealthTestSuite struct {
	IntegrationTestSuite
}

func (s *HealthTestSuite) TestHealthEndpoint() {
	resp, err := http.Get(s.adminURL + "/health")
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *HealthTestSuite) TestReadyEndpoint() {
	resp, err := http.Get(s.adminURL + "/health/ready")
	if err != nil {
		s.T().Skipf("Server not available: %v", err)
		return
	}
	defer resp.Body.Close()

	// Should return OK if server is ready
	s.True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusServiceUnavailable)
}

func TestHealthSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	suite.Run(t, new(HealthTestSuite))
}

// Used for testing without a running server
type MockServerTestSuite struct {
	suite.Suite
	server *httptest.Server
}

func (s *MockServerTestSuite) SetupSuite() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/admin/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)

		if body["username"] == "admin" && body["password"] == "Admin123" {
			json.NewEncoder(w).Encode(map[string]string{
				"access_token":  "test-token",
				"refresh_token": "test-refresh-token",
			})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		}
	})

	s.server = httptest.NewServer(mux)
}

func (s *MockServerTestSuite) TearDownSuite() {
	s.server.Close()
}

func (s *MockServerTestSuite) TestMockHealth() {
	resp, err := http.Get(s.server.URL + "/health")
	s.NoError(err)
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
}

func (s *MockServerTestSuite) TestMockLogin() {
	body := map[string]string{
		"username": "admin",
		"password": "Admin123",
	}
	jsonBody, _ := json.Marshal(body)

	resp, err := http.Post(
		s.server.URL+"/api/v1/admin/auth/login",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	s.NoError(err)
	defer resp.Body.Close()

	s.Equal(http.StatusOK, resp.StatusCode)
}

func TestMockServerSuite(t *testing.T) {
	suite.Run(t, new(MockServerTestSuite))
}

// Helper function for creating test files
func createTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}
