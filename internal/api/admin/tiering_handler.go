package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/piwi3910/nebulaio/internal/tiering"
)

// TieringRouteHandlers defines the interface for tiering route handlers.
// This interface is used to register routes for both production and test handlers.
type TieringRouteHandlers interface {
	ListTieringPolicies(w http.ResponseWriter, r *http.Request)
	CreateTieringPolicy(w http.ResponseWriter, r *http.Request)
	GetTieringPolicy(w http.ResponseWriter, r *http.Request)
	UpdateTieringPolicy(w http.ResponseWriter, r *http.Request)
	DeleteTieringPolicy(w http.ResponseWriter, r *http.Request)
	EnableTieringPolicy(w http.ResponseWriter, r *http.Request)
	DisableTieringPolicy(w http.ResponseWriter, r *http.Request)
	GetTieringPolicyStats(w http.ResponseWriter, r *http.Request)
	GetBucketAccessStats(w http.ResponseWriter, r *http.Request)
	GetObjectAccessStats(w http.ResponseWriter, r *http.Request)
	ManualTransition(w http.ResponseWriter, r *http.Request)
	GetS3Lifecycle(w http.ResponseWriter, r *http.Request)
	PutS3Lifecycle(w http.ResponseWriter, r *http.Request)
	DeleteS3Lifecycle(w http.ResponseWriter, r *http.Request)
	GetTieringStatus(w http.ResponseWriter, r *http.Request)
	GetTieringMetrics(w http.ResponseWriter, r *http.Request)
	GetAccessPrediction(w http.ResponseWriter, r *http.Request)
	GetTierRecommendations(w http.ResponseWriter, r *http.Request)
	GetHotObjectsPrediction(w http.ResponseWriter, r *http.Request)
	GetColdObjectsPrediction(w http.ResponseWriter, r *http.Request)
	GetAccessAnomalies(w http.ResponseWriter, r *http.Request)
}

// RegisterTieringRoutesForHandler registers tiering routes for any handler implementing TieringRouteHandlers.
func RegisterTieringRoutesForHandler(r chi.Router, h TieringRouteHandlers, policyPrefix string) {
	// Tiering Policies
	r.Get(policyPrefix, h.ListTieringPolicies)
	r.Post(policyPrefix, h.CreateTieringPolicy)
	r.Get(policyPrefix+"/{id}", h.GetTieringPolicy)
	r.Put(policyPrefix+"/{id}", h.UpdateTieringPolicy)
	r.Delete(policyPrefix+"/{id}", h.DeleteTieringPolicy)
	r.Post(policyPrefix+"/{id}/enable", h.EnableTieringPolicy)
	r.Post(policyPrefix+"/{id}/disable", h.DisableTieringPolicy)
	r.Get(policyPrefix+"/{id}/stats", h.GetTieringPolicyStats)

	// Access Stats
	r.Get("/tiering/access-stats/{bucket}", h.GetBucketAccessStats)
	r.Get("/tiering/access-stats/{bucket}/*", h.GetObjectAccessStats)

	// Manual Transitions
	r.Post("/tiering/transition", h.ManualTransition)

	// S3 Lifecycle Compatibility
	r.Get("/tiering/s3-lifecycle/{bucket}", h.GetS3Lifecycle)
	r.Put("/tiering/s3-lifecycle/{bucket}", h.PutS3Lifecycle)
	r.Delete("/tiering/s3-lifecycle/{bucket}", h.DeleteS3Lifecycle)

	// Tiering Status
	r.Get("/tiering/status", h.GetTieringStatus)
	r.Get("/tiering/metrics", h.GetTieringMetrics)

	// Predictive Tiering
	r.Get("/tiering/predictions/{bucket}/*", h.GetAccessPrediction)
	r.Get("/tiering/predictions/recommendations", h.GetTierRecommendations)
	r.Get("/tiering/predictions/hot-objects", h.GetHotObjectsPrediction)
	r.Get("/tiering/predictions/cold-objects", h.GetColdObjectsPrediction)
	r.Get("/tiering/anomalies", h.GetAccessAnomalies)
}

// TieringHandler handles tiering policy API requests.
type TieringHandler struct {
	service tiering.AdvancedServiceInterface
}

// NewTieringHandler creates a new tiering policy handler.
func NewTieringHandler(service tiering.AdvancedServiceInterface) *TieringHandler {
	return &TieringHandler{
		service: service,
	}
}

