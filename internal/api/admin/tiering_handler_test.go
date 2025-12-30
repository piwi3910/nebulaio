package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/tiering"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ====================
// Mock Implementation for Testing
// ====================

// MockPolicyStore implements tiering.PolicyStore for testing.
type MockPolicyStore struct {
	policies    map[string]*tiering.AdvancedPolicy
	policyStats map[string]*tiering.PolicyStats
	mu          sync.RWMutex
}

func NewMockPolicyStore() *MockPolicyStore {
	return &MockPolicyStore{
		policies:    make(map[string]*tiering.AdvancedPolicy),
		policyStats: make(map[string]*tiering.PolicyStats),
	}
}

func (m *MockPolicyStore) Create(ctx context.Context, policy *tiering.AdvancedPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.policies[policy.ID]; exists {
		return errors.New("policy already exists")
	}

	err := policy.Validate()
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	policy.Version = 1
	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()
	m.policies[policy.ID] = policy
	m.policyStats[policy.ID] = &tiering.PolicyStats{PolicyID: policy.ID}

	return nil
}

func (m *MockPolicyStore) Get(ctx context.Context, id string) (*tiering.AdvancedPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	policy, exists := m.policies[id]
	if !exists {
		return nil, errors.New("policy not found")
	}

	return policy, nil
}

func (m *MockPolicyStore) Update(ctx context.Context, policy *tiering.AdvancedPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.policies[policy.ID]
	if !exists {
		return errors.New("policy not found")
	}

	if policy.Version != 0 && policy.Version != existing.Version {
		return errors.New("version conflict")
	}

	policy.Version = existing.Version + 1
	policy.UpdatedAt = time.Now()
	m.policies[policy.ID] = policy

	return nil
}

func (m *MockPolicyStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.policies[id]; !exists {
		return errors.New("policy not found")
	}

	delete(m.policies, id)
	delete(m.policyStats, id)

	return nil
}

func (m *MockPolicyStore) List(ctx context.Context) ([]*tiering.AdvancedPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	policies := make([]*tiering.AdvancedPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		policies = append(policies, p)
	}

	return policies, nil
}

func (m *MockPolicyStore) ListByType(ctx context.Context, policyType tiering.PolicyType) ([]*tiering.AdvancedPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var policies []*tiering.AdvancedPolicy
	for _, p := range m.policies {
		if p.Type == policyType {
			policies = append(policies, p)
		}
	}

	return policies, nil
}

func (m *MockPolicyStore) ListByScope(ctx context.Context, scope tiering.PolicyScope, bucket string) ([]*tiering.AdvancedPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var policies []*tiering.AdvancedPolicy
	for _, p := range m.policies {
		if p.Scope == scope {
			policies = append(policies, p)
		}
	}

	return policies, nil
}

func (m *MockPolicyStore) GetStats(ctx context.Context, id string) (*tiering.PolicyStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, exists := m.policyStats[id]
	if !exists {
		return nil, errors.New("stats not found")
	}

	return stats, nil
}

func (m *MockPolicyStore) UpdateStats(ctx context.Context, stats *tiering.PolicyStats) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.policyStats[stats.PolicyID] = stats

	return nil
}

func (m *MockPolicyStore) SetStats(id string, stats *tiering.PolicyStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.policyStats[id] = stats
}

// MockAccessTracker implements access tracking for testing.
type MockAccessTracker struct {
	stats       map[string]*tiering.ObjectAccessStats
	hotObjects  []*tiering.ObjectAccessStats
	coldObjects []*tiering.ObjectAccessStats
	mu          sync.RWMutex
}

func NewMockAccessTracker() *MockAccessTracker {
	return &MockAccessTracker{
		stats:       make(map[string]*tiering.ObjectAccessStats),
		hotObjects:  []*tiering.ObjectAccessStats{},
		coldObjects: []*tiering.ObjectAccessStats{},
	}
}

func (m *MockAccessTracker) GetStats(ctx context.Context, bucket, key string) (*tiering.ObjectAccessStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, exists := m.stats[bucket+"/"+key]
	if !exists {
		//nolint:nilnil // mock returns nil,nil for not-found case
		return nil, nil
	}

	return stats, nil
}

func (m *MockAccessTracker) GetHotObjects(ctx context.Context, limit int) []*tiering.ObjectAccessStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.hotObjects) > limit {
		return m.hotObjects[:limit]
	}

	return m.hotObjects
}

func (m *MockAccessTracker) GetColdObjects(ctx context.Context, inactiveDays, limit int) []*tiering.ObjectAccessStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.coldObjects) > limit {
		return m.coldObjects[:limit]
	}

	return m.coldObjects
}

func (m *MockAccessTracker) SetStats(bucket, key string, stats *tiering.ObjectAccessStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stats[bucket+"/"+key] = stats
}

func (m *MockAccessTracker) SetHotObjects(objects []*tiering.ObjectAccessStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hotObjects = objects
}

func (m *MockAccessTracker) SetColdObjects(objects []*tiering.ObjectAccessStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.coldObjects = objects
}

// MockPredictiveEngine for testing predictions.
type MockPredictiveEngine struct {
	predictions     map[string]*tiering.AccessPrediction
	recommendations []*tiering.TierRecommendation
	mu              sync.RWMutex
}

func NewMockPredictiveEngine() *MockPredictiveEngine {
	return &MockPredictiveEngine{
		predictions:     make(map[string]*tiering.AccessPrediction),
		recommendations: []*tiering.TierRecommendation{},
	}
}

