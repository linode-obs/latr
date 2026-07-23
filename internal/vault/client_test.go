package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/linode-obs/latr/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	err = client.WriteToken(ctx, "test/path", "my-secret-token", "", "")
	require.NoError(t, err)

	assert.Equal(t, 1, writeCount)
	assert.NotNil(t, lastWrittenData)

	// Verify the data structure (empty key defaults to "token"; empty action = replace)
	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "my-secret-token", data["token"])
	assert.Len(t, data, 1)
}

func TestWriteToken_CustomKey(t *testing.T) {
	var lastWrittenData map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenData = payload
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 1},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "custom-value", "linode-token", WriteActionReplace)
	require.NoError(t, err)

	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "custom-value", data["linode-token"])
	assert.Nil(t, data["token"])
}

func TestWriteToken_AppendPreservesOtherKeys(t *testing.T) {
	var lastWrittenData map[string]interface{}
	readCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			readCount++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"other": "keep-me",
						"token": "old-token",
					},
					"metadata": map[string]interface{}{"version": 3},
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenData = payload
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 4},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "new-token", "token", WriteActionAppend)
	require.NoError(t, err)

	assert.Equal(t, 1, readCount)
	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "new-token", data["token"])
	assert.Equal(t, "keep-me", data["other"])
	// CAS must pin the version observed on read
	opts, ok := lastWrittenData["options"].(map[string]interface{})
	require.True(t, ok, "append write should include options.cas")
	assert.EqualValues(t, 3, opts["cas"])
}

func TestWriteToken_ReplaceDropsOtherKeys(t *testing.T) {
	var lastWrittenData map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		// replace must NOT read first
		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			t.Error("replace action should not read existing secret")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenData = payload
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 1},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "only", "token", WriteActionReplace)
	require.NoError(t, err)

	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "only", data["token"])
	assert.Len(t, data, 1)
	assert.Nil(t, lastWrittenData["options"], "replace should not set CAS options")
}

func TestWriteToken_AppendCASConflictRetries(t *testing.T) {
	writeAttempts := 0
	version := 1

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"other": "keep",
						"token": "old",
					},
					"metadata": map[string]interface{}{"version": version},
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			writeAttempts++
			if writeAttempts == 1 {
				// Simulate concurrent writer winning first CAS
				version = 2
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprint(w, `{"errors":["check-and-set parameter did not match the current version"]}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": version + 1},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "new", "token", "APPEND")
	require.NoError(t, err)
	assert.Equal(t, 2, writeAttempts)
}

func TestWriteToken_ActionCaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}
		if r.URL.Path == "/v1/secret/data/p" && (r.Method == "POST" || r.Method == "PUT") {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
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

	err = client.WriteToken(context.Background(), "p", "v", "token", " Replace ")
	require.NoError(t, err)
}

func TestWriteToken_AppendWhenMissingCreatesKey(t *testing.T) {
	var lastWrittenData map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		// No secret yet
		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": nil,
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && (r.Method == "POST" || r.Method == "PUT") {
			var payload map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			lastWrittenData = payload
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"version": 1},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "only-token", "token", WriteActionAppend)
	require.NoError(t, err)

	data, ok := lastWrittenData["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "only-token", data["token"])
	assert.Len(t, data, 1)
	opts, ok := lastWrittenData["options"].(map[string]interface{})
	require.True(t, ok)
	assert.EqualValues(t, 0, opts["cas"], "create should use cas=0")
}

func TestWriteToken_InvalidAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	err = client.WriteToken(context.Background(), "test/path", "v", "token", "merge")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid write action")
}

func TestReadToken(t *testing.T) {
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
						"token": "retrieved-secret-token",
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
	token, err := client.ReadToken(ctx, "test/path", "")
	require.NoError(t, err)
	assert.Equal(t, "retrieved-secret-token", token)
}

func TestReadToken_CustomKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"auth": map[string]interface{}{
					"client_token":   "test-token",
					"lease_duration": 3600,
				},
			})
			return
		}

		if r.URL.Path == "/v1/secret/data/test/path" && r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"data": map[string]interface{}{
						"api_token": "custom-key-token",
					},
					"metadata": map[string]interface{}{"version": 1},
				},
			})
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client, err := NewClient(&Config{
		Address:   server.URL,
		RoleID:    "test-role-id",
		SecretID:  "test-secret-id",
		MountPath: "secret",
	})
	require.NoError(t, err)

	token, err := client.ReadToken(context.Background(), "test/path", "api_token")
	require.NoError(t, err)
	assert.Equal(t, "custom-key-token", token)
}

func TestReadToken_NotFound(t *testing.T) {
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
	token, err := client.ReadToken(ctx, "nonexistent/path", "")
	require.Error(t, err)
	assert.Empty(t, token)
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
