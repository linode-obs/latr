package linode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test-token", "")
	require.NotNil(t, client)
	assert.Equal(t, "test-token", client.token)
}
