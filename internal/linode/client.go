package linode

import (
	"context"
	"fmt"
	"time"

	"github.com/linode/linodego"
	"github.com/wbh1/latr/pkg/models"
	"golang.org/x/oauth2"
)

// Client wraps the linodego client
type Client struct {
	client *linodego.Client
	token  string
}

// NewClient creates a new Linode API client.
// The apiURL parameter sets the Linode API base URL. If empty, the linodego
// default (https://api.linode.com) is used.
func NewClient(token, apiURL string) *Client {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	oauth2Client := oauth2.NewClient(context.Background(), tokenSource)

	linodeClient := linodego.NewClient(oauth2Client)

	if apiURL != "" {
		linodeClient.SetBaseURL(apiURL)
	}

	return &Client{
		client: &linodeClient,
		token:  token,
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

	created := time.Now()
	if token.Created != nil {
		created = *token.Created
	}
	tokenExpiry := time.Now().Add(90 * 24 * time.Hour)
	if token.Expiry != nil {
		tokenExpiry = *token.Expiry
	}

	return &models.Token{
		ID:        token.ID,
		Label:     token.Label,
		Token:     token.Token,
		CreatedAt: created,
		ExpiresAt: tokenExpiry,
		Scopes:    token.Scopes,
		Validity:  tokenExpiry.Sub(created),
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