func (m *MockPredictiveEngine) GetPrediction(bucket, key string) *tiering.AccessPrediction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.predictions[bucket+"/"+key]
}

func (m *MockPredictiveEngine) GetRecommendations(limit int) []*tiering.TierRecommendation {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.recommendations) > limit {
		return m.recommendations[:limit]
	}

	return m.recommendations
}

func (m *MockPredictiveEngine) SetPrediction(bucket, key string, prediction *tiering.AccessPrediction) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.predictions[bucket+"/"+key] = prediction
}

func (m *MockPredictiveEngine) SetRecommendations(recs []*tiering.TierRecommendation) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.recommendations = recs
}

// MockAnomalyDetector for testing anomalies.
type MockAnomalyDetector struct {
	anomalies []*tiering.AccessAnomaly
	mu        sync.RWMutex
}

func NewMockAnomalyDetector() *MockAnomalyDetector {
	return &MockAnomalyDetector{
		anomalies: []*tiering.AccessAnomaly{},
	}
}

func (m *MockAnomalyDetector) GetAnomalies(limit int) []*tiering.AccessAnomaly {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.anomalies) > limit {
		return m.anomalies[:limit]
	}

	return m.anomalies
}

func (m *MockAnomalyDetector) SetAnomalies(anomalies []*tiering.AccessAnomaly) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.anomalies = anomalies
}

// MockTierManager for testing tier transitions.
type MockTierManager struct {
	transitionErr error
	s3Configs     map[string]*tiering.S3LifecycleConfiguration
	mu            sync.RWMutex
}

func NewMockTierManager() *MockTierManager {
	return &MockTierManager{
		s3Configs: make(map[string]*tiering.S3LifecycleConfiguration),
	}
}

func (m *MockTierManager) TransitionObject(ctx context.Context, bucket, key string, targetTier tiering.TierType) error {
	if m.transitionErr != nil {
		return m.transitionErr
	}

	return nil
}

func (m *MockTierManager) SetTransitionError(err error) {
	m.transitionErr = err
}

func (m *MockTierManager) GetS3LifecycleConfiguration(bucket string) *tiering.S3LifecycleConfiguration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	config, exists := m.s3Configs[bucket]
	if !exists {
		return &tiering.S3LifecycleConfiguration{Rules: []tiering.S3LifecycleRule{}}
	}

	return config
}

func (m *MockTierManager) SetS3LifecycleConfiguration(bucket string, config *tiering.S3LifecycleConfiguration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.s3Configs[bucket] = config
}

// ====================
// Test Handler Wrapper
// ====================

// TestTieringHandler is a test-friendly version of TieringHandler.
type TestTieringHandler struct {
	policyStore      *MockPolicyStore
	accessTracker    *MockAccessTracker
	predictiveEngine *MockPredictiveEngine
	anomalyDetector  *MockAnomalyDetector
	tierManager      *MockTierManager
}

func NewTestTieringHandler() *TestTieringHandler {
	return &TestTieringHandler{
		policyStore:      NewMockPolicyStore(),
		accessTracker:    NewMockAccessTracker(),
		predictiveEngine: NewMockPredictiveEngine(),
		anomalyDetector:  NewMockAnomalyDetector(),
		tierManager:      NewMockTierManager(),
	}
}

// RegisterTieringRoutes registers test routes using the shared registration function.
func (h *TestTieringHandler) RegisterTieringRoutes(r chi.Router) {
	RegisterTieringRoutesForHandler(r, h, "/tiering/policies")
}

// Policy handlers.
func (h *TestTieringHandler) ListTieringPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	policyType := r.URL.Query().Get("type")
	enabled := r.URL.Query().Get("enabled")

	var (
		policies []*tiering.AdvancedPolicy
		err      error
	)

	if policyType != "" {
		policies, err = h.policyStore.ListByType(ctx, tiering.PolicyType(policyType))
	} else {
		policies, err = h.policyStore.List(ctx)
	}

	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter by enabled status if specified
	if enabled != "" {
		enabledBool := enabled == "true"
		filtered := make([]*tiering.AdvancedPolicy, 0)

		for _, p := range policies {
			if p.Enabled == enabledBool {
				filtered = append(filtered, p)
			}
		}

		policies = filtered
	}

	if policies == nil {
		policies = []*tiering.AdvancedPolicy{}
	}

	response := TieringPolicyListResponse{
		Policies:   policies,
		TotalCount: len(policies),
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *TestTieringHandler) CreateTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateTieringPolicyRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Generate ID if not provided
	policyID := req.ID
	if policyID == "" {
		policyID = fmt.Sprintf("policy-%d", time.Now().UnixNano())
	}

	// Convert simple triggers/actions to internal format
	triggers := convertSimpleTriggers(req.Triggers)
	actions := convertSimpleActions(req.Actions)

	// Build selector from bucket/prefix patterns
	selector := tiering.PolicySelector{}
	if req.BucketPattern != "" {
		selector.Buckets = []string{req.BucketPattern}
	}

	if req.PrefixPattern != "" {
		selector.Prefixes = []string{req.PrefixPattern}
	}

	// Build schedule config
	var schedule tiering.ScheduleConfig

	schedule.Enabled = req.CronExpression != ""

	// Build anti-thrash config from advanced options
	var antiThrash tiering.AntiThrashConfig
	if req.AdvancedOptions != nil && req.AdvancedOptions.AntiThrashHours > 0 {
		antiThrash.Enabled = true
		antiThrash.MinTimeInTier = (time.Duration(req.AdvancedOptions.AntiThrashHours) * time.Hour).String()
	}

	// Build rate limit config
	var rateLimit tiering.RateLimitConfig
	if req.AdvancedOptions != nil && req.AdvancedOptions.RateLimit > 0 {
		rateLimit.Enabled = true
		rateLimit.MaxObjectsPerSecond = req.AdvancedOptions.RateLimit
	}

	// Build distributed config
	var distributed tiering.DistributedConfig
	if req.AdvancedOptions != nil && req.AdvancedOptions.DistributedExecution {
		distributed.Enabled = true
	}

	policy := &tiering.AdvancedPolicy{
		ID:          policyID,
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Scope:       req.Scope,
		Enabled:     req.Enabled,
		Selector:    selector,
		Triggers:    triggers,
		Actions:     actions,
		AntiThrash:  antiThrash,
		Schedule:    schedule,
		RateLimit:   rateLimit,
		Distributed: distributed,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Version:     1,
	}

	if policy.Type == "" {
		policy.Type = tiering.PolicyTypeScheduled
	}

	if policy.Scope == "" {
		policy.Scope = tiering.PolicyScopeGlobal
	}

	err = h.policyStore.Create(ctx, policy)
	if err != nil {
		if containsString(err.Error(), "already exists") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}

		if containsString(err.Error(), "validation") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusCreated, policy)
}

