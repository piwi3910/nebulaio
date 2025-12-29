package auth

import (
	"errors"
	"regexp"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// Password validation errors
var (
	ErrPasswordEmpty    = errors.New("password is required and cannot be empty")
	ErrPasswordTooShort = errors.New("password must be at least 12 characters long")
	ErrPasswordNoUpper  = errors.New("password must contain at least one uppercase letter")
	ErrPasswordNoLower  = errors.New("password must contain at least one lowercase letter")
	ErrPasswordNoNumber = errors.New("password must contain at least one number")
	ErrPasswordMismatch = errors.New("password verification failed")
	ErrInvalidHash      = errors.New("invalid password hash")
)

// Username validation errors
var (
	ErrUsernameTooShort = errors.New("username must be at least 3 characters long")
	ErrUsernameTooLong  = errors.New("username must be at most 32 characters long")
	ErrUsernameInvalid  = errors.New("username must contain only alphanumeric characters, underscores, and hyphens")
)

// Email validation errors
var (
	ErrEmailInvalid = errors.New("invalid email format")
)

// Regular expressions for validation
var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
)

// ValidatePasswordStrength validates password meets minimum security requirements.
// Requirements:
// - Minimum 12 characters
// - At least one uppercase letter
// - At least one lowercase letter
// - At least one number
func ValidatePasswordStrength(password string) error {
	if password == "" {
		return ErrPasswordEmpty
	}
	if len(password) < 12 {
		return ErrPasswordTooShort
	}

	var hasUpper, hasLower, hasNumber bool

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		}
	}

	if !hasUpper {
		return ErrPasswordNoUpper
	}
	if !hasLower {
		return ErrPasswordNoLower
	}
	if !hasNumber {
		return ErrPasswordNoNumber
	}

	return nil
}

// HashPassword generates a bcrypt hash of the password
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword compares a password with its hash
func VerifyPassword(hash, password string) error {
	if hash == "" {
		return ErrInvalidHash
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}
		return err
	}
	return nil
}

// ValidateUsername validates username format.
// Requirements:
// - Between 3 and 32 characters
// - Only alphanumeric characters, underscores, and hyphens
func ValidateUsername(username string) error {
	if len(username) < 3 {
		return ErrUsernameTooShort
	}
	if len(username) > 32 {
		return ErrUsernameTooLong
	}
	if !usernameRegex.MatchString(username) {
		return ErrUsernameInvalid
	}
	return nil
}

// ValidateEmail validates email format if provided.
// Empty email is allowed (optional field).
func ValidateEmail(email string) error {
	if email == "" {
		return nil // Email is optional
	}
	if !emailRegex.MatchString(email) {
		return ErrEmailInvalid
	}
	return nil
}