// RegisterTieringRoutes registers tiering policy routes.
func (h *TieringHandler) RegisterTieringRoutes(r chi.Router) {
	RegisterTieringRoutesForHandler(r, h, "/tiering-policies")
}

// ====================
// Policy CRUD Handlers
// ====================

// TieringPolicyListResponse represents the list response.
type TieringPolicyListResponse struct {
	Policies   []*tiering.AdvancedPolicy `json:"policies"`
	TotalCount int                       `json:"total_count"`
}

// ListTieringPolicies lists all tiering policies.
func (h *TieringHandler) ListTieringPolicies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for filtering
	policyType := r.URL.Query().Get("type")
	scope := r.URL.Query().Get("scope")
	enabled := r.URL.Query().Get("enabled")

	var (
		policies []*tiering.AdvancedPolicy
		err      error
	)

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

// SimpleTieringTrigger is the simplified trigger format from the frontend.
type SimpleTieringTrigger struct {
	Type            string `json:"type"`
	AgeDays         int    `json:"age_days,omitempty"`
	AccessCount     int    `json:"access_count,omitempty"`
	AccessDays      int    `json:"access_days,omitempty"`
	CapacityPercent int    `json:"capacity_percent,omitempty"`
}

// SimpleTieringAction is the simplified action format from the frontend.
type SimpleTieringAction struct {
	Type       string `json:"type"`
	TargetTier string `json:"target_tier,omitempty"`
	NotifyURL  string `json:"notify_url,omitempty"`
}

// SimpleTieringSchedule is the simplified schedule format from the frontend.
type SimpleTieringSchedule struct {
	MaintenanceWindows []string `json:"maintenance_windows,omitempty"`
	BlackoutWindows    []string `json:"blackout_windows,omitempty"`
}

// SimpleTieringAdvancedOptions is the simplified advanced options from the frontend.
type SimpleTieringAdvancedOptions struct {
	RateLimit            int  `json:"rate_limit,omitempty"`
	AntiThrashHours      int  `json:"anti_thrash_hours,omitempty"`
	DistributedExecution bool `json:"distributed_execution,omitempty"`
}

// CreateTieringPolicyRequest represents the create request (frontend format).
type CreateTieringPolicyRequest struct {
	Schedule        *SimpleTieringSchedule        `json:"schedule,omitempty"`
	AdvancedOptions *SimpleTieringAdvancedOptions `json:"advanced_options,omitempty"`
	PrefixPattern   string                        `json:"prefix_pattern,omitempty"`
	Type            tiering.PolicyType            `json:"type"`
	Scope           tiering.PolicyScope           `json:"scope"`
	BucketPattern   string                        `json:"bucket_pattern,omitempty"`
	ID              string                        `json:"id"`
	CronExpression  string                        `json:"cron_expression,omitempty"`
	Description     string                        `json:"description"`
	Name            string                        `json:"name"`
	Triggers        []SimpleTieringTrigger        `json:"triggers"`
	Actions         []SimpleTieringAction         `json:"actions"`
	Enabled         bool                          `json:"enabled"`
}

// convertSimpleTriggers converts the simple frontend trigger format to the internal format.
func convertSimpleTriggers(simpleTriggers []SimpleTieringTrigger) []tiering.PolicyTrigger {
	triggers := make([]tiering.PolicyTrigger, len(simpleTriggers))
	for i, st := range simpleTriggers {
		trigger := tiering.PolicyTrigger{
			Type: tiering.TriggerType(st.Type),
		}
		switch st.Type {
		case "age":
			trigger.Age = &tiering.AgeTrigger{
				DaysSinceAccess: st.AgeDays,
			}
		case "access":
			trigger.Access = &tiering.AccessTrigger{
				CountThreshold: st.AccessCount,
				PeriodMinutes:  st.AccessDays * 24 * 60, // Convert days to minutes
			}
		case "capacity":
			trigger.Capacity = &tiering.CapacityTrigger{
				HighWatermark: float64(st.CapacityPercent),
			}
		}

		triggers[i] = trigger
	}

	return triggers
}

// convertSimpleActions converts the simple frontend action format to the internal format.
func convertSimpleActions(simpleActions []SimpleTieringAction) []tiering.PolicyAction {
	actions := make([]tiering.PolicyAction, len(simpleActions))
	for i, sa := range simpleActions {
		action := tiering.PolicyAction{
			Type: tiering.PolicyActionType(sa.Type),
		}
		if sa.Type == "transition" {
			action.Transition = &tiering.TransitionActionConfig{
				TargetTier: tiering.TierType(sa.TargetTier),
			}
		}

		if sa.NotifyURL != "" {
			action.Notify = &tiering.NotifyActionConfig{
				Endpoint: sa.NotifyURL,
			}
		}

		actions[i] = action
	}

	return actions
}