func (h *TestTieringHandler) GetTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.policyStore.Get(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

func (h *TestTieringHandler) UpdateTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	existing, err := h.policyStore.Get(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	var req CreateTieringPolicyRequest
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Convert simple triggers/actions to internal format
	triggers := convertSimpleTriggers(req.Triggers)
	actions := convertSimpleActions(req.Actions)

	// Build selector from bucket/prefix patterns
	selector := tiering.PolicySelector{}
	if req.BucketPattern != "" {
		selector.Buckets = []string{req.BucketPattern}
	}

	if req.PrefixPattern != "" {
		selector.Prefixes = []string{req.PrefixPattern}
	}

	// Build schedule config
	var schedule tiering.ScheduleConfig

	schedule.Enabled = req.CronExpression != ""

	// Build anti-thrash config from advanced options
	var antiThrash tiering.AntiThrashConfig
	if req.AdvancedOptions != nil && req.AdvancedOptions.AntiThrashHours > 0 {
		antiThrash.Enabled = true
		antiThrash.MinTimeInTier = (time.Duration(req.AdvancedOptions.AntiThrashHours) * time.Hour).String()
	}

	// Build rate limit config
	var rateLimit tiering.RateLimitConfig
	if req.AdvancedOptions != nil && req.AdvancedOptions.RateLimit > 0 {
		rateLimit.Enabled = true
		rateLimit.MaxObjectsPerSecond = req.AdvancedOptions.RateLimit
	}

	// Build distributed config
	var distributed tiering.DistributedConfig
	if req.AdvancedOptions != nil && req.AdvancedOptions.DistributedExecution {
		distributed.Enabled = true
	}

	existing.Name = req.Name
	existing.Description = req.Description
	existing.Type = req.Type
	existing.Scope = req.Scope
	existing.Enabled = req.Enabled
	existing.Selector = selector
	existing.Triggers = triggers
	existing.Actions = actions
	existing.AntiThrash = antiThrash
	existing.Schedule = schedule
	existing.RateLimit = rateLimit
	existing.Distributed = distributed
	existing.UpdatedAt = time.Now()

	err = h.policyStore.Update(ctx, existing)
	if err != nil {
		if containsString(err.Error(), "version conflict") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusOK, existing)
}

func (h *TestTieringHandler) DeleteTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	err := h.policyStore.Delete(ctx, id)
	if err != nil {
		if containsString(err.Error(), "not found") {
			writeError(w, err.Error(), http.StatusNotFound)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *TestTieringHandler) EnableTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.policyStore.Get(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	policy.Enabled = true
	policy.UpdatedAt = time.Now()

	err = h.policyStore.Update(ctx, policy)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"enabled": true,
		"message": "Policy enabled successfully",
	})
}

func (h *TestTieringHandler) DisableTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.policyStore.Get(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	policy.Enabled = false
	policy.UpdatedAt = time.Now()

	err = h.policyStore.Update(ctx, policy)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"enabled": false,
		"message": "Policy disabled successfully",
	})
}

func (h *TestTieringHandler) GetTieringPolicyStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.policyStore.Get(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	policyStats, err := h.policyStore.GetStats(ctx, id)
	if err != nil {
		stats := map[string]interface{}{
			"policy_id":   policy.ID,
			"policy_name": policy.Name,
			"enabled":     policy.Enabled,
			"type":        policy.Type,
		}
		writeJSON(w, http.StatusOK, stats)

		return
	}

	stats := map[string]interface{}{
		"policy_id":            policy.ID,
		"policy_name":          policy.Name,
		"enabled":              policy.Enabled,
		"type":                 policy.Type,
		"total_executions":     policyStats.TotalExecutions,
		"objects_transitioned": policyStats.ObjectsTransitioned,
		"bytes_transitioned":   policyStats.BytesTransitioned,
		"errors":               policyStats.Errors,
	}

	writeJSON(w, http.StatusOK, stats)
}

