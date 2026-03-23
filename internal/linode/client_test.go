package linode

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-token", "")
	require.NotNil(t, client)
	assert.Equal(t, "test-token", client.token)
}

func TestCreateToken(t *testing.T) {
	// This test will use a mock server to avoid real API calls
	// For now, we'll write a test that verifies the method signature and structure
	client := NewClient("test-token", "")
	require.NotNil(t, client)

	ctx := context.Background()
	expiry := time.Now().Add(90 * 24 * time.Hour)

	// Note: This will be tested with integration tests or mocks
	// For unit tests, we'll verify the client can be created
	_ = ctx
	_ = expiry
}

func TestParseTokenScopes(t *testing.T) {
	tests := []struct {
		name     string
		scopes   string
		expected string
	}{
		{
			name:     "wildcard scopes",
			scopes:   "*",
			expected: "*",
		},
		{
			name:     "specific scopes",
			scopes:   "linodes:read_only,domains:read_only",
			expected: "linodes:read_only,domains:read_only",
		},
		{
			name:     "single scope",
			scopes:   "linodes:read_write",
			expected: "linodes:read_write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseScopes(tt.scopes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
