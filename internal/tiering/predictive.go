package tiering

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

// PredictiveEngine provides ML-based access pattern prediction for tiering decisions
type PredictiveEngine struct {
	config     PredictiveConfig
	models     map[string]*ObjectModel
	mu         sync.RWMutex
	accessData *AccessTracker
}

// PredictiveConfig configures the predictive engine
type PredictiveConfig struct {
	// Minimum data points required for prediction
	MinDataPoints int

	// Time horizons for prediction (in hours)
	ShortTermHorizon  int // e.g., 24 hours
	MediumTermHorizon int // e.g., 7 days (168 hours)
	LongTermHorizon   int // e.g., 30 days (720 hours)

	// Model parameters
	SeasonalityPeriod    int     // Hours (e.g., 24 for daily, 168 for weekly)
	TrendSmoothingFactor float64 // Exponential smoothing alpha (0-1)
	SeasonalityWeight    float64 // Weight for seasonal component (0-1)

	// Thresholds for tier recommendations
	HotThreshold    float64 // Predicted daily accesses for hot tier
	WarmThreshold   float64 // Predicted daily accesses for warm tier
	ColdThreshold   float64 // Predicted daily accesses for cold tier
	ArchiveInactive int     // Days of predicted inactivity for archive

	// Confidence thresholds
	MinConfidenceForAction float64 // Minimum confidence to recommend tier change
}

// DefaultPredictiveConfig returns default configuration
func DefaultPredictiveConfig() PredictiveConfig {
	return PredictiveConfig{
		MinDataPoints:          10,
		ShortTermHorizon:       24,
		MediumTermHorizon:      168,
		LongTermHorizon:        720,
		SeasonalityPeriod:      24,
		TrendSmoothingFactor:   0.3,
		SeasonalityWeight:      0.2,
		HotThreshold:           5.0,  // 5+ accesses/day = hot
		WarmThreshold:          1.0,  // 1-5 accesses/day = warm
		ColdThreshold:          0.1,  // <1 access/day but some activity = cold
		ArchiveInactive:        90,   // 90 days predicted inactivity = archive
		MinConfidenceForAction: 0.7,
	}
}

// ObjectModel stores learned patterns for an object
type ObjectModel struct {
	Bucket string
	Key    string

	// Time series data
	HourlyAccesses []float64 // Access counts per hour
	LastUpdated    time.Time

	// Learned patterns
	TrendCoefficient float64   // Linear trend slope
	TrendIntercept   float64   // Linear trend intercept
	SeasonalFactors  []float64 // Seasonal adjustment factors (hourly)
	Volatility       float64   // Standard deviation of residuals

	// Predictions
	PredictedAccessRate  float64   // Predicted accesses per day
	PredictionConfidence float64   // Confidence in prediction (0-1)
	RecommendedTier      TierType  // Recommended tier based on prediction
	PredictionTime       time.Time // When prediction was made
}

// AccessPrediction represents a prediction for an object
type AccessPrediction struct {
	Bucket    string
	Key       string
	Timestamp time.Time

	// Short-term prediction (next 24h)
	ShortTermAccessRate float64
	ShortTermConfidence float64

	// Medium-term prediction (next 7 days)
	MediumTermAccessRate float64
	MediumTermConfidence float64

	// Long-term prediction (next 30 days)
	LongTermAccessRate float64
	LongTermConfidence float64

	// Trend analysis
	TrendDirection string  // "increasing", "declining", "stable"
	TrendStrength  float64 // 0-1, how strong the trend is

	// Seasonality analysis
	HasDailyPattern   bool
	HasWeeklyPattern  bool
	PeakAccessHour    int // Hour of day with most accesses
	PeakAccessDay     int // Day of week with most accesses

	// Tier recommendation
	CurrentTier     TierType
	RecommendedTier TierType
	Confidence      float64
	Reasoning       string
}