// Access stats handlers.
func (h *TestTieringHandler) GetBucketAccessStats(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	limit, offset := ParsePaginationParams(r)

	response := map[string]interface{}{
		"bucket": bucket,
		"limit":  limit,
		"offset": offset,
		"stats":  []interface{}{},
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *TestTieringHandler) GetObjectAccessStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")
	key := GetObjectKeyFromRequest(r)

	stats, err := h.accessTracker.GetStats(ctx, bucket, key)
	if err != nil {
		stats = nil
	}

	WriteObjectAccessStatsResponse(w, bucket, key, stats)
}

// Manual transition handler.
func (h *TestTieringHandler) ManualTransition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ManualTransitionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if !ValidateManualTransitionRequest(w, &req) {
		return
	}

	err = h.tierManager.TransitionObject(ctx, req.Bucket, req.Key, req.TargetTier)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	WriteManualTransitionResponse(w, req.Bucket, req.Key, req.TargetTier)
}

// S3 Lifecycle handlers.
func (h *TestTieringHandler) GetS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	config := h.tierManager.GetS3LifecycleConfiguration(bucket)
	writeJSON(w, http.StatusOK, config)
}

func (h *TestTieringHandler) PutS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")

	var config tiering.S3LifecycleConfiguration

	err := json.NewDecoder(r.Body).Decode(&config)
	if err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.tierManager.SetS3LifecycleConfiguration(bucket, &config)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":  bucket,
		"message": "Lifecycle configuration applied successfully",
		"rules":   len(config.Rules),
	})
}

func (h *TestTieringHandler) DeleteS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	h.tierManager.SetS3LifecycleConfiguration(bucket, &tiering.S3LifecycleConfiguration{})
	w.WriteHeader(http.StatusNoContent)
}

// Status and metrics handlers.
func (h *TestTieringHandler) GetTieringStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	policies, err := h.policyStore.List(ctx)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	enabledCount := 0

	for _, p := range policies {
		if p.Enabled {
			enabledCount++
		}
	}

	status := map[string]interface{}{
		"status":           "running",
		"total_policies":   len(policies),
		"enabled_policies": enabledCount,
	}

	writeJSON(w, http.StatusOK, status)
}

func (h *TestTieringHandler) GetTieringMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	policies, err := h.policyStore.List(ctx)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var totalExecutions, totalObjectsTransitioned, totalBytesTransitioned, totalErrors int64

	for _, p := range policies {
		stats, err := h.policyStore.GetStats(ctx, p.ID)
		if err != nil {
			continue
		}

		totalExecutions += stats.TotalExecutions
		totalObjectsTransitioned += stats.ObjectsTransitioned
		totalBytesTransitioned += stats.BytesTransitioned
		totalErrors += stats.Errors
	}

	metrics := map[string]interface{}{
		"total_executions":           totalExecutions,
		"total_objects_transitioned": totalObjectsTransitioned,
		"total_bytes_transitioned":   totalBytesTransitioned,
		"total_errors":               totalErrors,
		"policy_count":               len(policies),
	}

	writeJSON(w, http.StatusOK, metrics)
}

// Predictive handlers.
func (h *TestTieringHandler) GetAccessPrediction(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*") // Catch-all for paths with slashes

	if r.URL.Query().Get("key") != "" {
		key = r.URL.Query().Get("key")
	}

	prediction := h.predictiveEngine.GetPrediction(bucket, key)
	if prediction == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"bucket":  bucket,
			"key":     key,
			"message": "Insufficient data for prediction",
		})

		return
	}

	writeJSON(w, http.StatusOK, prediction)
}

func (h *TestTieringHandler) GetTierRecommendations(w http.ResponseWriter, r *http.Request) {
	limit := 100

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := json.Number(limitStr).Int64()
		if err == nil && l > 0 && l <= 1000 {
			limit = int(l)
		}
	}

	recommendations := h.predictiveEngine.GetRecommendations(limit)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"recommendations": recommendations,
		"count":           len(recommendations),
	})
}

func (h *TestTieringHandler) GetHotObjectsPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := json.Number(limitStr).Int64()
		if err == nil && l > 0 && l <= 500 {
			limit = int(l)
		}
	}

	hotObjects := h.accessTracker.GetHotObjects(ctx, limit)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hot_objects": hotObjects,
		"count":       len(hotObjects),
	})
}

func (h *TestTieringHandler) GetColdObjectsPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := ParseLimitParam(r, 50, 500)
	inactiveDays := ParseInactiveDaysParam(r, 30)

	coldObjects := h.accessTracker.GetColdObjects(ctx, inactiveDays, limit)
	WriteColdObjectsResponse(w, coldObjects, len(coldObjects), inactiveDays)
}

func (h *TestTieringHandler) GetAccessAnomalies(w http.ResponseWriter, r *http.Request) {
	limit := 50

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := json.Number(limitStr).Int64()
		if err == nil && l > 0 && l <= 500 {
			limit = int(l)
		}
	}

	anomalies := h.anomalyDetector.GetAnomalies(limit)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"anomalies": anomalies,
		"count":     len(anomalies),
	})
}

// ====================
// Test Helpers
// ====================

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

func createTestRouter(handler *TestTieringHandler) *chi.Mux {
	r := chi.NewRouter()
	handler.RegisterTieringRoutes(r)

	return r
}

