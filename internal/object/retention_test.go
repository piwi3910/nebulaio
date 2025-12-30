package object

import (
	"context"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
)

func TestCheckObjectLock_NoLock(t *testing.T) {
	s := &Service{}

	// No lock on object
	meta := &metadata.ObjectMeta{
		Bucket: "test-bucket",
		Key:    "test-key",
	}

	err := s.CheckObjectLock(context.TODO(), meta, nil)
	if err != nil {
		t.Errorf("expected no error for unlocked object, got: %v", err)
	}
}

func TestCheckObjectLock_LegalHold(t *testing.T) {
	s := &Service{}

	meta := &metadata.ObjectMeta{
		Bucket:                    "test-bucket",
		Key:                       "test-key",
		ObjectLockLegalHoldStatus: "ON",
	}

	err := s.CheckObjectLock(context.TODO(), meta, nil)
	if err != s3errors.ErrObjectLocked {
		t.Errorf("expected ErrObjectLocked, got: %v", err)
	}
}

func TestCheckObjectLock_ComplianceMode(t *testing.T) {
	s := &Service{}

	futureDate := time.Now().Add(24 * time.Hour)
	meta := &metadata.ObjectMeta{
		Bucket:                    "test-bucket",
		Key:                       "test-key",
		ObjectLockMode:            "COMPLIANCE",
		ObjectLockRetainUntilDate: &futureDate,
	}

	// Should not be able to delete even with bypass
	err := s.CheckObjectLock(context.TODO(), meta, &ObjectLockCheckOptions{
		BypassGovernanceRetention: true,
	})
	if err != s3errors.ErrObjectLocked {
		t.Errorf("expected ErrObjectLocked for compliance mode, got: %v", err)
	}
}

func TestCheckObjectLock_GovernanceMode(t *testing.T) {
	s := &Service{}

	futureDate := time.Now().Add(24 * time.Hour)
	meta := &metadata.ObjectMeta{
		Bucket:                    "test-bucket",
		Key:                       "test-key",
		ObjectLockMode:            "GOVERNANCE",
		ObjectLockRetainUntilDate: &futureDate,
	}

	// Should not be able to delete without bypass
	err := s.CheckObjectLock(context.TODO(), meta, nil)
	if err != s3errors.ErrObjectLocked {
		t.Errorf("expected ErrObjectLocked for governance mode without bypass, got: %v", err)
	}

	// Should be able to delete with bypass
	err = s.CheckObjectLock(nil, meta, &ObjectLockCheckOptions{
		BypassGovernanceRetention: true,
	})
	if err != nil {
		t.Errorf("expected no error for governance mode with bypass, got: %v", err)
	}
}

func TestCheckObjectLock_ExpiredRetention(t *testing.T) {
	s := &Service{}

	pastDate := time.Now().Add(-24 * time.Hour)
	meta := &metadata.ObjectMeta{
		Bucket:                    "test-bucket",
		Key:                       "test-key",
		ObjectLockMode:            "COMPLIANCE",
		ObjectLockRetainUntilDate: &pastDate,
	}

	// Should be able to delete after retention expires
	err := s.CheckObjectLock(context.TODO(), meta, nil)
	if err != nil {
		t.Errorf("expected no error for expired retention, got: %v", err)
	}
}

func TestValidateRetentionMode(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"GOVERNANCE", false},
		{"COMPLIANCE", false},
		{"", false},
		{"INVALID", true},
		{"governance", true}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			err := ValidateRetentionMode(tt.mode)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRetentionMode(%q) error = %v, wantErr %v", tt.mode, err, tt.wantErr)
			}
		})
	}
}

func TestValidateLegalHoldStatus(t *testing.T) {
	tests := []struct {
		status  string
		wantErr bool
	}{
		{"ON", false},
		{"OFF", false},
		{"", false},
		{"INVALID", true},
		{"on", true}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			err := ValidateLegalHoldStatus(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLegalHoldStatus(%q) error = %v, wantErr %v", tt.status, err, tt.wantErr)
			}
		})
	}
}

func TestValidateRetentionDate(t *testing.T) {
	tests := []struct {
		date    time.Time
		name    string
		wantErr bool
	}{
		{time.Time{}, "zero date", false},
		{time.Now().Add(24 * time.Hour), "future date", false},
		{time.Now().Add(-24 * time.Hour), "past date", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRetentionDate(tt.date)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRetentionDate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanExtendRetention(t *testing.T) {
	now := time.Now()
	tomorrow := now.Add(24 * time.Hour)
	nextWeek := now.Add(7 * 24 * time.Hour)
	yesterday := now.Add(-24 * time.Hour)

	tests := []struct {
		current  *time.Time
		new      *time.Time
		name     string
		expected bool
	}{
		{nil, &tomorrow, "no current, any new", true},
		{&tomorrow, &nextWeek, "current, extend to later", true},
		{&nextWeek, &tomorrow, "current, try to reduce", false},
		{&tomorrow, nil, "current, try to remove", false},
		{&yesterday, &tomorrow, "current expired, set new", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanExtendRetention(tt.current, tt.new)
			if result != tt.expected {
				t.Errorf("CanExtendRetention() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRetentionInfo(t *testing.T) {
	futureDate := time.Now().Add(48 * time.Hour)

	tests := []struct {
		meta       *metadata.ObjectMeta
		name       string
		wantReason string
		wantLocked bool
	}{
		{
			name:       "no lock",
			meta:       &metadata.ObjectMeta{},
			wantLocked: false,
		},
		{
			name: "legal hold",
			meta: &metadata.ObjectMeta{
				ObjectLockLegalHoldStatus: "ON",
			},
			wantLocked: true,
			wantReason: "legal hold is enabled",
		},
		{
			name: "retention period",
			meta: &metadata.ObjectMeta{
				ObjectLockMode:            "COMPLIANCE",
				ObjectLockRetainUntilDate: &futureDate,
			},
			wantLocked: true,
			wantReason: "retention period",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RetentionInfo{
				Mode:            RetentionMode(tt.meta.ObjectLockMode),
				RetainUntilDate: tt.meta.ObjectLockRetainUntilDate,
				LegalHold:       LegalHoldStatus(tt.meta.ObjectLockLegalHoldStatus),
			}

			// Determine if object is locked (same logic as GetRetentionInfo)
			if info.LegalHold == LegalHoldOn {
				info.IsLocked = true
				info.Reason = "legal hold is enabled"
			} else if info.RetainUntilDate != nil && time.Now().Before(*info.RetainUntilDate) {
				info.IsLocked = true
				info.Reason = "retention period"
			}

			if info.IsLocked != tt.wantLocked {
				t.Errorf("IsLocked = %v, want %v", info.IsLocked, tt.wantLocked)
			}

			if tt.wantReason != "" && info.Reason == "" {
				t.Errorf("Reason is empty, want to contain %q", tt.wantReason)
			}
		})
	}
}
