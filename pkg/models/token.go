package models

import (
	"time"
)

// Token represents a Linode API token with its metadata
type Token struct {
	ID        int           // Linode token ID
	Label     string        // Token label/name
	Token     string        // The actual token value (secret)
	CreatedAt time.Time     // When the token was created
	ExpiresAt time.Time     // When the token expires
	Scopes    string        // Token scopes
	Team      string        // Owning team (metadata)
	Validity  time.Duration // How long the token is valid for
}

// TokenState represents the current state of a managed token
// This is stored in Vault metadata to track rotation history
type TokenState struct {
	Label             string    // Token label (matches config)
	CurrentLinodeID   int       // Current active token ID in Linode
	CurrentTokenValue string    // Current token value
	LastRotatedAt     time.Time // When the token was last rotated
	PreviousLinodeID  int       // Previous token ID (not yet deleted)
	PreviousExpiresAt time.Time // When the previous token expires
	RotationCount     int       // How many times the token has been rotated
}

// NeedsRotation determines if a token needs to be rotated based on the threshold percentage
// thresholdPercent is the percentage of validity remaining at which rotation should occur
func (t *Token) NeedsRotation(thresholdPercent int) bool {
	// If already expired, definitely needs rotation
	if t.IsExpired() {
		return true
	}

	percentRemaining := t.PercentValidityRemaining()
	return percentRemaining <= float64(thresholdPercent)
}

// IsExpired returns true if the token has already expired
func (t *Token) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// TimeUntilExpiry returns the duration until the token expires
func (t *Token) TimeUntilExpiry() time.Duration {
	return time.Until(t.ExpiresAt)
}

// PercentValidityRemaining calculates what percentage of the token's validity period remains
func (t *Token) PercentValidityRemaining() float64 {
	if t.IsExpired() {
		return 0.0
	}

	timeRemaining := t.TimeUntilExpiry()
	return (timeRemaining.Seconds() / t.Validity.Seconds()) * 100.0
}