func createValidPolicy(id, name string) *tiering.AdvancedPolicy {
	return &tiering.AdvancedPolicy{
		ID:      id,
		Name:    name,
		Type:    tiering.PolicyTypeScheduled,
		Scope:   tiering.PolicyScopeGlobal,
		Enabled: true,
		Triggers: []tiering.PolicyTrigger{
			{
				Type: tiering.TriggerTypeAge,
				Age:  &tiering.AgeTrigger{DaysSinceCreation: 30},
			},
		},
		Actions: []tiering.PolicyAction{
			{
				Type:       tiering.ActionTransition,
				Transition: &tiering.TransitionActionConfig{TargetTier: tiering.TierCold},
			},
		},
	}
}

// ====================
// Policy CRUD Tests
// ====================

func TestListTieringPolicies(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	t.Run("empty list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tiering/policies", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response TieringPolicyListResponse

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, 0, response.TotalCount)
		assert.Empty(t, response.Policies)
	})

	t.Run("list with policies", func(t *testing.T) {
		// Add policies
		policy1 := createValidPolicy("policy-1", "Test Policy 1")
		policy2 := createValidPolicy("policy-2", "Test Policy 2")

		require.NoError(t, handler.policyStore.Create(context.Background(), policy1))
		require.NoError(t, handler.policyStore.Create(context.Background(), policy2))

		req := httptest.NewRequest(http.MethodGet, "/tiering/policies", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response TieringPolicyListResponse

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, 2, response.TotalCount)
		assert.Len(t, response.Policies, 2)
	})

	t.Run("filter by type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tiering/policies?type=scheduled", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response TieringPolicyListResponse

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)

		for _, p := range response.Policies {
			assert.Equal(t, tiering.PolicyTypeScheduled, p.Type)
		}
	})

	t.Run("filter by enabled", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tiering/policies?enabled=true", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response TieringPolicyListResponse

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)

		for _, p := range response.Policies {
			assert.True(t, p.Enabled)
		}
	})
}

