package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/piwi3910/nebulaio/internal/tiering"
)

// TieringHandler handles tiering policy API requests
type TieringHandler struct {
	service *tiering.AdvancedService
}

// NewTieringHandler creates a new tiering policy handler
func NewTieringHandler(service *tiering.AdvancedService) *TieringHandler {
	return &TieringHandler{
		service: service,
	}
}

// RegisterTieringRoutes registers tiering policy routes
func (h *TieringHandler) RegisterTieringRoutes(r chi.Router) {
	// Tiering Policies
	r.Get("/tiering/policies", h.ListTieringPolicies)
	r.Post("/tiering/policies", h.CreateTieringPolicy)
	r.Get("/tiering/policies/{id}", h.GetTieringPolicy)
	r.Put("/tiering/policies/{id}", h.UpdateTieringPolicy)
	r.Delete("/tiering/policies/{id}", h.DeleteTieringPolicy)
	r.Post("/tiering/policies/{id}/enable", h.EnableTieringPolicy)
	r.Post("/tiering/policies/{id}/disable", h.DisableTieringPolicy)
	r.Get("/tiering/policies/{id}/stats", h.GetTieringPolicyStats)

	// Access Stats
	r.Get("/tiering/access-stats/{bucket}", h.GetBucketAccessStats)
	r.Get("/tiering/access-stats/{bucket}/{key}", h.GetObjectAccessStats)

	// Manual Transitions
	r.Post("/tiering/transition", h.ManualTransition)

	// S3 Lifecycle Compatibility
	r.Get("/tiering/s3-lifecycle/{bucket}", h.GetS3Lifecycle)
	r.Put("/tiering/s3-lifecycle/{bucket}", h.PutS3Lifecycle)
	r.Delete("/tiering/s3-lifecycle/{bucket}", h.DeleteS3Lifecycle)

	// Tiering Status
	r.Get("/tiering/status", h.GetTieringStatus)
	r.Get("/tiering/metrics", h.GetTieringMetrics)

	// Predictive Tiering (ML-based)
	r.Get("/tiering/predictions/{bucket}/{key}", h.GetAccessPrediction)
	r.Get("/tiering/predictions/recommendations", h.GetTierRecommendations)
	r.Get("/tiering/predictions/hot-objects", h.GetHotObjectsPrediction)
	r.Get("/tiering/predictions/cold-objects", h.GetColdObjectsPrediction)
	r.Get("/tiering/anomalies", h.GetAccessAnomalies)
}

// ====================
// Policy CRUD Handlers
// ====================

// TieringPolicyListResponse represents the list response
type TieringPolicyListResponse struct {
	Policies   []*tiering.AdvancedPolicy `json:"policies"`
	TotalCount int                       `json:"total_count"`
}

// ListTieringPolicies lists all tiering policies
func (h *TieringHandler) ListTieringPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for filtering
	policyType := r.URL.Query().Get("type")
	scope := r.URL.Query().Get("scope")
	enabled := r.URL.Query().Get("enabled")

	var policies []*tiering.AdvancedPolicy
	var err error

	if policyType != "" {
		policies, err = h.service.ListPoliciesByType(ctx, tiering.PolicyType(policyType))
	} else if scope != "" {
		policies, err = h.service.ListPoliciesByScope(ctx, tiering.PolicyScope(scope))
	} else {
		policies, err = h.service.ListPolicies(ctx)
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

	response := TieringPolicyListResponse{
		Policies:   policies,
		TotalCount: len(policies),
	}

	writeJSON(w, http.StatusOK, response)
}

// CreateTieringPolicyRequest represents the create request
type CreateTieringPolicyRequest struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Type        tiering.PolicyType        `json:"type"`
	Scope       tiering.PolicyScope       `json:"scope"`
	Enabled     bool                      `json:"enabled"`
	Priority    int                       `json:"priority"`
	Selector    tiering.PolicySelector    `json:"selector"`
	Triggers    []tiering.PolicyTrigger   `json:"triggers"`
	Actions     []tiering.PolicyAction    `json:"actions"`
	AntiThrash  tiering.AntiThrashConfig  `json:"anti_thrash"`
	Schedule    tiering.ScheduleConfig    `json:"schedule,omitempty"`
	RateLimit   tiering.RateLimitConfig   `json:"rate_limit,omitempty"`
	Distributed tiering.DistributedConfig `json:"distributed,omitempty"`
}

