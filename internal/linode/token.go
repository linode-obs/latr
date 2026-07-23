package linode

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// AccessTokenEnv is the environment variable holding a static Linode API token.
	AccessTokenEnv = "LINODE_TOKEN"
	// TokenFilePathEnv points at a file containing the management PAT (e.g. K8s secret mount).
	// When set and readable, latr re-reads this file so the token can rotate without restart.
	TokenFilePathEnv = "LINODE_TOKEN_FILE"
	// TokenCacheTTLEnv optionally overrides the file token cache TTL in seconds.
	TokenCacheTTLEnv = "LINODE_TOKEN_CACHE_TTL_SECONDS"
	// DefaultTokenFileCacheTTL is how long a file-backed token is cached between re-reads.
	DefaultTokenFileCacheTTL = time.Minute
)

// TokenProvider returns a Linode API token for the current request.
// Implementations may re-read a mounted secret so tokens can rotate without a restart.
type TokenProvider func(context.Context) (string, error)

// StaticTokenProvider returns a TokenProvider that always yields the given token.
func StaticTokenProvider(token string) TokenProvider {
	return staticTokenProvider{token: token}.GetToken
}

type staticTokenProvider struct {
	token string
}

func (t staticTokenProvider) GetToken(context.Context) (string, error) {
	if t.token == "" {
		return "", fmt.Errorf("linode API token is empty; set %s or %s", AccessTokenEnv, TokenFilePathEnv)
	}
	return t.token, nil
}

// TokenFileProvider reads a token from a file with a short TTL cache so secret
// updates (e.g. projected K8s Secret) are picked up without restarting the process.
type TokenFileProvider struct {
	path     string
	now      func() time.Time
	cacheTTL time.Duration

	mu          sync.RWMutex
	cachedToken string
	expiresAt   time.Time
}

// NewTokenFileProvider constructs a file-backed token provider.
func NewTokenFileProvider(path string, cacheTTL time.Duration) *TokenFileProvider {
	return &TokenFileProvider{
		path:     path,
		cacheTTL: cacheTTL,
	}
}

// Path returns the file path this provider reads.
func (t *TokenFileProvider) Path() string {
	return t.path
}

func (t *TokenFileProvider) nowTime() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// GetToken returns a cached token when still valid, otherwise re-reads the file.
func (t *TokenFileProvider) GetToken(_ context.Context) (string, error) {
	now := t.nowTime()
	cacheTTL := t.cacheTTL
	if cacheTTL <= 0 {
		cacheTTL = DefaultTokenFileCacheTTL
	}

	t.mu.RLock()
	if t.cachedToken != "" && now.Before(t.expiresAt) {
		token := t.cachedToken
		t.mu.RUnlock()
		return token, nil
	}
	t.mu.RUnlock()

	rawToken, err := os.ReadFile(t.path)
	if err != nil {
		return "", fmt.Errorf("failed to read token file %q: %w", t.path, err)
	}

	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return "", fmt.Errorf("token file %q is empty", t.path)
	}

	t.mu.Lock()
	t.cachedToken = token
	t.expiresAt = t.nowTime().Add(cacheTTL)
	t.mu.Unlock()

	return token, nil
}

// TokenFileCacheTTLFromEnv returns the configured cache TTL or the default.
func TokenFileCacheTTLFromEnv() time.Duration {
	tokenCacheTTL := DefaultTokenFileCacheTTL
	if raw, ok := os.LookupEnv(TokenCacheTTLEnv); ok {
		if ttlSeconds, err := strconv.Atoi(raw); err == nil && ttlSeconds > 0 {
			tokenCacheTTL = time.Duration(ttlSeconds) * time.Second
		}
	}
	return tokenCacheTTL
}

// TokenProviderFromEnv chooses the management-token source once at process start.
//
// Preference:
//  1. LINODE_TOKEN_FILE when set and readable at startup → file provider (hot-reload)
//  2. Otherwise LINODE_TOKEN if set → static provider
//
// Source selection is not re-evaluated later: if the file provider was chosen,
// later read failures do not fall back to LINODE_TOKEN (requests fail until the
// file is readable again). Startup fallback to env only applies when the file
// is missing/unreadable at init (e.g. mount race); prefer setting only
// LINODE_TOKEN_FILE in production if you require file-backed auth.
//
// Returns the provider and a short description of the source for logging
// (never includes the token value).
func TokenProviderFromEnv(ctx context.Context) (TokenProvider, string, error) {
	tokenFilePath := strings.TrimSpace(os.Getenv(TokenFilePathEnv))
	if tokenFilePath != "" {
		cacheTTL := TokenFileCacheTTLFromEnv()
		fileProvider := NewTokenFileProvider(tokenFilePath, cacheTTL)
		if _, err := fileProvider.GetToken(ctx); err == nil {
			return fileProvider.GetToken, fmt.Sprintf("file %q (cache TTL %s)", fileProvider.Path(), cacheTTL), nil
		} else {
			// Fall back to env only at startup if the file is not yet present.
			if envToken := strings.TrimSpace(os.Getenv(AccessTokenEnv)); envToken != "" {
				return StaticTokenProvider(envToken), fmt.Sprintf("environment variable %q (file %q not readable at startup: %v)", AccessTokenEnv, tokenFilePath, err), nil
			}
			return nil, "", fmt.Errorf("failed to load linode API token from %s=%q: %w", TokenFilePathEnv, tokenFilePath, err)
		}
	}

	if envToken := strings.TrimSpace(os.Getenv(AccessTokenEnv)); envToken != "" {
		return StaticTokenProvider(envToken), fmt.Sprintf("environment variable %q", AccessTokenEnv), nil
	}

	return nil, "", fmt.Errorf("linode API token required: set %s or %s", AccessTokenEnv, TokenFilePathEnv)
}
