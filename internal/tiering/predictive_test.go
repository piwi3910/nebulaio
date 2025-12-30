package tiering

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestPredictiveEngine(t *testing.T) {
	config := DefaultPredictiveConfig()
	config.MinDataPoints = 5 // Lower for testing

	// Create access tracker
	trackerConfig := DefaultAccessTrackerConfig()
	trackerConfig.MaxEntries = 1000
	tracker := NewAccessTracker(trackerConfig)

	engine := NewPredictiveEngine(config, tracker)

	t.Run("prediction with no data", func(t *testing.T) {
		pred, err := engine.Predict(context.Background(), "test-bucket", "no-data-key")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if pred == nil {
			t.Fatal("expected prediction, got nil")
		}

		if pred.Confidence != 0 {
			t.Errorf("expected 0 confidence with no data, got %f", pred.Confidence)
		}
	})

	t.Run("prediction with access data", func(t *testing.T) {
		bucket := "test-bucket"
		key := "active-key"
		ctx := context.Background()

		// Simulate access history
		for range 100 {
			tracker.RecordAccess(ctx, bucket, key, "GET", 1024)
		}

		pred, err := engine.Predict(ctx, bucket, key)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if pred == nil {
			t.Fatal("expected prediction, got nil")
		}

		// With data, should have some confidence
		// Note: confidence may still be low with limited data
		if pred.ShortTermAccessRate < 0 {
			t.Errorf("expected non-negative access rate, got %f", pred.ShortTermAccessRate)
		}
	})
}