// CreateTieringPolicy creates a new tiering policy
func (h *TieringHandler) CreateTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateTieringPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	policy := &tiering.AdvancedPolicy{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Scope:       req.Scope,
		Enabled:     req.Enabled,
		Priority:    req.Priority,
		Selector:    req.Selector,
		Triggers:    req.Triggers,
		Actions:     req.Actions,
		AntiThrash:  req.AntiThrash,
		Schedule:    req.Schedule,
		RateLimit:   req.RateLimit,
		Distributed: req.Distributed,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Version:     1,
	}
	// Stats will be initialized by the service

	// Set defaults
	if policy.Type == "" {
		policy.Type = tiering.PolicyTypeScheduled
	}
	if policy.Scope == "" {
		policy.Scope = tiering.PolicyScopeGlobal
	}

	if err := h.service.CreatePolicy(ctx, policy); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}
		if strings.Contains(err.Error(), "validation") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, policy)
}

// GetTieringPolicy gets a tiering policy by ID
func (h *TieringHandler) GetTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.service.GetPolicy(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

// UpdateTieringPolicy updates a tiering policy
func (h *TieringHandler) UpdateTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	// Get existing policy
	existing, err := h.service.GetPolicy(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	var req CreateTieringPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Update fields
	existing.Name = req.Name
	existing.Description = req.Description
	existing.Type = req.Type
	existing.Scope = req.Scope
	existing.Enabled = req.Enabled
	existing.Priority = req.Priority
	existing.Selector = req.Selector
	existing.Triggers = req.Triggers
	existing.Actions = req.Actions
	existing.AntiThrash = req.AntiThrash
	existing.Schedule = req.Schedule
	existing.RateLimit = req.RateLimit
	existing.Distributed = req.Distributed
	existing.UpdatedAt = time.Now()

	if err := h.service.UpdatePolicy(ctx, existing); err != nil {
		if strings.Contains(err.Error(), "version conflict") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}
		if strings.Contains(err.Error(), "validation") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, existing)
}

// DeleteTieringPolicy deletes a tiering policy
func (h *TieringHandler) DeleteTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if err := h.service.DeletePolicy(ctx, id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, err.Error(), http.StatusNotFound)
			return
		}
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// EnableTieringPolicy enables a tiering policy
func (h *TieringHandler) EnableTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.service.GetPolicy(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	policy.Enabled = true
	policy.UpdatedAt = time.Now()

	if err := h.service.UpdatePolicy(ctx, policy); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"enabled": true,
		"message": "Policy enabled successfully",
	})
}

// DisableTieringPolicy disables a tiering policy
func (h *TieringHandler) DisableTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.service.GetPolicy(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	policy.Enabled = false
	policy.UpdatedAt = time.Now()

	if err := h.service.UpdatePolicy(ctx, policy); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      id,
		"enabled": false,
		"message": "Policy disabled successfully",
	})
}

// GetTieringPolicyStats gets statistics for a tiering policy
func (h *TieringHandler) GetTieringPolicyStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	policy, err := h.service.GetPolicy(ctx, id)
	if err != nil {
		writeError(w, "Policy not found", http.StatusNotFound)
		return
	}

	// Get stats from service
	policyStats, err := h.service.GetPolicyStats(ctx, id)
	if err != nil {
		// Return basic info without stats if stats not available
		stats := map[string]interface{}{
			"policy_id":   policy.ID,
			"policy_name": policy.Name,
			"enabled":     policy.Enabled,
			"type":        policy.Type,
			"last_run_at": policy.LastRunAt,
		}
		writeJSON(w, http.StatusOK, stats)
		return
	}

	// Return policy with its stats
	stats := map[string]interface{}{
		"policy_id":            policy.ID,
		"policy_name":          policy.Name,
		"enabled":              policy.Enabled,
		"type":                 policy.Type,
		"last_executed":        policyStats.LastExecuted,
		"total_executions":     policyStats.TotalExecutions,
		"objects_evaluated":    policyStats.ObjectsEvaluated,
		"objects_transitioned": policyStats.ObjectsTransitioned,
		"bytes_transitioned":   policyStats.BytesTransitioned,
		"errors":               policyStats.Errors,
		"last_error":           policyStats.LastError,
	}

	writeJSON(w, http.StatusOK, stats)
}

// ====================
// Access Stats Handlers
// ====================