// CreateTieringPolicy creates a new tiering policy.
func (h *TieringHandler) CreateTieringPolicy(w http.ResponseWriter, r *http.Request) {
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
		policyID = "policy-" + uuid.New().String()[:8]
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
	// MaintenanceWindows and BlackoutWindows need to be MaintenanceWindow type
	// For now, we'll leave them empty as conversion would be complex

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

	// Set defaults
	if policy.Type == "" {
		policy.Type = tiering.PolicyTypeScheduled
	}

	if policy.Scope == "" {
		policy.Scope = tiering.PolicyScopeGlobal
	}

	err = h.service.CreatePolicy(ctx, policy)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}

		if strings.Contains(err.Error(), "invalid") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusCreated, policy)
}

// GetTieringPolicy gets a tiering policy by ID.
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

// UpdateTieringPolicy updates a tiering policy.
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

	// Update fields
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

	if err := h.service.UpdatePolicy(ctx, existing); err != nil {
		if strings.Contains(err.Error(), "version conflict") {
			writeError(w, err.Error(), http.StatusConflict)
			return
		}

		if strings.Contains(err.Error(), "invalid") {
			writeError(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	writeJSON(w, http.StatusOK, existing)
}

// DeleteTieringPolicy deletes a tiering policy.
func (h *TieringHandler) DeleteTieringPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	err := h.service.DeletePolicy(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, err.Error(), http.StatusNotFound)
			return
		}

		writeError(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// EnableTieringPolicy enables a tiering policy.
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

// DisableTieringPolicy disables a tiering policy.
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

// GetTieringPolicyStats gets statistics for a tiering policy.
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

// ParsePaginationParams parses limit and offset query parameters with validation.
// Returns (limit, offset) with defaults of (100, 0) if not provided or invalid.
func ParsePaginationParams(r *http.Request) (limit int, offset int) {
	limit = 100
	offset = 0

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

	return limit, offset
}

// GetObjectKeyFromRequest extracts the object key from request, checking URL params and query string.
func GetObjectKeyFromRequest(r *http.Request) string {
	key := chi.URLParam(r, "*") // Catch-all for paths with slashes
	if queryKey := r.URL.Query().Get("key"); queryKey != "" {
		key = queryKey
	}
	return key
}

// WriteObjectAccessStatsResponse writes the access stats response for an object.
func WriteObjectAccessStatsResponse(w http.ResponseWriter, bucket, key string, stats *tiering.ObjectAccessStats) {
	if stats == nil {
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

// WriteManualTransitionResponse writes the success response for a manual transition.
func WriteManualTransitionResponse(w http.ResponseWriter, bucket, key string, targetTier tiering.TierType) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bucket":      bucket,
		"key":         key,
		"target_tier": targetTier,
		"message":     "Object transition initiated successfully",
	})
}

// ValidateTierType checks if a tier type is valid and returns an error if not.
func ValidateTierType(tier tiering.TierType) bool {
	validTiers := map[tiering.TierType]bool{
		tiering.TierHot:     true,
		tiering.TierWarm:    true,
		tiering.TierCold:    true,
		tiering.TierArchive: true,
	}
	return validTiers[tier]
}

// ParseLimitParam parses the limit query parameter with a given default and max.
func ParseLimitParam(r *http.Request, defaultLimit, maxLimit int) int {
	limit := defaultLimit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := json.Number(limitStr).Int64(); err == nil && l > 0 && l <= int64(maxLimit) {
			limit = int(l)
		}
	}
	return limit
}

// ParseInactiveDaysParam parses the inactive_days query parameter.
func ParseInactiveDaysParam(r *http.Request, defaultDays int) int {
	inactiveDays := defaultDays
	if daysStr := r.URL.Query().Get("inactive_days"); daysStr != "" {
		if d, err := json.Number(daysStr).Int64(); err == nil && d > 0 && d <= 365 {
			inactiveDays = int(d)
		}
	}
	return inactiveDays
}

// WriteColdObjectsResponse writes the cold objects prediction response.
func WriteColdObjectsResponse(w http.ResponseWriter, coldObjects interface{}, count, inactiveDays int) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cold_objects":  coldObjects,
		"count":         count,
		"inactive_days": inactiveDays,
	})
}