func TestLinearRegression(t *testing.T) {
	engine := NewPredictiveEngine(DefaultPredictiveConfig(), nil)

	t.Run("increasing trend", func(t *testing.T) {
		data := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
		slope, intercept := engine.linearRegression(data)

		// Expected: y = 1*x + 1 (for 0-indexed)
		if math.Abs(slope-1.0) > 0.01 {
			t.Errorf("expected slope ~1.0, got %f", slope)
		}

		if math.Abs(intercept-1.0) > 0.1 {
			t.Errorf("expected intercept ~1.0, got %f", intercept)
		}
	})

	t.Run("decreasing trend", func(t *testing.T) {
		data := []float64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
		slope, _ := engine.linearRegression(data)

		if slope >= 0 {
			t.Errorf("expected negative slope, got %f", slope)
		}
	})

	t.Run("flat trend", func(t *testing.T) {
		data := []float64{5, 5, 5, 5, 5, 5, 5, 5, 5, 5}
		slope, intercept := engine.linearRegression(data)

		if math.Abs(slope) > 0.01 {
			t.Errorf("expected slope ~0, got %f", slope)
		}

		if math.Abs(intercept-5.0) > 0.01 {
			t.Errorf("expected intercept ~5.0, got %f", intercept)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		data := []float64{}
		slope, intercept := engine.linearRegression(data)

		if slope != 0 || intercept != 0 {
			t.Errorf("expected (0,0), got (%f,%f)", slope, intercept)
		}
	})
}

func TestStandardDeviation(t *testing.T) {
	engine := NewPredictiveEngine(DefaultPredictiveConfig(), nil)

	t.Run("constant values", func(t *testing.T) {
		data := []float64{5, 5, 5, 5, 5}
		stdDev := engine.standardDeviation(data)

		if stdDev != 0 {
			t.Errorf("expected stddev 0, got %f", stdDev)
		}
	})

	t.Run("varying values", func(t *testing.T) {
		data := []float64{1, 2, 3, 4, 5}
		stdDev := engine.standardDeviation(data)

		// Standard deviation should be positive for varying data
		if stdDev <= 0 {
			t.Errorf("expected positive stddev, got %f", stdDev)
		}

		// For 1,2,3,4,5: mean=3, variance=2, stddev≈1.414
		expected := math.Sqrt(2)
		if math.Abs(stdDev-expected) > 0.01 {
			t.Errorf("expected stddev ~%f, got %f", expected, stdDev)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		data := []float64{}
		stdDev := engine.standardDeviation(data)

		if stdDev != 0 {
			t.Errorf("expected 0, got %f", stdDev)
		}
	})
}

func TestDecomposeTimeSeries(t *testing.T) {
	engine := NewPredictiveEngine(DefaultPredictiveConfig(), nil)

	t.Run("simple series", func(t *testing.T) {
		// Generate simple data with trend + seasonal
		data := make([]float64, 48)
		for i := range data {
			data[i] = float64(i/24) + math.Sin(float64(i)*math.Pi/12) // Trend + daily pattern
		}

		trend, seasonal, residual := engine.decomposeTimeSeries(data)

		if len(trend) != len(data) {
			t.Errorf("expected trend length %d, got %d", len(data), len(trend))
		}

		if len(seasonal) != len(data) {
			t.Errorf("expected seasonal length %d, got %d", len(data), len(seasonal))
		}

		if len(residual) != len(data) {
			t.Errorf("expected residual length %d, got %d", len(data), len(residual))
		}
	})

	t.Run("empty data", func(t *testing.T) {
		trend, seasonal, residual := engine.decomposeTimeSeries([]float64{})

		if trend != nil || seasonal != nil || residual != nil {
			t.Error("expected nil slices for empty data")
		}
	})
}

func TestTierRecommendation(t *testing.T) {
	config := DefaultPredictiveConfig()
	engine := NewPredictiveEngine(config, nil)

	tests := []struct {
		name         string
		expectedTier TierType
		dailyAccess  float64
	}{
		{"high access -> hot", TierHot, 10.0},
		{"moderate access -> warm", TierWarm, 2.0},
		{"low access -> cold", TierCold, 0.5},
		{"minimal access -> archive", TierArchive, 0.01},
		{"zero access -> archive", TierArchive, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := engine.recommendTier(tt.dailyAccess)
			if tier != tt.expectedTier {
				t.Errorf("expected %s, got %s", tt.expectedTier, tier)
			}
		})
	}
}

func TestAnomalyDetector(t *testing.T) {
	config := DefaultAnomalyConfig()
	config.MinBaselinePoints = 24 // Lower for testing
	detector := NewAnomalyDetector(config)

	t.Run("no baseline", func(t *testing.T) {
		anomaly := detector.DetectAnomaly("bucket", "key", 100, 1)
		if anomaly != nil {
			t.Error("expected no anomaly without baseline")
		}
	})

	t.Run("with baseline - normal", func(t *testing.T) {
		// Create baseline
		hourlyData := make([]float64, 24)
		for i := range hourlyData {
			hourlyData[i] = 10 // Consistent 10 accesses per hour
		}

		detector.UpdateBaseline("bucket", "key", hourlyData)

		// Normal access (within 3 std dev)
		anomaly := detector.DetectAnomaly("bucket", "key", 10, 1)
		if anomaly != nil {
			t.Error("expected no anomaly for normal access")
		}
	})

	t.Run("with baseline - spike", func(t *testing.T) {
		// Create baseline with low variance
		hourlyData := make([]float64, 48)
		for i := range hourlyData {
			hourlyData[i] = 10.0
		}

		detector.UpdateBaseline("bucket", "spike-key", hourlyData)

		// Massive spike (way beyond 3 std dev)
		anomaly := detector.DetectAnomaly("bucket", "spike-key", 1000, 1)
		if anomaly == nil {
			t.Error("expected anomaly for massive spike")
		} else if anomaly.AnomalyType != "spike" {
			t.Errorf("expected spike type, got %s", anomaly.AnomalyType)
		}
	})
}

func TestAggregateToHourly(t *testing.T) {
	engine := NewPredictiveEngine(DefaultPredictiveConfig(), nil)

	t.Run("empty records", func(t *testing.T) {
		result := engine.aggregateToHourly(nil)
		if result != nil {
			t.Errorf("expected nil for empty records, got %v", result)
		}
	})

	t.Run("single record", func(t *testing.T) {
		records := []AccessRecord{
			{Timestamp: time.Now()},
		}

		result := engine.aggregateToHourly(records)
		if len(result) != 1 {
			t.Errorf("expected 1 hour bucket, got %d", len(result))
		}

		if result[0] != 1 {
			t.Errorf("expected count 1, got %f", result[0])
		}
	})

	t.Run("multiple records same hour", func(t *testing.T) {
		now := time.Now()
		records := []AccessRecord{
			{Timestamp: now},
			{Timestamp: now.Add(5 * time.Minute)},
			{Timestamp: now.Add(30 * time.Minute)},
		}

		result := engine.aggregateToHourly(records)
		if len(result) != 1 {
			t.Errorf("expected 1 hour bucket, got %d", len(result))
		}

		if result[0] != 3 {
			t.Errorf("expected count 3, got %f", result[0])
		}
	})

	t.Run("multiple records different hours", func(t *testing.T) {
		now := time.Now()
		records := []AccessRecord{
			{Timestamp: now},
			{Timestamp: now.Add(-1 * time.Hour)},
			{Timestamp: now.Add(-2 * time.Hour)},
		}

		result := engine.aggregateToHourly(records)
		if len(result) != 3 {
			t.Errorf("expected 3 hour buckets, got %d", len(result))
		}
	})
}

func TestTrendDirection(t *testing.T) {
	engine := NewPredictiveEngine(DefaultPredictiveConfig(), nil)

	tests := []struct {
		name     string
		expected string
		slope    float64
	}{
		{"increasing", "increasing", 0.01},
		{"declining", "declining", -0.01},
		{"stable_positive", "stable", 0.0005},
		{"stable_negative", "stable", -0.0005},
		{"zero", "stable", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &ObjectModel{
				TrendCoefficient: tt.slope,
			}

			result := engine.getTrendDirection(model)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		expected string
		input    float64
	}{
		{"15", 15.0},
		{"5", 5.0},
		{"0.5", 0.5},
		{"0.1", 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatFloat(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
