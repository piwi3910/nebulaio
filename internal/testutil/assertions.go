package testutil

import (
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertBucketEqual asserts that two buckets are equal (ignoring timestamps).
func AssertBucketEqual(t *testing.T, expected, actual *metadata.Bucket) {
	t.Helper()
	require.NotNil(t, expected, "expected bucket should not be nil")
	require.NotNil(t, actual, "actual bucket should not be nil")

	assert.Equal(t, expected.Name, actual.Name, "bucket names should match")
	assert.Equal(t, expected.Owner, actual.Owner, "bucket owners should match")
	assert.Equal(t, expected.Region, actual.Region, "bucket regions should match")
	assert.Equal(t, expected.ACL, actual.ACL, "bucket ACLs should match")
	assert.Equal(t, expected.Versioning, actual.Versioning, "bucket versioning should match")
}

// AssertObjectMetaEqual asserts that two object metadata are equal (ignoring timestamps).
func AssertObjectMetaEqual(t *testing.T, expected, actual *metadata.ObjectMeta) {
	t.Helper()
	require.NotNil(t, expected, "expected object meta should not be nil")
	require.NotNil(t, actual, "actual object meta should not be nil")

	assert.Equal(t, expected.Bucket, actual.Bucket, "object buckets should match")
	assert.Equal(t, expected.Key, actual.Key, "object keys should match")
	assert.Equal(t, expected.Size, actual.Size, "object sizes should match")
	assert.Equal(t, expected.ContentType, actual.ContentType, "object content types should match")
	assert.Equal(t, expected.ETag, actual.ETag, "object ETags should match")
	assert.Equal(t, expected.Owner, actual.Owner, "object owners should match")
	assert.Equal(t, expected.StorageClass, actual.StorageClass, "object storage classes should match")
}

// AssertUserEqual asserts that two users are equal (ignoring timestamps and password).
func AssertUserEqual(t *testing.T, expected, actual *metadata.User) {
	t.Helper()
	require.NotNil(t, expected, "expected user should not be nil")
	require.NotNil(t, actual, "actual user should not be nil")

	assert.Equal(t, expected.ID, actual.ID, "user IDs should match")
	assert.Equal(t, expected.Username, actual.Username, "usernames should match")
	assert.Equal(t, expected.Email, actual.Email, "user emails should match")
	assert.Equal(t, expected.Role, actual.Role, "user roles should match")
	assert.Equal(t, expected.Enabled, actual.Enabled, "user enabled status should match")
}

// AssertErrorType asserts that an error is of a specific type.
func AssertErrorType(t *testing.T, expected, actual error) {
	t.Helper()

	if expected == nil {
		assert.NoError(t, actual, "expected no error")
		return
	}

	require.Error(t, actual, "expected an error")
	assert.ErrorIs(t, actual, expected, "error type should match")
}

// AssertEventually waits for a condition to become true within a timeout.
// Useful for testing async operations.
func AssertEventually(t *testing.T, condition func() bool, timeout, tick time.Duration, msgAndArgs ...interface{}) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		if condition() {
			return
		}

		if time.Now().After(deadline) {
			assert.Fail(t, "condition not met within timeout", msgAndArgs...)
			return
		}

		time.Sleep(tick)
	}
}

// RequireEventually waits for a condition to become true within a timeout.
// Fails the test immediately if the condition is not met.
func RequireEventually(t *testing.T, condition func() bool, timeout, tick time.Duration, msgAndArgs ...interface{}) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		if condition() {
			return
		}

		if time.Now().After(deadline) {
			require.Fail(t, "condition not met within timeout", msgAndArgs...)
			return
		}

		time.Sleep(tick)
	}
}

// AssertNever asserts that a condition is never true within a duration.
// Useful for testing that something doesn't happen.
func AssertNever(t *testing.T, condition func() bool, duration, tick time.Duration, msgAndArgs ...interface{}) {
	t.Helper()

	deadline := time.Now().Add(duration)

	for {
		if condition() {
			assert.Fail(t, "condition became true unexpectedly", msgAndArgs...)
			return
		}

		if time.Now().After(deadline) {
			return // Success - condition never became true
		}

		time.Sleep(tick)
	}
}

// AssertSliceContains asserts that a slice contains all expected elements.
func AssertSliceContains[T comparable](t *testing.T, slice []T, expected ...T) {
	t.Helper()

	for _, e := range expected {
		found := false

		for _, s := range slice {
			if s == e {
				found = true
				break
			}
		}

		assert.True(t, found, "slice should contain %v", e)
	}
}

// AssertSliceNotContains asserts that a slice does not contain any of the excluded elements.
func AssertSliceNotContains[T comparable](t *testing.T, slice []T, excluded ...T) {
	t.Helper()

	for _, e := range excluded {
		for _, s := range slice {
			if s == e {
				assert.Fail(t, "slice should not contain element", "found: %v", e)
				return
			}
		}
	}
}

// AssertMapContainsKey asserts that a map contains a specific key.
func AssertMapContainsKey[K comparable, V any](t *testing.T, m map[K]V, key K) {
	t.Helper()

	_, exists := m[key]
	assert.True(t, exists, "map should contain key %v", key)
}

// AssertMapNotContainsKey asserts that a map does not contain a specific key.
func AssertMapNotContainsKey[K comparable, V any](t *testing.T, m map[K]V, key K) {
	t.Helper()

	_, exists := m[key]
	assert.False(t, exists, "map should not contain key %v", key)
}
