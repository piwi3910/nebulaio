package object

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/piwi3910/nebulaio/pkg/s3errors"
)

// RetentionMode represents the object lock retention mode.
type RetentionMode string

const (
	// RetentionModeGovernance allows users with special permissions to bypass lock.
	RetentionModeGovernance RetentionMode = "GOVERNANCE"
	// RetentionModeCompliance provides strict lock that cannot be bypassed.
	RetentionModeCompliance RetentionMode = "COMPLIANCE"
)

// LegalHoldStatus represents the legal hold status.
type LegalHoldStatus string

const (
	// LegalHoldOn enables legal hold.
	LegalHoldOn LegalHoldStatus = "ON"
	// LegalHoldOff disables legal hold.
	LegalHoldOff LegalHoldStatus = "OFF"
)

// ObjectLockCheckOptions contains options for checking object lock.
type ObjectLockCheckOptions struct {
	// BypassGovernanceRetention allows bypassing governance mode retention
	// Requires s3:BypassGovernanceRetention permission
	BypassGovernanceRetention bool
}

// CheckObjectLock verifies if an object can be modified or deleted based on its lock status
// Returns an error if the object is locked and cannot be modified.
func (s *Service) CheckObjectLock(ctx context.Context, meta *metadata.ObjectMeta, opts *ObjectLockCheckOptions) error {
	if meta == nil {
		return nil
	}

	// Check legal hold first - it takes precedence
	if meta.ObjectLockLegalHoldStatus == string(LegalHoldOn) {
		return s3errors.ErrObjectLocked
	}

	// Check retention period
	if meta.ObjectLockRetainUntilDate != nil && !meta.ObjectLockRetainUntilDate.IsZero() {
		if time.Now().Before(*meta.ObjectLockRetainUntilDate) {
			// Object is within retention period
			mode := RetentionMode(meta.ObjectLockMode)

			switch mode {
			case RetentionModeCompliance:
				// Compliance mode cannot be bypassed
				return s3errors.ErrObjectLocked

			case RetentionModeGovernance:
				// Governance mode can be bypassed with proper permissions
				if opts != nil && opts.BypassGovernanceRetention {
					return nil
				}

				return s3errors.ErrObjectLocked
			}
		}
	}

	return nil
}

// CheckBucketObjectLockEnabled verifies if object lock is enabled for a bucket.
func (s *Service) CheckBucketObjectLockEnabled(ctx context.Context, bucket string) (bool, error) {
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return false, err
	}

	return bucketInfo.ObjectLockEnabled, nil
}

// ValidateRetentionMode validates the retention mode string.
func ValidateRetentionMode(mode string) error {
	switch RetentionMode(mode) {
	case RetentionModeGovernance, RetentionModeCompliance:
		return nil
	case "":
		return nil // Empty is allowed (no retention)
	default:
		return fmt.Errorf("invalid retention mode: %s", mode)
	}
}

// ValidateLegalHoldStatus validates the legal hold status string.
func ValidateLegalHoldStatus(status string) error {
	switch LegalHoldStatus(status) {
	case LegalHoldOn, LegalHoldOff:
		return nil
	case "":
		return nil // Empty is allowed
	default:
		return fmt.Errorf("invalid legal hold status: %s", status)
	}
}

// ValidateRetentionDate validates that the retention date is in the future.
func ValidateRetentionDate(date time.Time) error {
	if date.IsZero() {
		return nil
	}

	if date.Before(time.Now()) {
		return errors.New("retention date must be in the future")
	}

	return nil
}

// CanExtendRetention checks if retention can be extended (only longer periods allowed).
func CanExtendRetention(current, newRetention *time.Time) bool {
	if current == nil || current.IsZero() {
		return true // No current retention, any new retention is valid
	}

	if newRetention == nil || newRetention.IsZero() {
		return false // Cannot remove existing retention
	}

	return newRetention.After(*current) // New date must be after current date
}