// GetBucketAccessStats gets access stats for all objects in a bucket
func (h *TieringHandler) GetBucketAccessStats(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")

	// Parse pagination parameters
	limit := 100
	offset := 0

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := json.Number(limitStr).Int64(); err == nil && l > 0 && l <= 1000 {
			limit = int(l)
		}
	}
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := json.Number(offsetStr).Int64(); err == nil && o >= 0 {
			offset = int(o)
		}
	}

	// Get all tracked keys for bucket (this is a simplified implementation)
	// In production, you'd query the access tracker directly
	response := map[string]interface{}{
		"bucket": bucket,
		"limit":  limit,
		"offset": offset,
		"stats":  []interface{}{},
	}

	writeJSON(w, http.StatusOK, response)
}

// GetObjectAccessStats gets access stats for a specific object
func (h *TieringHandler) GetObjectAccessStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// URL decode the key if needed
	if r.URL.Query().Get("key") != "" {
		key = r.URL.Query().Get("key")
	}

	stats, err := h.service.GetAccessStats(ctx, bucket, key)
	if err != nil || stats == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"bucket":  bucket,
			"key":     key,
			"tracked": false,
			"message": "No access stats tracked for this object",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":               bucket,
		"key":                  key,
		"tracked":              true,
		"access_count":         stats.AccessCount,
		"last_accessed":        stats.LastAccessed,
		"accesses_last_24h":    stats.AccessesLast24h,
		"accesses_last_7d":     stats.AccessesLast7d,
		"accesses_last_30d":    stats.AccessesLast30d,
		"average_accesses_day": stats.AverageAccessesDay,
		"access_trend":         stats.AccessTrend,
	})
}

// ====================
// Manual Transition Handler
// ====================

// ManualTransitionRequest represents a manual transition request
type ManualTransitionRequest struct {
	Bucket     string            `json:"bucket"`
	Key        string            `json:"key"`
	TargetTier tiering.TierType  `json:"target_tier"`
	Force      bool              `json:"force"` // Skip anti-thrash checks
}

// ManualTransition manually transitions an object to a different tier
func (h *TieringHandler) ManualTransition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ManualTransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Bucket == "" {
		writeError(w, "bucket is required", http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		writeError(w, "key is required", http.StatusBadRequest)
		return
	}
	if req.TargetTier == "" {
		writeError(w, "target_tier is required", http.StatusBadRequest)
		return
	}

	// Validate tier
	validTiers := map[tiering.TierType]bool{
		tiering.TierHot:     true,
		tiering.TierWarm:    true,
		tiering.TierCold:    true,
		tiering.TierArchive: true,
	}
	if !validTiers[req.TargetTier] {
		writeError(w, "invalid target_tier: must be hot, warm, cold, or archive", http.StatusBadRequest)
		return
	}

	if err := h.service.TransitionObject(ctx, req.Bucket, req.Key, req.TargetTier); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":      req.Bucket,
		"key":         req.Key,
		"target_tier": req.TargetTier,
		"message":     "Object transition initiated successfully",
	})
}

// ====================
// S3 Lifecycle Compatibility Handlers
// ====================

// GetS3Lifecycle gets S3-compatible lifecycle configuration for a bucket
func (h *TieringHandler) GetS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")

	config, err := h.service.GetS3LifecycleConfiguration(ctx, bucket)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Check Accept header for XML vs JSON
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/xml") || strings.Contains(accept, "text/xml") {
		xmlData, err := config.ToXML()
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write(xmlData)
		return
	}

	writeJSON(w, http.StatusOK, config)
}

// PutS3Lifecycle sets S3-compatible lifecycle configuration for a bucket
func (h *TieringHandler) PutS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")

	contentType := r.Header.Get("Content-Type")

	var config *tiering.S3LifecycleConfiguration
	var err error

	if strings.Contains(contentType, "application/xml") || strings.Contains(contentType, "text/xml") {
		// Parse XML body
		var xmlData []byte
		xmlData = make([]byte, r.ContentLength)
		if _, err := r.Body.Read(xmlData); err != nil {
			writeError(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		config, err = tiering.ParseS3LifecycleXML(xmlData)
		if err != nil {
			writeError(w, "Invalid XML: "+err.Error(), http.StatusBadRequest)
			return
		}
	} else {
		// Parse JSON body
		if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
			writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := h.service.SetS3LifecycleConfiguration(ctx, bucket, config); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":  bucket,
		"message": "Lifecycle configuration applied successfully",
		"rules":   len(config.Rules),
	})
}