// GetBucketAccessStats gets access stats for all objects in a bucket.
func (h *TieringHandler) GetBucketAccessStats(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	limit, offset := ParsePaginationParams(r)

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

// GetObjectAccessStats gets access stats for a specific object.
func (h *TieringHandler) GetObjectAccessStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")
	key := GetObjectKeyFromRequest(r)

	stats, err := h.service.GetAccessStats(ctx, bucket, key)
	if err != nil {
		stats = nil
	}
	WriteObjectAccessStatsResponse(w, bucket, key, stats)
}

// ====================
// Manual Transition Handler
// ====================

// ManualTransitionRequest represents a manual transition request.
type ManualTransitionRequest struct {
	Bucket     string           `json:"bucket"`
	Key        string           `json:"key"`
	TargetTier tiering.TierType `json:"target_tier"`
	Force      bool             `json:"force"` // Skip anti-thrash checks
}

// ValidateManualTransitionRequest validates the manual transition request fields.
func ValidateManualTransitionRequest(w http.ResponseWriter, req *ManualTransitionRequest) bool {
	if req.Bucket == "" {
		writeError(w, "bucket is required", http.StatusBadRequest)
		return false
	}
	if req.Key == "" {
		writeError(w, "key is required", http.StatusBadRequest)
		return false
	}
	if req.TargetTier == "" {
		writeError(w, "target_tier is required", http.StatusBadRequest)
		return false
	}
	if !ValidateTierType(req.TargetTier) {
		writeError(w, "invalid target_tier: must be hot, warm, cold, or archive", http.StatusBadRequest)
		return false
	}
	return true
}

// ManualTransition manually transitions an object to a different tier.
func (h *TieringHandler) ManualTransition(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ManualTransitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if !ValidateManualTransitionRequest(w, &req) {
		return
	}

	if err := h.service.TransitionObject(ctx, req.Bucket, req.Key, req.TargetTier); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	WriteManualTransitionResponse(w, req.Bucket, req.Key, req.TargetTier)
}

// ====================
// S3 Lifecycle Compatibility Handlers
// ====================

// GetS3Lifecycle gets S3-compatible lifecycle configuration for a bucket.
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

// PutS3Lifecycle sets S3-compatible lifecycle configuration for a bucket.
func (h *TieringHandler) PutS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")

	contentType := r.Header.Get("Content-Type")

	var (
		config *tiering.S3LifecycleConfiguration
		err    error
	)

	if strings.Contains(contentType, "application/xml") || strings.Contains(contentType, "text/xml") {
		// Parse XML body
		xmlData := make([]byte, r.ContentLength)
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
		err := json.NewDecoder(r.Body).Decode(&config)
		if err != nil {
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

// DeleteS3Lifecycle deletes S3-compatible lifecycle configuration for a bucket.
func (h *TieringHandler) DeleteS3Lifecycle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")

	// Delete by setting empty configuration
	err := h.service.SetS3LifecycleConfiguration(ctx, bucket, &tiering.S3LifecycleConfiguration{})
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ====================
// Status and Metrics Handlers
// ====================

// GetTieringStatus gets the overall tiering system status.
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
		"status":            "running",
		"total_policies":    len(policies),
		"enabled_policies":  enabledCount,
		"policies_by_type":  byType,
		"policies_by_scope": byScope,
	}

	writeJSON(w, http.StatusOK, status)
}

// GetTieringMetrics gets tiering metrics.
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
		"total_executions":           totalExecutions,
		"total_objects_transitioned": totalObjectsTransitioned,
		"total_bytes_transitioned":   totalBytesTransitioned,
		"total_errors":               totalErrors,
		"policy_count":               len(policies),
	}

	writeJSON(w, http.StatusOK, metrics)
}

// ====================
// Predictive Tiering Handlers
// ====================

// GetAccessPrediction gets ML-based access prediction for an object.
func (h *TieringHandler) GetAccessPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*") // Catch-all for paths with slashes

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

// GetTierRecommendations gets ML-based tier recommendations.
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

// GetHotObjectsPrediction gets objects predicted to become hot.
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

// GetColdObjectsPrediction gets objects predicted to become cold.
func (h *TieringHandler) GetColdObjectsPrediction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := ParseLimitParam(r, 50, 500)
	inactiveDays := ParseInactiveDaysParam(r, 30)

	coldObjects := h.service.GetColdObjects(ctx, inactiveDays, limit)
	WriteColdObjectsResponse(w, coldObjects, len(coldObjects), inactiveDays)
}

// GetAccessAnomalies gets detected access anomalies.
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
