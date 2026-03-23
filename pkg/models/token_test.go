package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewToken(t *testing.T) {
	now := time.Now()
	expiry := now.Add(90 * 24 * time.Hour)

	token := &Token{
		ID:        123,
		Label:     "test-token",
		Token:     "test-secret-token",
		CreatedAt: now,
		ExpiresAt: expiry,
		Scopes:    "*",
		Team:      "platform-team",
		Validity:  90 * 24 * time.Hour,
	}

	assert.Equal(t, 123, token.ID)
	assert.Equal(t, "test-token", token.Label)
	assert.Equal(t, "test-secret-token", token.Token)
	assert.Equal(t, "*", token.Scopes)
	assert.Equal(t, "platform-team", token.Team)
}

func TestTokenNeedsRotation(t *testing.T) {
	tests := []struct {
		name             string
		expiresAt        time.Time
		validity         time.Duration
		thresholdPercent int
		expectedNeedsRot bool
	}{
		{
			name:             "needs rotation - at 10% threshold",
			expiresAt:        time.Now().Add(9 * 24 * time.Hour), // 9 days left
			validity:         90 * 24 * time.Hour,                // 90 day validity
			thresholdPercent: 10,                                 // 10% = 9 days
			expectedNeedsRot: true,
		},
		{
			name:             "needs rotation - below 10% threshold",
			expiresAt:        time.Now().Add(5 * 24 * time.Hour), // 5 days left
			validity:         90 * 24 * time.Hour,                // 90 day validity
			thresholdPercent: 10,
			expectedNeedsRot: true,
		},
		{
			name:             "does not need rotation - above 10% threshold",
			expiresAt:        time.Now().Add(15 * 24 * time.Hour), // 15 days left
			validity:         90 * 24 * time.Hour,                 // 90 day validity
			thresholdPercent: 10,
			expectedNeedsRot: false,
		},
		{
			name:             "needs rotation - custom 20% threshold",
			expiresAt:        time.Now().Add(18 * 24 * time.Hour), // 18 days left
			validity:         90 * 24 * time.Hour,                 // 90 day validity
			thresholdPercent: 20,                                  // 20% = 18 days
			expectedNeedsRot: true,
		},
		{
			name:             "already expired",
			expiresAt:        time.Now().Add(-1 * time.Hour), // Already expired
			validity:         90 * 24 * time.Hour,
			thresholdPercent: 10,
			expectedNeedsRot: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &Token{
				Label:     "test-token",
				ExpiresAt: tt.expiresAt,
				Validity:  tt.validity,
			}

			needsRotation := token.NeedsRotation(tt.thresholdPercent)
			assert.Equal(t, tt.expectedNeedsRot, needsRotation)
		})
	}
}

func TestTokenIsExpired(t *testing.T) {
	tests := []struct {
		name            string
		expiresAt       time.Time
		expectedExpired bool
	}{
		{
			name:            "not expired - future date",
			expiresAt:       time.Now().Add(10 * 24 * time.Hour),
			expectedExpired: false,
		},
		{
			name:            "expired - past date",
			expiresAt:       time.Now().Add(-1 * time.Hour),
			expectedExpired: true,
		},
		{
			name:            "expired - just now",
			expiresAt:       time.Now().Add(-1 * time.Second),
			expectedExpired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &Token{
				Label:     "test-token",
				ExpiresAt: tt.expiresAt,
			}

			isExpired := token.IsExpired()
			assert.Equal(t, tt.expectedExpired, isExpired)
		})
	}
}

func TestTokenTimeUntilExpiry(t *testing.T) {
	future := time.Now().Add(48 * time.Hour)
	token := &Token{
		Label:     "test-token",
		ExpiresAt: future,
	}

	timeUntil := token.TimeUntilExpiry()

	// Allow for small time differences in test execution
	assert.InDelta(t, (48 * time.Hour).Seconds(), timeUntil.Seconds(), 1.0)
}

func TestTokenPercentValidityRemaining(t *testing.T) {
	tests := []struct {
		name            string
		createdAt       time.Time
		expiresAt       time.Time
		validity        time.Duration
		expectedPercent float64
	}{
		{
			name:            "50% remaining",
			createdAt:       time.Now().Add(-45 * 24 * time.Hour), // Created 45 days ago
			expiresAt:       time.Now().Add(45 * 24 * time.Hour),  // Expires in 45 days
			validity:        90 * 24 * time.Hour,                  // 90 day validity
			expectedPercent: 50.0,
		},
		{
			name:            "10% remaining",
			createdAt:       time.Now().Add(-81 * 24 * time.Hour), // Created 81 days ago
			expiresAt:       time.Now().Add(9 * 24 * time.Hour),   // Expires in 9 days
			validity:        90 * 24 * time.Hour,                  // 90 day validity
			expectedPercent: 10.0,
		},
		{
			name:            "0% remaining (expired)",
			createdAt:       time.Now().Add(-90 * 24 * time.Hour), // Created 90 days ago
			expiresAt:       time.Now().Add(-1 * time.Hour),       // Expired 1 hour ago
			validity:        90 * 24 * time.Hour,
			expectedPercent: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := &Token{
				Label:     "test-token",
				CreatedAt: tt.createdAt,
				ExpiresAt: tt.expiresAt,
				Validity:  tt.validity,
			}

			percent := token.PercentValidityRemaining()
			assert.InDelta(t, tt.expectedPercent, percent, 1.0) // Allow 1% delta for time precision
		})
	}
}

func TestTokenState(t *testing.T) {
	state := &TokenState{
		Label:             "test-token",
		CurrentLinodeID:   123,
		CurrentTokenValue: "current-secret",
		LastRotatedAt:     time.Now().Add(-30 * 24 * time.Hour),
		PreviousLinodeID:  100,
		PreviousExpiresAt: time.Now().Add(60 * 24 * time.Hour),
		RotationCount:     5,
	}

	assert.Equal(t, "test-token", state.Label)
	assert.Equal(t, 123, state.CurrentLinodeID)
	assert.Equal(t, "current-secret", state.CurrentTokenValue)
	assert.Equal(t, 5, state.RotationCount)
}

func TestTokenStateAfterRotation(t *testing.T) {
	now := time.Now()
	oldExpiry := now.Add(10 * 24 * time.Hour)

	// Initial state
	state := &TokenState{
		Label:             "test-token",
		CurrentLinodeID:   100,
		CurrentTokenValue: "old-secret",
		LastRotatedAt:     now.Add(-80 * 24 * time.Hour),
		PreviousLinodeID:  0,
		PreviousExpiresAt: time.Time{},
		RotationCount:     0,
	}

	// After rotation
	newToken := &Token{
		ID:        200,
		Label:     "test-token",
		Token:     "new-secret",
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}

	// Update state
	newState := &TokenState{
		Label:             state.Label,
		CurrentLinodeID:   newToken.ID,
		CurrentTokenValue: newToken.Token,
		LastRotatedAt:     now,
		PreviousLinodeID:  state.CurrentLinodeID, // Old becomes previous
		PreviousExpiresAt: oldExpiry,             // Old expiry
		RotationCount:     state.RotationCount + 1,
	}

	assert.Equal(t, 200, newState.CurrentLinodeID)
	assert.Equal(t, "new-secret", newState.CurrentTokenValue)
	assert.Equal(t, 100, newState.PreviousLinodeID)
	assert.Equal(t, 1, newState.RotationCount)
	require.NotNil(t, newState.LastRotatedAt)
}