// DeleteS3Lifecycle deletes S3-compatible lifecycle configuration for a bucket
func (h *TieringHandler) DeleteS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")

	// Delete by setting empty configuration
	if err := h.service.SetS3LifecycleConfiguration(ctx, bucket, &tiering.S3LifecycleConfiguration{}); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ====================
// Status and Metrics Handlers
// ====================

// GetTieringStatus gets the overall tiering system status
func (h *TieringHandler) GetTieringStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	policies, err := h.service.ListPolicies(ctx)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Count policies by type and status
	byType := make(map[string]int)
	byScope := make(map[string]int)
	enabledCount := 0

	for _, p := range policies {
		byType[string(p.Type)]++
		byScope[string(p.Scope)]++
		if p.Enabled {
			enabledCount++
		}
	}

	status := map[string]interface{}{
		"status":          "running",
		"total_policies":  len(policies),
		"enabled_policies": enabledCount,
		"policies_by_type": byType,
		"policies_by_scope": byScope,
	}

	writeJSON(w, http.StatusOK, status)
}

// GetTieringMetrics gets tiering metrics
func (h *TieringHandler) GetTieringMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	policies, err := h.service.ListPolicies(ctx)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Aggregate metrics across all policies
	var totalExecutions, totalObjectsTransitioned, totalBytesTransitioned, totalErrors int64

	for _, p := range policies {
		stats, err := h.service.GetPolicyStats(ctx, p.ID)
		if err != nil {
			continue
		}
		totalExecutions += stats.TotalExecutions
		totalObjectsTransitioned += stats.ObjectsTransitioned
		totalBytesTransitioned += stats.BytesTransitioned
		totalErrors += stats.Errors
	}

	metrics := map[string]interface{}{
		"total_executions":          totalExecutions,
		"total_objects_transitioned": totalObjectsTransitioned,
		"total_bytes_transitioned":  totalBytesTransitioned,
		"total_errors":              totalErrors,
		"policy_count":              len(policies),
	}

	writeJSON(w, http.StatusOK, metrics)
}

// ====================
// Predictive Tiering Handlers
// ====================

// GetAccessPrediction gets ML-based access prediction for an object
func (h *TieringHandler) GetAccessPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "key")

	// URL decode key if needed
	if r.URL.Query().Get("key") != "" {
		key = r.URL.Query().Get("key")
	}

	prediction, err := h.service.GetPrediction(ctx, bucket, key)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

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

// GetTierRecommendations gets ML-based tier recommendations
func (h *TieringHandler) GetTierRecommendations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse limit parameter
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := json.Number(limitStr).Int64(); err == nil && l > 0 && l <= 1000 {
			limit = int(l)
		}
	}

	recommendations, err := h.service.GetTierRecommendations(ctx, limit)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"recommendations": recommendations,
		"count":           len(recommendations),
	})
}

// GetHotObjectsPrediction gets objects predicted to become hot
func (h *TieringHandler) GetHotObjectsPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := json.Number(limitStr).Int64(); err == nil && l > 0 && l <= 500 {
			limit = int(l)
		}
	}

	hotObjects := h.service.GetHotObjects(ctx, limit)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hot_objects": hotObjects,
		"count":       len(hotObjects),
	})
}

// GetColdObjectsPrediction gets objects predicted to become cold
func (h *TieringHandler) GetColdObjectsPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := json.Number(limitStr).Int64(); err == nil && l > 0 && l <= 500 {
			limit = int(l)
		}
	}

	inactiveDays := 30
	if daysStr := r.URL.Query().Get("inactive_days"); daysStr != "" {
		if d, err := json.Number(daysStr).Int64(); err == nil && d > 0 && d <= 365 {
			inactiveDays = int(d)
		}
	}

	coldObjects := h.service.GetColdObjects(ctx, inactiveDays, limit)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cold_objects":  coldObjects,
		"count":         len(coldObjects),
		"inactive_days": inactiveDays,
	})
}

// GetAccessAnomalies gets detected access anomalies
func (h *TieringHandler) GetAccessAnomalies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := json.Number(limitStr).Int64(); err == nil && l > 0 && l <= 500 {
			limit = int(l)
		}
	}

	anomalies, err := h.service.GetAccessAnomalies(ctx, limit)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"anomalies": anomalies,
		"count":     len(anomalies),
	})
}
