package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const testValidPassword = "ValidPassword123"

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		errorType   error
		name        string
		password    string
		expectError bool
	}{
		{
			name:        "empty password",
			password:    "",
			expectError: true,
			errorType:   ErrPasswordEmpty,
		},
		{
			name:        "too short - 1 char",
			password:    "A",
			expectError: true,
			errorType:   ErrPasswordTooShort,
		},
		{
			name:        "too short - 11 chars with all requirements",
			password:    "Short1Aaaaa",
			expectError: true,
			errorType:   ErrPasswordTooShort,
		},
		{
			name:        "no uppercase",
			password:    "lowercaseonly123",
			expectError: true,
			errorType:   ErrPasswordNoUpper,
		},
		{
			name:        "no lowercase",
			password:    "UPPERCASEONLY123",
			expectError: true,
			errorType:   ErrPasswordNoLower,
		},
		{
			name:        "no number",
			password:    "NoNumbersHereABC",
			expectError: true,
			errorType:   ErrPasswordNoNumber,
		},
		{
			name:        "valid password - exactly 12 chars",
			password:    "Exactly12Aa1",
			expectError: false,
		},
		{
			name:        "valid password - standard",
			password:    "ValidPassword123",
			expectError: false,
		},
		{
			name:        "valid password - with special chars",
			password:    "Valid@Pass#123!",
			expectError: false,
		},
		{
			name:        "valid password - long password",
			password:    "ThisIsAVeryLongPassword123WithManyCharacters",
			expectError: false,
		},
		{
			name:        "valid password - unicode letters",
			password:    "ValidPasswörd123",
			expectError: false,
		},
		{
			name:        "missing upper - all numbers and lower",
			password:    "alllowercase123456",
			expectError: true,
			errorType:   ErrPasswordNoUpper,
		},
		{
			name:        "missing lower - all numbers and upper",
			password:    "ALLUPPERCASE123456",
			expectError: true,
			errorType:   ErrPasswordNoLower,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordStrength(tt.password)
			if tt.expectError {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.errorType)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{
			name:     "standard password",
			password: "ValidPassword123",
		},
		{
			name:     "password with special chars",
			password: "P@ssw0rd!@#$%^&*()",
		},
		{
			name:     "long password",
			password: "ThisIsAVeryLongPasswordThatShouldStillWork123",
		},
		{
			name:     "unicode password",
			password: "Pässwörd123日本語",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)
			require.NoError(t, err)
			assert.NotEmpty(t, hash)
			assert.NotEqual(t, tt.password, hash, "hash should not equal password")

			// Verify the hash works
			err = VerifyPassword(hash, tt.password)
			require.NoError(t, err)
		})
	}
}

func TestHashPasswordUniqueness(t *testing.T) {
	password := "SamePassword123"

	hash1, err := HashPassword(password)
	require.NoError(t, err)

	hash2, err := HashPassword(password)
	require.NoError(t, err)

	// Same password should produce different hashes (due to salt)
	assert.NotEqual(t, hash1, hash2, "hashes should differ due to salting")

	// Both hashes should verify correctly
	require.NoError(t, VerifyPassword(hash1, password))
	require.NoError(t, VerifyPassword(hash2, password))
}

func TestVerifyPassword(t *testing.T) {
	correctPassword := "CorrectPassword123"
	hash, err := HashPassword(correctPassword)
	require.NoError(t, err)

	tests := []struct {
		errorType   error
		name        string
		hash        string
		password    string
		expectError bool
	}{
		{
			name:        "correct password",
			hash:        hash,
			password:    correctPassword,
			expectError: false,
		},
		{
			name:        "wrong password",
			hash:        hash,
			password:    "WrongPassword123",
			expectError: true,
			errorType:   ErrPasswordMismatch,
		},
		{
			name:        "empty hash",
			hash:        "",
			password:    correctPassword,
			expectError: true,
			errorType:   ErrInvalidHash,
		},
		{
			name:        "case sensitive - wrong case",
			hash:        hash,
			password:    "correctpassword123",
			expectError: true,
			errorType:   ErrPasswordMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifyPassword(tt.hash, tt.password)
			if tt.expectError {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.errorType)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		errorType   error
		name        string
		username    string
		expectError bool
	}{
		{
			name:        "too short - 2 chars",
			username:    "ab",
			expectError: true,
			errorType:   ErrUsernameTooShort,
		},
		{
			name:        "too short - 1 char",
			username:    "a",
			expectError: true,
			errorType:   ErrUsernameTooShort,
		},
		{
			name:        "too short - empty",
			username:    "",
			expectError: true,
			errorType:   ErrUsernameTooShort,
		},
		{
			name:        "too long - 33 chars",
			username:    "abcdefghijklmnopqrstuvwxyz1234567",
			expectError: true,
			errorType:   ErrUsernameTooLong,
		},
		{
			name:        "invalid - contains space",
			username:    "user name",
			expectError: true,
			errorType:   ErrUsernameInvalid,
		},
		{
			name:        "invalid - contains special char",
			username:    "user@name",
			expectError: true,
			errorType:   ErrUsernameInvalid,
		},
		{
			name:        "valid - minimum length",
			username:    "abc",
			expectError: false,
		},
		{
			name:        "valid - maximum length",
			username:    "abcdefghijklmnopqrstuvwxyz123456",
			expectError: false,
		},
		{
			name:        "valid - with underscore",
			username:    "user_name",
			expectError: false,
		},
		{
			name:        "valid - with hyphen",
			username:    "user-name",
			expectError: false,
		},
		{
			name:        "valid - alphanumeric",
			username:    "user123",
			expectError: false,
		},
		{
			name:        "valid - uppercase",
			username:    "UserName",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if tt.expectError {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.errorType)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		errorType   error
		name        string
		email       string
		expectError bool
	}{
		{
			name:        "empty email - allowed",
			email:       "",
			expectError: false,
		},
		{
			name:        "valid email - simple",
			email:       "user@example.com",
			expectError: false,
		},
		{
			name:        "valid email - with subdomain",
			email:       "user@mail.example.com",
			expectError: false,
		},
		{
			name:        "valid email - with plus",
			email:       "user+tag@example.com",
			expectError: false,
		},
		{
			name:        "valid email - with dots in local",
			email:       "user.name@example.com",
			expectError: false,
		},
		{
			name:        "invalid email - no at sign",
			email:       "userexample.com",
			expectError: true,
			errorType:   ErrEmailInvalid,
		},
		{
			name:        "invalid email - no domain",
			email:       "user@",
			expectError: true,
			errorType:   ErrEmailInvalid,
		},
		{
			name:        "invalid email - no local part",
			email:       "@example.com",
			expectError: true,
			errorType:   ErrEmailInvalid,
		},
		{
			name:        "invalid email - no TLD",
			email:       "user@example",
			expectError: true,
			errorType:   ErrEmailInvalid,
		},
		{
			name:        "invalid email - spaces",
			email:       "user @example.com",
			expectError: true,
			errorType:   ErrEmailInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if tt.expectError {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.errorType)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Benchmark tests.
func BenchmarkValidatePasswordStrength(b *testing.B) {
	password := testValidPassword
	for range b.N {
		_ = ValidatePasswordStrength(password)
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := testValidPassword
	for range b.N {
		_, _ = HashPassword(password)
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	password := testValidPassword
	hash, _ := HashPassword(password)

	b.ResetTimer()

	for range b.N {
		_ = VerifyPassword(hash, password)
	}
}