// NewPredictiveEngine creates a new predictive engine
func NewPredictiveEngine(config PredictiveConfig, accessData *AccessTracker) *PredictiveEngine {
	return &PredictiveEngine{
		config:     config,
		models:     make(map[string]*ObjectModel),
		accessData: accessData,
	}
}

// Predict generates an access prediction for an object
func (e *PredictiveEngine) Predict(ctx context.Context, bucket, key string) (*AccessPrediction, error) {
	// Get or create model
	model := e.getOrCreateModel(bucket, key)

	// Update model with latest data
	if err := e.updateModel(ctx, model); err != nil {
		return nil, err
	}

	// Generate predictions
	prediction := e.generatePrediction(model)

	return prediction, nil
}

// PredictBatch generates predictions for multiple objects
func (e *PredictiveEngine) PredictBatch(ctx context.Context, objects []ObjectIdentifier) ([]*AccessPrediction, error) {
	predictions := make([]*AccessPrediction, 0, len(objects))

	for _, obj := range objects {
		pred, err := e.Predict(ctx, obj.Bucket, obj.Key)
		if err != nil {
			continue // Skip failed predictions
		}
		predictions = append(predictions, pred)
	}

	return predictions, nil
}

// ObjectIdentifier identifies an object
type ObjectIdentifier struct {
	Bucket string
	Key    string
}

// GetTierRecommendations returns tier recommendations based on predictions
func (e *PredictiveEngine) GetTierRecommendations(ctx context.Context, limit int) ([]*TierRecommendation, error) {
	recommendations := make([]*TierRecommendation, 0)

	e.mu.RLock()
	for _, model := range e.models {
		if model.PredictionConfidence >= e.config.MinConfidenceForAction {
			rec := &TierRecommendation{
				Bucket:          model.Bucket,
				Key:             model.Key,
				CurrentTier:     e.getCurrentTier(ctx, model.Bucket, model.Key),
				RecommendedTier: model.RecommendedTier,
				Confidence:      model.PredictionConfidence,
				PredictedAccess: model.PredictedAccessRate,
				Reasoning:       e.generateReasoningText(model),
			}

			// Only recommend if tier change is needed
			if rec.CurrentTier != rec.RecommendedTier {
				recommendations = append(recommendations, rec)
			}
		}
	}
	e.mu.RUnlock()

	// Sort by confidence
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Confidence > recommendations[j].Confidence
	})

	if len(recommendations) > limit {
		recommendations = recommendations[:limit]
	}

	return recommendations, nil
}

// TierRecommendation represents a recommendation to change an object's tier
type TierRecommendation struct {
	Bucket          string
	Key             string
	CurrentTier     TierType
	RecommendedTier TierType
	Confidence      float64
	PredictedAccess float64
	Reasoning       string
}

// getOrCreateModel gets or creates a model for an object
func (e *PredictiveEngine) getOrCreateModel(bucket, key string) *ObjectModel {
	fullKey := bucket + "/" + key

	e.mu.Lock()
	defer e.mu.Unlock()

	if model, exists := e.models[fullKey]; exists {
		return model
	}

	model := &ObjectModel{
		Bucket:          bucket,
		Key:             key,
		HourlyAccesses:  make([]float64, 0),
		SeasonalFactors: make([]float64, e.config.SeasonalityPeriod),
	}

	// Initialize seasonal factors to 1.0
	for i := range model.SeasonalFactors {
		model.SeasonalFactors[i] = 1.0
	}

	e.models[fullKey] = model
	return model
}