func TestCreateTieringPolicy(t *testing.T) {
	t.Run("valid policy creation", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := CreateTieringPolicyRequest{
			ID:      "test-policy-1",
			Name:    "Test Policy",
			Type:    tiering.PolicyTypeScheduled,
			Scope:   tiering.PolicyScopeGlobal,
			Enabled: true,
			Triggers: []SimpleTieringTrigger{
				{
					Type:    "age",
					AgeDays: 30,
				},
			},
			Actions: []SimpleTieringAction{
				{
					Type:       "transition",
					TargetTier: "cold",
				},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/policies", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusCreated, rr.Code)

		var policy tiering.AdvancedPolicy

		err := json.NewDecoder(rr.Body).Decode(&policy)
		require.NoError(t, err)
		assert.Equal(t, "test-policy-1", policy.ID)
		assert.Equal(t, "Test Policy", policy.Name)
		assert.Equal(t, 1, policy.Version)
	})

	t.Run("invalid request body", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/tiering/policies", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("duplicate policy", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		// Create first policy
		policy := createValidPolicy("test-policy-1", "Test Policy")
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		// Try to create duplicate
		reqBody := CreateTieringPolicyRequest{
			ID:      "test-policy-1",
			Name:    "Test Policy Duplicate",
			Type:    tiering.PolicyTypeScheduled,
			Scope:   tiering.PolicyScopeGlobal,
			Enabled: true,
			Triggers: []SimpleTieringTrigger{
				{Type: "age", AgeDays: 30},
			},
			Actions: []SimpleTieringAction{
				{Type: "transition", TargetTier: "cold"},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/policies", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusConflict, rr.Code)
	})

	t.Run("missing triggers validation", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := CreateTieringPolicyRequest{
			ID:      "test-policy-invalid",
			Name:    "Invalid Policy",
			Type:    tiering.PolicyTypeScheduled,
			Scope:   tiering.PolicyScopeGlobal,
			Enabled: true,
			// Missing triggers
			Actions: []SimpleTieringAction{
				{Type: "transition", TargetTier: "cold"},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/policies", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestGetTieringPolicy(t *testing.T) {
	t.Run("policy found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		policy := createValidPolicy("test-policy-1", "Test Policy")
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		req := httptest.NewRequest(http.MethodGet, "/tiering/policies/test-policy-1", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var retrieved tiering.AdvancedPolicy

		err := json.NewDecoder(rr.Body).Decode(&retrieved)
		require.NoError(t, err)
		assert.Equal(t, "test-policy-1", retrieved.ID)
		assert.Equal(t, "Test Policy", retrieved.Name)
	})

	t.Run("policy not found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/policies/nonexistent", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestUpdateTieringPolicy(t *testing.T) {
	t.Run("successful update", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		policy := createValidPolicy("test-policy-1", "Test Policy")
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		reqBody := CreateTieringPolicyRequest{
			ID:      "test-policy-1",
			Name:    "Updated Policy Name",
			Type:    tiering.PolicyTypeScheduled,
			Scope:   tiering.PolicyScopeGlobal,
			Enabled: false,
			Triggers: []SimpleTieringTrigger{
				{Type: "age", AgeDays: 60},
			},
			Actions: []SimpleTieringAction{
				{Type: "transition", TargetTier: "archive"},
			},
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPut, "/tiering/policies/test-policy-1", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var updated tiering.AdvancedPolicy

		err := json.NewDecoder(rr.Body).Decode(&updated)
		require.NoError(t, err)
		assert.Equal(t, "Updated Policy Name", updated.Name)
		assert.False(t, updated.Enabled)
		assert.Equal(t, 2, updated.Version)
	})

	t.Run("policy not found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := CreateTieringPolicyRequest{
			ID:   "nonexistent",
			Name: "Updated",
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPut, "/tiering/policies/nonexistent", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestDeleteTieringPolicy(t *testing.T) {
	t.Run("successful delete", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		policy := createValidPolicy("test-policy-1", "Test Policy")
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		req := httptest.NewRequest(http.MethodDelete, "/tiering/policies/test-policy-1", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNoContent, rr.Code)

		// Verify deleted
		_, err := handler.policyStore.Get(context.Background(), "test-policy-1")
		assert.Error(t, err)
	})

	t.Run("policy not found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodDelete, "/tiering/policies/nonexistent", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestGetPolicyStats(t *testing.T) {
	t.Run("stats found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		policy := createValidPolicy("test-policy-1", "Test Policy")
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		// Set custom stats
		handler.policyStore.SetStats("test-policy-1", &tiering.PolicyStats{
			PolicyID:            "test-policy-1",
			TotalExecutions:     100,
			ObjectsTransitioned: 500,
			BytesTransitioned:   1024000,
		})

		req := httptest.NewRequest(http.MethodGet, "/tiering/policies/test-policy-1/stats", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var stats map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&stats)
		require.NoError(t, err)
		assert.Equal(t, "test-policy-1", stats["policy_id"])
		assert.InDelta(t, float64(100), stats["total_executions"], 0.001)
	})

	t.Run("policy not found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/policies/nonexistent/stats", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestEnableDisableTieringPolicy(t *testing.T) {
	t.Run("enable policy", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		policy := createValidPolicy("test-policy-1", "Test Policy")
		policy.Enabled = false
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		req := httptest.NewRequest(http.MethodPost, "/tiering/policies/test-policy-1/enable", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, true, response["enabled"])
	})

	t.Run("disable policy", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		policy := createValidPolicy("test-policy-1", "Test Policy")
		policy.Enabled = true
		require.NoError(t, handler.policyStore.Create(context.Background(), policy))

		req := httptest.NewRequest(http.MethodPost, "/tiering/policies/test-policy-1/disable", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, false, response["enabled"])
	})

	t.Run("enable nonexistent policy", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/tiering/policies/nonexistent/enable", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

// ====================
// Access Statistics Tests
// ====================

func TestGetObjectAccessStats(t *testing.T) {
	t.Run("stats found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		handler.accessTracker.SetStats("my-bucket", "my-key.txt", &tiering.ObjectAccessStats{
			Bucket:          "my-bucket",
			Key:             "my-key.txt",
			AccessCount:     150,
			AccessesLast24h: 10,
			AccessesLast7d:  50,
			AccessesLast30d: 150,
			AccessTrend:     "stable",
		})

		req := httptest.NewRequest(http.MethodGet, "/tiering/access-stats/my-bucket/my-key.txt", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, true, response["tracked"])
		assert.Equal(t, "my-bucket", response["bucket"])
		assert.Equal(t, "my-key.txt", response["key"])
		assert.InDelta(t, float64(150), response["access_count"], 0.001)
	})

	t.Run("stats not found", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/access-stats/my-bucket/unknown-key.txt", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, false, response["tracked"])
	})
}

func TestGetBucketAccessStats(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/tiering/access-stats/my-bucket", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}

	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "my-bucket", response["bucket"])
}

func TestGetHotObjects(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	handler.accessTracker.SetHotObjects([]*tiering.ObjectAccessStats{
		{Bucket: "bucket1", Key: "hot-file1.txt", AccessCount: 1000},
		{Bucket: "bucket1", Key: "hot-file2.txt", AccessCount: 800},
	})

	req := httptest.NewRequest(http.MethodGet, "/tiering/predictions/hot-objects", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}

	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.InDelta(t, float64(2), response["count"], 0.001)
}

func TestGetColdObjects(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	handler.accessTracker.SetColdObjects([]*tiering.ObjectAccessStats{
		{Bucket: "bucket1", Key: "cold-file1.txt", AccessCount: 1},
		{Bucket: "bucket1", Key: "cold-file2.txt", AccessCount: 0},
	})

	req := httptest.NewRequest(http.MethodGet, "/tiering/predictions/cold-objects?inactive_days=30", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}

	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.InDelta(t, float64(2), response["count"], 0.001)
	assert.InDelta(t, float64(30), response["inactive_days"], 0.001)
}

// ====================
// Tier Management Tests
// ====================

func TestManualTransition(t *testing.T) {
	t.Run("successful transition", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := ManualTransitionRequest{
			Bucket:     "my-bucket",
			Key:        "my-file.txt",
			TargetTier: tiering.TierCold,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/transition", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "my-bucket", response["bucket"])
		assert.Equal(t, "my-file.txt", response["key"])
		assert.Equal(t, string(tiering.TierCold), response["target_tier"])
	})

	t.Run("missing bucket", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := ManualTransitionRequest{
			Key:        "my-file.txt",
			TargetTier: tiering.TierCold,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/transition", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("missing key", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := ManualTransitionRequest{
			Bucket:     "my-bucket",
			TargetTier: tiering.TierCold,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/transition", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("invalid target tier", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		reqBody := ManualTransitionRequest{
			Bucket:     "my-bucket",
			Key:        "my-file.txt",
			TargetTier: "invalid-tier",
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/transition", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("transition error", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		handler.tierManager.SetTransitionError(errors.New("storage error"))

		reqBody := ManualTransitionRequest{
			Bucket:     "my-bucket",
			Key:        "my-file.txt",
			TargetTier: tiering.TierCold,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/tiering/transition", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})

	t.Run("all valid tier types", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		tiers := []tiering.TierType{
			tiering.TierHot,
			tiering.TierWarm,
			tiering.TierCold,
			tiering.TierArchive,
		}

		for _, tier := range tiers {
			reqBody := ManualTransitionRequest{
				Bucket:     "my-bucket",
				Key:        "my-file.txt",
				TargetTier: tier,
			}

			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPost, "/tiering/transition", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "Expected 200 for tier %s", tier)
		}
	})
}

// ====================
// S3 Lifecycle Tests
// ====================

func TestGetS3LifecycleConfiguration(t *testing.T) {
	t.Run("get empty configuration", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/s3-lifecycle/my-bucket", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response tiering.S3LifecycleConfiguration

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Empty(t, response.Rules)
	})

	t.Run("get existing configuration", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		// Set configuration
		config := &tiering.S3LifecycleConfiguration{
			Rules: []tiering.S3LifecycleRule{
				{
					ID:     "rule-1",
					Status: "Enabled",
					Filter: &tiering.S3LifecycleFilter{Prefix: "logs/"},
					Transitions: []tiering.S3LifecycleTransition{
						{Days: 30, StorageClass: "STANDARD_IA"},
					},
				},
			},
		}
		handler.tierManager.SetS3LifecycleConfiguration("my-bucket", config)

		req := httptest.NewRequest(http.MethodGet, "/tiering/s3-lifecycle/my-bucket", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response tiering.S3LifecycleConfiguration

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Len(t, response.Rules, 1)
		assert.Equal(t, "rule-1", response.Rules[0].ID)
	})
}

func TestSetS3LifecycleConfiguration(t *testing.T) {
	t.Run("set configuration with JSON", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		config := &tiering.S3LifecycleConfiguration{
			Rules: []tiering.S3LifecycleRule{
				{
					ID:     "archive-logs",
					Status: "Enabled",
					Filter: &tiering.S3LifecycleFilter{Prefix: "logs/"},
					Transitions: []tiering.S3LifecycleTransition{
						{Days: 30, StorageClass: "STANDARD_IA"},
					},
				},
			},
		}

		body, _ := json.Marshal(config)
		req := httptest.NewRequest(http.MethodPut, "/tiering/s3-lifecycle/my-bucket", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "my-bucket", response["bucket"])
		assert.InDelta(t, float64(1), response["rules"], 0.001)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodPut, "/tiering/s3-lifecycle/my-bucket", bytes.NewReader([]byte("invalid")))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestDeleteS3LifecycleConfiguration(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	// Set initial configuration
	config := &tiering.S3LifecycleConfiguration{
		Rules: []tiering.S3LifecycleRule{
			{ID: "rule-1", Status: "Enabled"},
		},
	}
	handler.tierManager.SetS3LifecycleConfiguration("my-bucket", config)

	req := httptest.NewRequest(http.MethodDelete, "/tiering/s3-lifecycle/my-bucket", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)

	// Verify configuration is empty
	retrieved := handler.tierManager.GetS3LifecycleConfiguration("my-bucket")
	assert.Empty(t, retrieved.Rules)
}

// ====================
// Predictive Analytics Tests
// ====================

func TestGetAccessPrediction(t *testing.T) {
	t.Run("prediction available", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		handler.predictiveEngine.SetPrediction("my-bucket", "my-file.txt", &tiering.AccessPrediction{
			Bucket:               "my-bucket",
			Key:                  "my-file.txt",
			ShortTermAccessRate:  5.0,
			ShortTermConfidence:  0.85,
			MediumTermAccessRate: 4.0,
			LongTermAccessRate:   3.0,
			TrendDirection:       "stable",
			RecommendedTier:      tiering.TierWarm,
			Confidence:           0.8,
		})

		req := httptest.NewRequest(http.MethodGet, "/tiering/predictions/my-bucket/my-file.txt", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response tiering.AccessPrediction

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "my-bucket", response.Bucket)
		assert.InDelta(t, 5.0, response.ShortTermAccessRate, 0.001)
		assert.Equal(t, tiering.TierWarm, response.RecommendedTier)
	})

	t.Run("no prediction data", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/predictions/my-bucket/unknown-file.txt", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.Contains(t, response["message"], "Insufficient data")
	})
}

func TestGetTierRecommendations(t *testing.T) {
	t.Run("with recommendations", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		handler.predictiveEngine.SetRecommendations([]*tiering.TierRecommendation{
			{
				Bucket:          "bucket1",
				Key:             "file1.txt",
				CurrentTier:     tiering.TierHot,
				RecommendedTier: tiering.TierCold,
				Confidence:      0.9,
				Reasoning:       "Low access rate detected",
			},
			{
				Bucket:          "bucket1",
				Key:             "file2.txt",
				CurrentTier:     tiering.TierCold,
				RecommendedTier: tiering.TierHot,
				Confidence:      0.85,
				Reasoning:       "High access rate detected",
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/tiering/predictions/recommendations", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.InDelta(t, float64(2), response["count"], 0.001)
	})

	t.Run("with limit parameter", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		recs := make([]*tiering.TierRecommendation, 10)
		for i := range 10 {
			recs[i] = &tiering.TierRecommendation{
				Bucket:          "bucket1",
				Key:             fmt.Sprintf("file%d.txt", i),
				CurrentTier:     tiering.TierHot,
				RecommendedTier: tiering.TierCold,
			}
		}

		handler.predictiveEngine.SetRecommendations(recs)

		req := httptest.NewRequest(http.MethodGet, "/tiering/predictions/recommendations?limit=5", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.InDelta(t, float64(5), response["count"], 0.001)
	})
}

func TestGetAccessAnomalies(t *testing.T) {
	t.Run("with anomalies", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		handler.anomalyDetector.SetAnomalies([]*tiering.AccessAnomaly{
			{
				Bucket:      "bucket1",
				Key:         "suspicious-file.txt",
				DetectedAt:  time.Now(),
				AnomalyType: "spike",
				Severity:    4.5,
				Expected:    10,
				Actual:      100,
				Description: "Access rate significantly higher than baseline",
			},
		})

		req := httptest.NewRequest(http.MethodGet, "/tiering/anomalies", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.InDelta(t, float64(1), response["count"], 0.001)
	})

	t.Run("no anomalies", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/anomalies", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.InDelta(t, float64(0), response["count"], 0.001)
	})

	t.Run("with limit parameter", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		anomalies := make([]*tiering.AccessAnomaly, 20)
		for i := range 20 {
			anomalies[i] = &tiering.AccessAnomaly{
				Bucket:      "bucket1",
				Key:         fmt.Sprintf("file%d.txt", i),
				AnomalyType: "spike",
			}
		}

		handler.anomalyDetector.SetAnomalies(anomalies)

		req := httptest.NewRequest(http.MethodGet, "/tiering/anomalies?limit=10", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.InDelta(t, float64(10), response["count"], 0.001)
	})
}

// ====================
// Status and Metrics Tests
// ====================

func TestGetTieringStatus(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	// Add some policies
	policy1 := createValidPolicy("policy-1", "Policy 1")
	policy1.Enabled = true
	policy2 := createValidPolicy("policy-2", "Policy 2")
	policy2.Enabled = false

	require.NoError(t, handler.policyStore.Create(context.Background(), policy1))
	require.NoError(t, handler.policyStore.Create(context.Background(), policy2))

	req := httptest.NewRequest(http.MethodGet, "/tiering/status", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}

	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "running", response["status"])
	assert.InDelta(t, float64(2), response["total_policies"], 0.001)
	assert.InDelta(t, float64(1), response["enabled_policies"], 0.001)
}

func TestGetTieringMetrics(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	// Add policies with stats
	policy1 := createValidPolicy("policy-1", "Policy 1")
	require.NoError(t, handler.policyStore.Create(context.Background(), policy1))
	handler.policyStore.SetStats("policy-1", &tiering.PolicyStats{
		PolicyID:            "policy-1",
		TotalExecutions:     50,
		ObjectsTransitioned: 200,
		BytesTransitioned:   500000,
		Errors:              5,
	})

	req := httptest.NewRequest(http.MethodGet, "/tiering/metrics", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response map[string]interface{}

	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.InDelta(t, float64(50), response["total_executions"], 0.001)
	assert.InDelta(t, float64(200), response["total_objects_transitioned"], 0.001)
	assert.InDelta(t, float64(500000), response["total_bytes_transitioned"], 0.001)
	assert.InDelta(t, float64(5), response["total_errors"], 0.001)
}

// ====================
// Edge Cases and Error Handling Tests
// ====================

func TestPaginationParameters(t *testing.T) {
	t.Run("bucket access stats with pagination", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodGet, "/tiering/access-stats/my-bucket?limit=50&offset=10", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		assert.InDelta(t, float64(50), response["limit"], 0.001)
		assert.InDelta(t, float64(10), response["offset"], 0.001)
	})

	t.Run("limit clamping", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		// Request with limit exceeding maximum
		req := httptest.NewRequest(http.MethodGet, "/tiering/access-stats/my-bucket?limit=9999", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response map[string]interface{}

		err := json.NewDecoder(rr.Body).Decode(&response)
		require.NoError(t, err)
		// Should be clamped to maximum allowed (1000)
		limit := int(response["limit"].(float64))
		assert.LessOrEqual(t, limit, 1000)
	})
}

func TestRequestBodyValidation(t *testing.T) {
	t.Run("empty request body for create", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/tiering/policies", nil)
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("malformed JSON", func(t *testing.T) {
		handler := NewTestTieringHandler()
		router := createTestRouter(handler)

		req := httptest.NewRequest(http.MethodPost, "/tiering/policies", bytes.NewReader([]byte("{invalid json")))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestURLEncodedKeys(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	// Test with URL-encoded key parameter
	handler.accessTracker.SetStats("my-bucket", "path/to/file.txt", &tiering.ObjectAccessStats{
		Bucket:      "my-bucket",
		Key:         "path/to/file.txt",
		AccessCount: 100,
	})

	req := httptest.NewRequest(http.MethodGet, "/tiering/access-stats/my-bucket/path/to/file.txt", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestConcurrentRequests(t *testing.T) {
	handler := NewTestTieringHandler()
	router := createTestRouter(handler)

	// Create initial policy
	policy := createValidPolicy("concurrent-test", "Concurrent Test")
	require.NoError(t, handler.policyStore.Create(context.Background(), policy))

	// Run concurrent requests
	var wg sync.WaitGroup

	errors := make(chan error, 10)

	for i := range 10 {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodGet, "/tiering/policies/concurrent-test", nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				errors <- fmt.Errorf("request %d failed with status %d", id, rr.Code)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}
