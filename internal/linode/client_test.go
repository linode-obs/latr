package linode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-token")
	require.NotNil(t, client)
	require.NotNil(t, client.tokenProvider)
	require.NotNil(t, client.client)
}

func TestNewClientWithTokenProvider(t *testing.T) {
	p := StaticTokenProvider("from-provider")
	client := NewClientWithTokenProvider(p)
	require.NotNil(t, client)
	assert.NotNil(t, client.tokenProvider)
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