// updateModel updates the model with latest access data
func (e *PredictiveEngine) updateModel(ctx context.Context, model *ObjectModel) error {
	// Get access stats
	stats, err := e.accessData.GetStats(ctx, model.Bucket, model.Key)
	if err != nil || stats == nil {
		return err
	}

	// Convert access history to hourly buckets
	hourlyData := e.aggregateToHourly(stats.AccessHistory)
	model.HourlyAccesses = hourlyData

	// Need minimum data points
	if len(hourlyData) < e.config.MinDataPoints {
		model.PredictionConfidence = 0
		return nil
	}

	// Decompose time series
	trend, seasonal, residual := e.decomposeTimeSeries(hourlyData)

	// Calculate trend using linear regression
	model.TrendCoefficient, model.TrendIntercept = e.linearRegression(trend)

	// Store seasonal factors
	if len(seasonal) >= e.config.SeasonalityPeriod {
		for i := 0; i < e.config.SeasonalityPeriod; i++ {
			model.SeasonalFactors[i] = seasonal[i%len(seasonal)]
		}
	}

	// Calculate volatility (standard deviation of residuals)
	model.Volatility = e.standardDeviation(residual)

	// Calculate prediction confidence
	model.PredictionConfidence = e.calculateConfidence(model)

	// Make prediction
	model.PredictedAccessRate = e.predictAccessRate(model)
	model.RecommendedTier = e.recommendTier(model.PredictedAccessRate)
	model.PredictionTime = time.Now()
	model.LastUpdated = time.Now()

	return nil
}

// aggregateToHourly converts access records to hourly counts
func (e *PredictiveEngine) aggregateToHourly(records []AccessRecord) []float64 {
	if len(records) == 0 {
		return nil
	}

	// Find time range
	minTime := records[0].Timestamp
	maxTime := records[0].Timestamp
	for _, r := range records {
		if r.Timestamp.Before(minTime) {
			minTime = r.Timestamp
		}
		if r.Timestamp.After(maxTime) {
			maxTime = r.Timestamp
		}
	}

	// Calculate number of hours
	hours := int(maxTime.Sub(minTime).Hours()) + 1
	if hours <= 0 {
		hours = 1
	}
	if hours > 720 { // Cap at 30 days
		hours = 720
		minTime = maxTime.Add(-720 * time.Hour)
	}

	// Aggregate to hourly buckets
	hourly := make([]float64, hours)
	for _, r := range records {
		if r.Timestamp.Before(minTime) {
			continue
		}
		hourIndex := int(r.Timestamp.Sub(minTime).Hours())
		if hourIndex >= 0 && hourIndex < hours {
			hourly[hourIndex]++
		}
	}

	return hourly
}

// decomposeTimeSeries performs STL-like decomposition
func (e *PredictiveEngine) decomposeTimeSeries(data []float64) (trend, seasonal, residual []float64) {
	n := len(data)
	if n == 0 {
		return nil, nil, nil
	}

	// Calculate trend using moving average
	trend = make([]float64, n)
	window := e.config.SeasonalityPeriod
	if window > n {
		window = n
	}

	for i := 0; i < n; i++ {
		start := i - window/2
		end := i + window/2 + 1
		if start < 0 {
			start = 0
		}
		if end > n {
			end = n
		}

		sum := 0.0
		for j := start; j < end; j++ {
			sum += data[j]
		}
		trend[i] = sum / float64(end-start)
	}

	// Calculate seasonal component
	seasonal = make([]float64, n)
	detrended := make([]float64, n)
	for i := 0; i < n; i++ {
		detrended[i] = data[i] - trend[i]
	}

	// Average seasonal effect for each position in cycle
	period := e.config.SeasonalityPeriod
	seasonalAvg := make([]float64, period)
	seasonalCount := make([]int, period)

	for i := 0; i < n; i++ {
		pos := i % period
		seasonalAvg[pos] += detrended[i]
		seasonalCount[pos]++
	}

	for i := 0; i < period; i++ {
		if seasonalCount[i] > 0 {
			seasonalAvg[i] /= float64(seasonalCount[i])
		}
	}

	// Assign seasonal values
	for i := 0; i < n; i++ {
		seasonal[i] = seasonalAvg[i%period]
	}

	// Calculate residual
	residual = make([]float64, n)
	for i := 0; i < n; i++ {
		residual[i] = data[i] - trend[i] - seasonal[i]
	}

	return trend, seasonal, residual
}

