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

func TestWriteToken_PATCHSuccess(t *testing.T) {
	var lastMethod string
	var lastWrittenData map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" {
			lastMethod = r.Method
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenData = payload
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 1},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address: server.URL, RoleID: "r", SecretID: "s", MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "", "my-secret-token")
	require.NoError(t, err)

	assert.Equal(t, "PATCH", lastMethod, "WriteToken should use PATCH when it succeeds")
	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "my-secret-token", data["token"])
}

func TestWriteToken_FallbackOn404(t *testing.T) {
	var methods []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" {
			methods = append(methods, r.Method)
			if r.Method == "PATCH" {
				w.WriteHeader(http.StatusNotFound)
				json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"no data at path"}})
				return
			}
			// POST/PUT fallback succeeds
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 1},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address: server.URL, RoleID: "r", SecretID: "s", MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "custom-key", "my-token")
	require.NoError(t, err)

	require.Len(t, methods, 2)
	assert.Equal(t, "PATCH", methods[0], "should try PATCH first")
	assert.Contains(t, []string{"PUT", "POST"}, methods[1], "should fall back to full write on 404 using PUT or POST")
}

func TestWriteToken_FallbackOn405_PreservesSiblingKeys(t *testing.T) {
	var methods []string
	var writtenPayload map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" {
			methods = append(methods, r.Method)
			if r.Method == "PATCH" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"method not allowed"}})
				return
			}
			if r.Method == "GET" {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"data":     map[string]interface{}{"sibling-key": "preserve-me", "old-key": "also-keep"},
						"metadata": map[string]interface{}{"version": 1},
					},
				})
				return
			}
			// PUT/POST write
			json.NewDecoder(r.Body).Decode(&writtenPayload)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 2},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address: server.URL, RoleID: "r", SecretID: "s", MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "custom-key", "new-token-value")
	require.NoError(t, err)

	require.Len(t, methods, 3, "should PATCH, GET, then PUT/POST")
	assert.Equal(t, "PATCH", methods[0])
	assert.Equal(t, "GET", methods[1])
	assert.Contains(t, []string{"PUT", "POST"}, methods[2])

	// Verify the written data contains both the new key and existing sibling keys
	require.NotNil(t, writtenPayload)
	dataMap, ok := writtenPayload["data"].(map[string]interface{})
	require.True(t, ok, "payload should have data key")
	assert.Equal(t, "new-token-value", dataMap["custom-key"], "should contain the new token")
	assert.Equal(t, "preserve-me", dataMap["sibling-key"], "should preserve existing sibling key")
	assert.Equal(t, "also-keep", dataMap["old-key"], "should preserve all existing keys")
}

func TestWriteToken_FallbackOn403(t *testing.T) {
	var methods []string
	existingData := map[string]interface{}{
		"data": map[string]interface{}{
			"data":     map[string]interface{}{"sibling-key": "preserve-me"},
			"metadata": map[string]interface{}{"version": 1},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" {
			methods = append(methods, r.Method)
			if r.Method == "PATCH" {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{"errors": []string{"permission denied"}})
				return
			}
			if r.Method == "GET" {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(existingData)
				return
			}
			// PUT/POST write succeeds
			var payload map[string]interface{}
			json.NewDecoder(r.Body).Decode(&payload)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 2},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address: server.URL, RoleID: "r", SecretID: "s", MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "custom-key", "my-token")
	require.NoError(t, err)

	require.Len(t, methods, 3, "should PATCH, then GET (read existing), then PUT/POST (write merged)")
	assert.Equal(t, "PATCH", methods[0], "should try PATCH first")
	assert.Equal(t, "GET", methods[1], "should read existing data for merge")
	assert.Contains(t, []string{"PUT", "POST"}, methods[2], "should write merged data")
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
