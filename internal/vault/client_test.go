package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wbh1/latr/pkg/models"
)

func TestNewClient_AppRoleAuth(t *testing.T) {
	// Create a mock Vault server
	loginCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			loginCalled = true
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.True(t, loginCalled)
}

func TestNewClient_AuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "permission denied")
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "invalid-role-id",
		SecretID:  "invalid-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to authenticate")
}

func TestWriteToken(t *testing.T) {
	writeCount := 0
	var lastWrittenData map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			writeCount++
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenData = payload
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"version": 1,
				},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.WriteToken(ctx, "test/path", "", "my-secret-token")
	require.NoError(t, err)

	assert.Equal(t, 1, writeCount)
	assert.NotNil(t, lastWrittenData)

	// Verify the data structure
	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "my-secret-token", data["token"])
}

func TestReadSecretKey_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"my-key": "my-secret-value",
					},
					"metadata": map[string]interface{}{
						"version": 1,
					},
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)

	ctx := context.Background()
	value, err := client.ReadSecretKey(ctx, "test/path", "my-key")
	require.NoError(t, err)
	assert.Equal(t, "my-secret-value", value)
}

func TestReadSecretKey_MissingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"other-key": "other-value",
					},
					"metadata": map[string]interface{}{
						"version": 1,
					},
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)

	ctx := context.Background()
	value, err := client.ReadSecretKey(ctx, "test/path", "nonexistent-key")
	require.Error(t, err)
	assert.Empty(t, value)
	assert.Contains(t, err.Error(), "nonexistent-key")
}

func TestWriteTokenState(t *testing.T) {
	var lastWrittenMetadata map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path == "/v1/secret/metadata/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenMetadata = payload
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)

	state := &models.TokenState{
		Label:             "test-token",
		CurrentLinodeID:   123,
		CurrentTokenValue: "secret-value",
		LastRotatedAt:     time.Now(),
		PreviousLinodeID:  100,
		PreviousExpiresAt: time.Now().Add(60 * 24 * time.Hour),
		RotationCount:     5,
	}

	ctx := context.Background()
	err = client.WriteTokenState(ctx, "test/path", state)
	require.NoError(t, err)

	assert.NotNil(t, lastWrittenMetadata)
	customMeta, ok := lastWrittenMetadata["custom_metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-token", customMeta["label"])
	assert.Equal(t, "123", customMeta["current_linode_id"])
	assert.Equal(t, "5", customMeta["rotation_count"])
}

func TestReadTokenState(t *testing.T) {
	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		if r.URL.Path == "/v1/secret/metadata/test/path" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"data": map[string]interface{}{
					"custom_metadata": map[string]interface{}{
						"label":               "test-token",
						"current_linode_id":   "123",
						"last_rotated_at":     now.Format(time.RFC3339),
						"previous_linode_id":  "100",
						"previous_expires_at": now.Add(60 * 24 * time.Hour).Format(time.RFC3339),
						"rotation_count":      "5",
					},
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)

	ctx := context.Background()
	state, err := client.ReadTokenState(ctx, "test/path")
	require.NoError(t, err)
	require.NotNil(t, state)

	assert.Equal(t, "test-token", state.Label)
	assert.Equal(t, 123, state.CurrentLinodeID)
	assert.Equal(t, 100, state.PreviousLinodeID)
	assert.Equal(t, 5, state.RotationCount)
}

func TestReadTokenState_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			response := map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			}
			json.NewEncoder(w).Encode(response)
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	config := &Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	}

	client, err := NewClient(config)
	require.NoError(t, err)

	ctx := context.Background()
	state, err := client.ReadTokenState(ctx, "nonexistent/path")
	// Not found should return nil state without error (it's a new token)
	require.NoError(t, err)
	assert.Nil(t, state)
}