// linearRegression performs simple linear regression
func (e *PredictiveEngine) linearRegression(data []float64) (slope, intercept float64) {
	n := len(data)
	if n == 0 {
		return 0, 0
	}

	// Calculate means
	sumX := 0.0
	sumY := 0.0
	for i, y := range data {
		sumX += float64(i)
		sumY += y
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	// Calculate slope and intercept
	numerator := 0.0
	denominator := 0.0
	for i, y := range data {
		x := float64(i)
		numerator += (x - meanX) * (y - meanY)
		denominator += (x - meanX) * (x - meanX)
	}

	if denominator == 0 {
		return 0, meanY
	}

	slope = numerator / denominator
	intercept = meanY - slope*meanX

	return slope, intercept
}

// standardDeviation calculates standard deviation
func (e *PredictiveEngine) standardDeviation(data []float64) float64 {
	n := len(data)
	if n == 0 {
		return 0
	}

	// Calculate mean
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(n)

	// Calculate variance
	variance := 0.0
	for _, v := range data {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n)

	return math.Sqrt(variance)
}

// calculateConfidence calculates prediction confidence
func (e *PredictiveEngine) calculateConfidence(model *ObjectModel) float64 {
	n := len(model.HourlyAccesses)
	if n < e.config.MinDataPoints {
		return 0
	}

	// Factors that increase confidence:
	// 1. More data points
	dataFactor := math.Min(float64(n)/168.0, 1.0) // Max at 1 week of data

	// 2. Lower volatility relative to mean
	mean := 0.0
	for _, v := range model.HourlyAccesses {
		mean += v
	}
	mean /= float64(n)

	volatilityFactor := 1.0
	if mean > 0 {
		cv := model.Volatility / mean // Coefficient of variation
		volatilityFactor = math.Max(0, 1.0-cv)
	}

	// 3. Strong trend (either direction)
	trendFactor := math.Min(math.Abs(model.TrendCoefficient)*10, 0.3)

	// Combine factors
	confidence := 0.5*dataFactor + 0.3*volatilityFactor + 0.2*trendFactor

	return math.Min(confidence, 1.0)
}

// predictAccessRate predicts average daily access rate
func (e *PredictiveEngine) predictAccessRate(model *ObjectModel) float64 {
	n := len(model.HourlyAccesses)
	if n == 0 {
		return 0
	}

	// Use trend to predict next period
	futureHours := float64(e.config.ShortTermHorizon)
	predictedHourly := model.TrendIntercept + model.TrendCoefficient*float64(n+int(futureHours)/2)

	// Apply seasonal adjustment (average seasonal factor)
	avgSeasonal := 0.0
	for _, s := range model.SeasonalFactors {
		avgSeasonal += s
	}
	avgSeasonal /= float64(len(model.SeasonalFactors))

	predictedHourly *= avgSeasonal

	// Convert to daily rate (ensure non-negative)
	dailyRate := math.Max(0, predictedHourly*24)

	return dailyRate
}

// recommendTier recommends a tier based on predicted access rate
func (e *PredictiveEngine) recommendTier(predictedDailyAccess float64) TierType {
	if predictedDailyAccess >= e.config.HotThreshold {
		return TierHot
	}
	if predictedDailyAccess >= e.config.WarmThreshold {
		return TierWarm
	}
	if predictedDailyAccess >= e.config.ColdThreshold {
		return TierCold
	}
	return TierArchive
}

// generatePrediction generates a full prediction from the model
func (e *PredictiveEngine) generatePrediction(model *ObjectModel) *AccessPrediction {
	prediction := &AccessPrediction{
		Bucket:    model.Bucket,
		Key:       model.Key,
		Timestamp: time.Now(),
	}

	// Short-term prediction
	prediction.ShortTermAccessRate = model.PredictedAccessRate
	prediction.ShortTermConfidence = model.PredictionConfidence

	// Medium-term prediction (with lower confidence)
	prediction.MediumTermAccessRate = e.predictAtHorizon(model, e.config.MediumTermHorizon)
	prediction.MediumTermConfidence = model.PredictionConfidence * 0.8

	// Long-term prediction (with even lower confidence)
	prediction.LongTermAccessRate = e.predictAtHorizon(model, e.config.LongTermHorizon)
	prediction.LongTermConfidence = model.PredictionConfidence * 0.6

	// Trend analysis
	prediction.TrendDirection = e.getTrendDirection(model)
	prediction.TrendStrength = math.Min(math.Abs(model.TrendCoefficient)*100, 1.0)

	// Seasonality analysis
	prediction.HasDailyPattern = e.hasDailyPattern(model)
	prediction.HasWeeklyPattern = e.hasWeeklyPattern(model)
	prediction.PeakAccessHour = e.findPeakHour(model)
	prediction.PeakAccessDay = e.findPeakDay(model)

	// Tier recommendation
	prediction.RecommendedTier = model.RecommendedTier
	prediction.Confidence = model.PredictionConfidence
	prediction.Reasoning = e.generateReasoningText(model)

	return prediction
}

// predictAtHorizon predicts access rate at a future horizon
func (e *PredictiveEngine) predictAtHorizon(model *ObjectModel, horizonHours int) float64 {
	n := len(model.HourlyAccesses)
	predictedHourly := model.TrendIntercept + model.TrendCoefficient*float64(n+horizonHours/2)
	return math.Max(0, predictedHourly*24)
}

// getTrendDirection returns the trend direction
func (e *PredictiveEngine) getTrendDirection(model *ObjectModel) string {
	threshold := 0.001 // Minimum slope to consider as trend
	if model.TrendCoefficient > threshold {
		return "increasing"
	}
	if model.TrendCoefficient < -threshold {
		return "declining"
	}
	return "stable"
}

// hasDailyPattern checks if there's a daily access pattern
func (e *PredictiveEngine) hasDailyPattern(model *ObjectModel) bool {
	if len(model.SeasonalFactors) < 24 {
		return false
	}

	// Check variance in first 24 hours of seasonal factors
	factors := model.SeasonalFactors[:24]
	variance := e.standardDeviation(factors)

	return variance > 0.1 // Has pattern if variance is significant
}

// hasWeeklyPattern checks if there's a weekly access pattern
func (e *PredictiveEngine) hasWeeklyPattern(model *ObjectModel) bool {
	n := len(model.HourlyAccesses)
	if n < 168 { // Need at least a week of data
		return false
	}

	// Aggregate by day of week
	dailyTotals := make([]float64, 7)
	dailyCounts := make([]int, 7)

	startTime := time.Now().Add(-time.Duration(n) * time.Hour)
	for i, v := range model.HourlyAccesses {
		dayOfWeek := int(startTime.Add(time.Duration(i) * time.Hour).Weekday())
		dailyTotals[dayOfWeek] += v
		dailyCounts[dayOfWeek]++
	}

	// Calculate averages
	dailyAvgs := make([]float64, 7)
	for i := 0; i < 7; i++ {
		if dailyCounts[i] > 0 {
			dailyAvgs[i] = dailyTotals[i] / float64(dailyCounts[i])
		}
	}

	variance := e.standardDeviation(dailyAvgs)
	return variance > 0.2
}

// findPeakHour finds the hour with most accesses
func (e *PredictiveEngine) findPeakHour(model *ObjectModel) int {
	if len(model.SeasonalFactors) < 24 {
		return 12 // Default to noon
	}

	peakHour := 0
	peakValue := model.SeasonalFactors[0]

	for i := 1; i < 24 && i < len(model.SeasonalFactors); i++ {
		if model.SeasonalFactors[i] > peakValue {
			peakValue = model.SeasonalFactors[i]
			peakHour = i
		}
	}

	return peakHour
}

// findPeakDay finds the day with most accesses
func (e *PredictiveEngine) findPeakDay(model *ObjectModel) int {
	n := len(model.HourlyAccesses)
	if n < 168 {
		return 1 // Default to Monday
	}

	// Aggregate by day of week
	dailyTotals := make([]float64, 7)

	startTime := time.Now().Add(-time.Duration(n) * time.Hour)
	for i, v := range model.HourlyAccesses {
		dayOfWeek := int(startTime.Add(time.Duration(i) * time.Hour).Weekday())
		dailyTotals[dayOfWeek] += v
	}

	peakDay := 0
	peakValue := dailyTotals[0]
	for i := 1; i < 7; i++ {
		if dailyTotals[i] > peakValue {
			peakValue = dailyTotals[i]
			peakDay = i
		}
	}

	return peakDay
}

// generateReasoningText generates human-readable reasoning for the recommendation
func (e *PredictiveEngine) generateReasoningText(model *ObjectModel) string {
	trend := e.getTrendDirection(model)
	rate := model.PredictedAccessRate

	switch model.RecommendedTier {
	case TierHot:
		return "Predicted high access rate (" + formatFloat(rate) + "/day, " + trend + " trend). Recommend hot tier for optimal performance."
	case TierWarm:
		return "Predicted moderate access rate (" + formatFloat(rate) + "/day, " + trend + " trend). Recommend warm tier for balanced cost/performance."
	case TierCold:
		return "Predicted low access rate (" + formatFloat(rate) + "/day, " + trend + " trend). Recommend cold tier to reduce costs."
	case TierArchive:
		return "Predicted minimal access (" + formatFloat(rate) + "/day, " + trend + " trend). Recommend archive tier for long-term storage."
	default:
		return "Insufficient data for confident prediction."
	}
}

// getCurrentTier gets the current tier for an object
func (e *PredictiveEngine) getCurrentTier(ctx context.Context, bucket, key string) TierType {
	// This would normally query the tier manager
	// For now, default to hot
	return TierHot
}

// formatFloat formats a float for display
func formatFloat(f float64) string {
	if f >= 10 {
		return string(rune('0'+int(f/10))) + string(rune('0'+int(f)%10))
	}
	if f >= 1 {
		return string(rune('0' + int(f)))
	}
	return "0." + string(rune('0'+int(f*10)%10))
}

// AnomalyDetector detects unusual access patterns
type AnomalyDetector struct {
	config     AnomalyConfig
	baselines  map[string]*AccessBaseline
	anomalies  []*AccessAnomaly
	maxHistory int
	mu         sync.RWMutex
}

// AnomalyConfig configures anomaly detection
type AnomalyConfig struct {
	// Standard deviations for anomaly threshold
	AnomalyThreshold float64

	// Minimum data points for baseline
	MinBaselinePoints int

	// Baseline update interval
	BaselineUpdateInterval time.Duration
}

// DefaultAnomalyConfig returns default configuration
func DefaultAnomalyConfig() AnomalyConfig {
	return AnomalyConfig{
		AnomalyThreshold:       3.0, // 3 standard deviations
		MinBaselinePoints:      168, // 1 week of hourly data
		BaselineUpdateInterval: 24 * time.Hour,
	}
}

// AccessBaseline stores baseline access patterns
type AccessBaseline struct {
	Bucket       string
	Key          string
	HourlyMean   float64
	HourlyStdDev float64
	DailyMean    float64
	DailyStdDev  float64
	LastUpdated  time.Time
}

// AccessAnomaly represents an anomaly in access patterns
type AccessAnomaly struct {
	Bucket       string
	Key          string
	DetectedAt   time.Time
	AnomalyType  string  // "spike", "drop", "pattern_change"
	Severity     float64 // Standard deviations from normal
	Expected     float64
	Actual       float64
	Description  string
}

// NewAnomalyDetector creates a new anomaly detector
func NewAnomalyDetector(config AnomalyConfig) *AnomalyDetector {
	return &AnomalyDetector{
		config:     config,
		baselines:  make(map[string]*AccessBaseline),
		anomalies:  make([]*AccessAnomaly, 0),
		maxHistory: 1000, // Keep last 1000 anomalies
	}
}

// DetectAnomaly checks if current access pattern is anomalous
func (d *AnomalyDetector) DetectAnomaly(bucket, key string, recentAccessCount float64, hoursSinceLastCheck float64) *AccessAnomaly {
	fullKey := bucket + "/" + key

	d.mu.RLock()
	baseline, exists := d.baselines[fullKey]
	d.mu.RUnlock()

	if !exists {
		return nil
	}

	// Calculate expected accesses for the time period
	expected := baseline.HourlyMean * hoursSinceLastCheck

	// Calculate z-score
	// Use a minimum stdDev to handle constant baseline data
	// When stdDev is 0, use a small fraction of the mean as minimum stdDev
	stdDev := baseline.HourlyStdDev
	if stdDev == 0 {
		stdDev = math.Max(baseline.HourlyMean*0.1, 0.1) // 10% of mean or 0.1
	}

	zScore := (recentAccessCount - expected) / (stdDev * math.Sqrt(math.Max(hoursSinceLastCheck, 1)))

	// Check if anomalous
	if math.Abs(zScore) < d.config.AnomalyThreshold {
		return nil
	}

	anomaly := &AccessAnomaly{
		Bucket:     bucket,
		Key:        key,
		DetectedAt: time.Now(),
		Severity:   math.Abs(zScore),
		Expected:   expected,
		Actual:     recentAccessCount,
	}

	if zScore > 0 {
		anomaly.AnomalyType = "spike"
		anomaly.Description = "Access rate significantly higher than baseline"
	} else {
		anomaly.AnomalyType = "drop"
		anomaly.Description = "Access rate significantly lower than baseline"
	}

	// Store the anomaly
	d.mu.Lock()
	d.anomalies = append(d.anomalies, anomaly)
	// Trim if we exceed max history
	if len(d.anomalies) > d.maxHistory {
		d.anomalies = d.anomalies[len(d.anomalies)-d.maxHistory:]
	}
	d.mu.Unlock()

	return anomaly
}

// GetAnomalies returns the most recent anomalies
func (d *AnomalyDetector) GetAnomalies(limit int) ([]*AccessAnomaly, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.anomalies) == 0 {
		return []*AccessAnomaly{}, nil
	}

	// Return most recent anomalies (up to limit)
	start := 0
	if len(d.anomalies) > limit {
		start = len(d.anomalies) - limit
	}

	result := make([]*AccessAnomaly, len(d.anomalies)-start)
	copy(result, d.anomalies[start:])

	// Reverse to show most recent first
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result, nil
}

