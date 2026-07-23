package linode

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/linode-obs/latr/pkg/models"
	"github.com/linode/linodego"
)

// Client wraps the linodego client
type Client struct {
	client        *linodego.Client
	tokenProvider TokenProvider
}

// tokenTransport injects Authorization from TokenProvider on each request so
// file-backed tokens can rotate without reconstructing the linodego client.
type tokenTransport struct {
	base          http.RoundTripper
	tokenProvider TokenProvider
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokenProvider(req.Context())
	if err != nil {
		return nil, err
	}

	clone := req.Clone(req.Context())
	if token != "" {
		clone.Header.Set("Authorization", "Bearer "+token)
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// NewClient creates a new Linode API client with a static token.
// Prefer NewClientWithTokenProvider when the management PAT may rotate without restart.
func NewClient(token string) *Client {
	return NewClientWithTokenProvider(StaticTokenProvider(token))
}

// NewClientWithTokenProvider creates a Linode API client that obtains the bearer
// token from tokenProvider on each HTTP request (hot-reload friendly).
func NewClientWithTokenProvider(tokenProvider TokenProvider) *Client {
	if tokenProvider == nil {
		tokenProvider = StaticTokenProvider("")
	}

	httpClient := &http.Client{
		Transport: &tokenTransport{
			base:          http.DefaultTransport,
			tokenProvider: tokenProvider,
		},
	}

	linodeClient := linodego.NewClient(httpClient)

	// Support base URL override for testing
	baseURL := os.Getenv("LINODE_API_URL")
	if baseURL != "" {
		linodeClient.SetBaseURL(baseURL)
	}

	// Do not call SetToken: auth is applied per-request by tokenTransport.
	return &Client{
		client:        &linodeClient,
		tokenProvider: tokenProvider,
	}
}

// CreateToken creates a new Linode API token
func (c *Client) CreateToken(ctx context.Context, label, scopes string, expiry time.Time) (*models.Token, error) {
	createOpts := linodego.TokenCreateOptions{
		Label:  label,
		Scopes: scopes,
		Expiry: &expiry,
	}

	token, err := c.client.CreateToken(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create token: %w", err)
	}

	return &models.Token{
		ID:        token.ID,
		Label:     token.Label,
		Token:     token.Token,
		CreatedAt: *token.Created,
		ExpiresAt: *token.Expiry,
		Scopes:    token.Scopes,
		Validity:  time.Until(*token.Expiry),
	}, nil
}

// GetToken retrieves a token by ID
func (c *Client) GetToken(ctx context.Context, tokenID int) (*models.Token, error) {
	token, err := c.client.GetToken(ctx, tokenID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	created := time.Now()
	if token.Created != nil {
		created = *token.Created
	}

	expiry := time.Now().Add(90 * 24 * time.Hour)
	if token.Expiry != nil {
		expiry = *token.Expiry
	}

	return &models.Token{
		ID:        token.ID,
		Label:     token.Label,
		Token:     "", // The API doesn't return the token value for existing tokens
		CreatedAt: created,
		ExpiresAt: expiry,
		Scopes:    token.Scopes,
		Validity:  expiry.Sub(created),
	}, nil
}

// FindTokenByLabel finds a token by its label
// Note: more than one token may have the same label
func (c *Client) FindTokenByLabel(ctx context.Context, label string) ([]*models.Token, error) {
	f := linodego.Filter{}
	f.AddField(linodego.Eq, "label", label)

	filterStr, err := f.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("unable to apply Linode API filter to tokens: %w", err)
	}
	opts := linodego.NewListOptions(0, string(filterStr))

	tokens, err := c.client.ListTokens(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}

	var result []*models.Token
	for _, token := range tokens {
		created := time.Now()
		if token.Created != nil {
			created = *token.Created
		}

		expiry := time.Now().Add(90 * 24 * time.Hour)
		if token.Expiry != nil {
			expiry = *token.Expiry
		}

		result = append(result, &models.Token{
			ID:        token.ID,
			Label:     token.Label,
			Token:     "", // The API doesn't return the token value for existing tokens
			CreatedAt: created,
			ExpiresAt: expiry,
			Scopes:    token.Scopes,
			Validity:  expiry.Sub(created),
		})
	}

	return result, nil
}

// UpdateToken updates a token's expiry (if supported by the API)
// Note: Linode API may not support updating token expiry, so we may need to create a new one
func (c *Client) UpdateToken(ctx context.Context, tokenID int, expiry time.Time) error {
	updateOpts := linodego.TokenUpdateOptions{
		Label: "", // Keep existing label
	}

	_, err := c.client.UpdateToken(ctx, tokenID, updateOpts)
	if err != nil {
		return fmt.Errorf("failed to update token: %w", err)
	}
	return nil
}

// IsNotFoundError checks if an error is a 404 not found error
func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	if apiErr, ok := err.(*linodego.Error); ok {
		return apiErr.Code == http.StatusNotFound
	}

	return false
}

// ParseScopes parses and returns the scopes string
// This is a simple pass-through for now, but could be enhanced to validate scopes
func ParseScopes(scopes string) string {
	return scopes
}