// ApplyDefaultRetention applies bucket default retention to an object if not already set.
func (s *Service) ApplyDefaultRetention(ctx context.Context, bucket string, meta *metadata.ObjectMeta) error {
	// If object already has retention, don't override
	if meta.ObjectLockMode != "" || meta.ObjectLockRetainUntilDate != nil {
		return nil
	}

	// Get bucket object lock configuration
	bucketInfo, err := s.bucketService.GetBucket(ctx, bucket)
	if err != nil {
		return err
	}

	// Check if bucket has default retention configured
	if bucketInfo.ObjectLockConfig != nil && bucketInfo.ObjectLockConfig.DefaultRetention != nil {
		defaultRetention := bucketInfo.ObjectLockConfig.DefaultRetention

		// Set mode from default
		meta.ObjectLockMode = defaultRetention.Mode

		// Calculate retain until date based on default
		now := time.Now()
		if defaultRetention.Days > 0 {
			retainUntil := now.AddDate(0, 0, defaultRetention.Days)
			meta.ObjectLockRetainUntilDate = &retainUntil
		} else if defaultRetention.Years > 0 {
			retainUntil := now.AddDate(defaultRetention.Years, 0, 0)
			meta.ObjectLockRetainUntilDate = &retainUntil
		}
	}

	return nil
}

// RetentionInfo contains the retention information for an object.
type RetentionInfo struct {
	RetainUntilDate *time.Time
	Mode            RetentionMode
	LegalHold       LegalHoldStatus
	Reason          string
	IsLocked        bool
}

// GetRetentionInfo retrieves retention information for an object.
func (s *Service) GetRetentionInfo(ctx context.Context, bucket, key, versionID string) (*RetentionInfo, error) {
	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = s.store.GetObjectVersion(ctx, bucket, key, versionID)
	} else {
		meta, err = s.store.GetObjectMeta(ctx, bucket, key)
	}

	if err != nil {
		return nil, err
	}

	info := &RetentionInfo{
		Mode:            RetentionMode(meta.ObjectLockMode),
		RetainUntilDate: meta.ObjectLockRetainUntilDate,
		LegalHold:       LegalHoldStatus(meta.ObjectLockLegalHoldStatus),
	}

	// Determine if object is locked
	if info.LegalHold == LegalHoldOn {
		info.IsLocked = true
		info.Reason = "legal hold is enabled"
	} else if info.RetainUntilDate != nil && time.Now().Before(*info.RetainUntilDate) {
		info.IsLocked = true
		info.Reason = "retention period until " + info.RetainUntilDate.Format(time.RFC3339)
	}

	return info, nil
}

// ExtendRetention extends the retention period for an object (only to a later date).
func (s *Service) ExtendRetention(ctx context.Context, bucket, key, versionID string, mode string, retainUntilDate time.Time) error {
	var (
		meta *metadata.ObjectMeta
		err  error
	)

	if versionID != "" {
		meta, err = s.store.GetObjectVersion(ctx, bucket, key, versionID)
	} else {
		meta, err = s.store.GetObjectMeta(ctx, bucket, key)
	}

	if err != nil {
		return err
	}

	// Validate mode
	validationErr := ValidateRetentionMode(mode)
	if validationErr != nil {
		return validationErr
	}

	// Check if extension is valid
	if !CanExtendRetention(meta.ObjectLockRetainUntilDate, &retainUntilDate) {
		return errors.New("retention can only be extended, not reduced")
	}

	// For compliance mode, mode cannot be changed once set
	if meta.ObjectLockMode == string(RetentionModeCompliance) && mode != "" && mode != string(RetentionModeCompliance) {
		return errors.New("compliance mode cannot be changed")
	}

	// Update retention
	if mode != "" {
		meta.ObjectLockMode = mode
	}

	meta.ObjectLockRetainUntilDate = &retainUntilDate

	// Use PutObjectMeta or PutObjectMetaVersioned based on versioning
	if meta.VersionID != "" {
		return s.store.PutObjectMetaVersioned(ctx, meta, false)
	}

	return s.store.PutObjectMeta(ctx, meta)
}