// UpdateBaseline updates the baseline for an object
func (d *AnomalyDetector) UpdateBaseline(bucket, key string, hourlyData []float64) {
	if len(hourlyData) < d.config.MinBaselinePoints {
		return
	}

	fullKey := bucket + "/" + key

	// Calculate hourly statistics
	sum := 0.0
	for _, v := range hourlyData {
		sum += v
	}
	mean := sum / float64(len(hourlyData))

	variance := 0.0
	for _, v := range hourlyData {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(hourlyData))
	stdDev := math.Sqrt(variance)

	// Calculate daily statistics
	numDays := len(hourlyData) / 24
	dailyTotals := make([]float64, numDays)
	for i := 0; i < numDays; i++ {
		for j := 0; j < 24; j++ {
			idx := i*24 + j
			if idx < len(hourlyData) {
				dailyTotals[i] += hourlyData[idx]
			}
		}
	}

	dailySum := 0.0
	for _, v := range dailyTotals {
		dailySum += v
	}
	dailyMean := dailySum / float64(numDays)

	dailyVariance := 0.0
	for _, v := range dailyTotals {
		diff := v - dailyMean
		dailyVariance += diff * diff
	}
	dailyVariance /= float64(numDays)
	dailyStdDev := math.Sqrt(dailyVariance)

	d.mu.Lock()
	d.baselines[fullKey] = &AccessBaseline{
		Bucket:       bucket,
		Key:          key,
		HourlyMean:   mean,
		HourlyStdDev: stdDev,
		DailyMean:    dailyMean,
		DailyStdDev:  dailyStdDev,
		LastUpdated:  time.Now(),
	}
	d.mu.Unlock()
}
