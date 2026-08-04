package linode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStaticTokenProvider(t *testing.T) {
	t.Parallel()

	p := StaticTokenProvider("abc123")
	tok, err := p(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "abc123", tok)

	empty := StaticTokenProvider("")
	_, err = empty(context.Background())
	require.Error(t, err)
}

func TestTokenFileProviderCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("  first-token  \n"), 0o600))

	now := time.Unix(1_700_000_000, 0)
	p := NewTokenFileProvider(path, time.Minute)
	p.now = func() time.Time { return now }

	tok, err := p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first-token", tok)

	// Change file; still within cache TTL → old value
	require.NoError(t, os.WriteFile(path, []byte("second-token"), 0o600))
	tok, err = p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first-token", tok)

	// Advance past TTL → re-read
	now = now.Add(time.Minute + time.Second)
	tok, err = p.GetToken(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "second-token", tok)
}

func TestTokenFileProviderEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("  \n"), 0o600))

	p := NewTokenFileProvider(path, time.Minute)
	_, err := p.GetToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestTokenFileProviderMissingFile(t *testing.T) {
	p := NewTokenFileProvider(filepath.Join(t.TempDir(), "missing"), time.Minute)
	_, err := p.GetToken(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read")
}

func TestTokenFileCacheTTLFromEnv(t *testing.T) {
	t.Setenv(TokenCacheTTLEnv, "")
	// empty/unset uses default — LookupEnv is false for empty set after clear
	_ = os.Unsetenv(TokenCacheTTLEnv)
	assert.Equal(t, DefaultTokenFileCacheTTL, TokenFileCacheTTLFromEnv())

	t.Setenv(TokenCacheTTLEnv, "30")
	assert.Equal(t, 30*time.Second, TokenFileCacheTTLFromEnv())

	t.Setenv(TokenCacheTTLEnv, "0")
	assert.Equal(t, DefaultTokenFileCacheTTL, TokenFileCacheTTLFromEnv())

	t.Setenv(TokenCacheTTLEnv, "nope")
	assert.Equal(t, DefaultTokenFileCacheTTL, TokenFileCacheTTLFromEnv())
}

func TestTokenProviderFromEnv(t *testing.T) {
	ctx := context.Background()

	t.Run("uses file when set and readable", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "token")
		require.NoError(t, os.WriteFile(path, []byte("file-token"), 0o600))

		t.Setenv(TokenFilePathEnv, path)
		t.Setenv(AccessTokenEnv, "env-token")

		p, src, err := TokenProviderFromEnv(ctx)
		require.NoError(t, err)
		assert.Contains(t, src, "file")
		tok, err := p(ctx)
		require.NoError(t, err)
		assert.Equal(t, "file-token", tok)
	})

	t.Run("falls back to env when file missing", func(t *testing.T) {
		t.Setenv(TokenFilePathEnv, filepath.Join(t.TempDir(), "gone"))
		t.Setenv(AccessTokenEnv, "env-token")

		p, src, err := TokenProviderFromEnv(ctx)
		require.NoError(t, err)
		assert.Contains(t, src, AccessTokenEnv)
		tok, err := p(ctx)
		require.NoError(t, err)
		assert.Equal(t, "env-token", tok)
	})

	t.Run("uses env when file unset", func(t *testing.T) {
		t.Setenv(TokenFilePathEnv, "")
		t.Setenv(AccessTokenEnv, "only-env")

		p, src, err := TokenProviderFromEnv(ctx)
		require.NoError(t, err)
		assert.Contains(t, src, AccessTokenEnv)
		tok, err := p(ctx)
		require.NoError(t, err)
		assert.Equal(t, "only-env", tok)
	})

	t.Run("errors when file set but unreadable and no env", func(t *testing.T) {
		t.Setenv(TokenFilePathEnv, filepath.Join(t.TempDir(), "gone"))
		t.Setenv(AccessTokenEnv, "")

		_, _, err := TokenProviderFromEnv(ctx)
		require.Error(t, err)
	})

	t.Run("errors when nothing set", func(t *testing.T) {
		t.Setenv(TokenFilePathEnv, "")
		t.Setenv(AccessTokenEnv, "")

		_, _, err := TokenProviderFromEnv(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), AccessTokenEnv)
	})
}

func TestTokenTransportAuthorization(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":[],"page":1,"pages":1,"results":0}`)
	}))
	t.Cleanup(server.Close)

	t.Setenv("LINODE_API_URL", server.URL+"/")
	client := NewClientWithTokenProvider(StaticTokenProvider("secret-pat"))

	_, err := client.FindTokenByLabel(context.Background(), "any")
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret-pat", gotAuth)
}

func TestTokenTransportUsesProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP request should not be sent when token provider fails")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	t.Setenv("LINODE_API_URL", server.URL+"/")
	client := NewClientWithTokenProvider(func(context.Context) (string, error) {
		return "", assert.AnError
	})

	_, err := client.FindTokenByLabel(context.Background(), "any")
	require.Error(t, err)
	// linodego/resty wrap the transport error; ensure the provider error is visible.
	assert.Contains(t, err.Error(), assert.AnError.Error())
}
