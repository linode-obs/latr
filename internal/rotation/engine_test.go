package rotation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wbh1/latr/internal/config"
	"github.com/wbh1/latr/pkg/models"
)

// MockLinodeClient is a mock implementation of the Linode client
type MockLinodeClient struct {
	mock.Mock
}

func (m *MockLinodeClient) CreateToken(ctx context.Context, label, scopes string, expiry time.Time) (*models.Token, error) {
	args := m.Called(ctx, label, scopes, expiry)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Token), args.Error(1)
}

func (m *MockLinodeClient) FindTokenByLabel(ctx context.Context, label string) ([]*models.Token, error) {
	args := m.Called(ctx, label)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return []*models.Token{args.Get(0).(*models.Token)}, args.Error(1)
}

// MockVaultClient is a mock implementation of the Vault client
type MockVaultClient struct {
	mock.Mock
}

func (m *MockVaultClient) WriteToken(ctx context.Context, path, key, token string) error {
	args := m.Called(ctx, path, key, token)
	return args.Error(0)
}

func (m *MockVaultClient) ReadToken(ctx context.Context, path string) (string, error) {
	args := m.Called(ctx, path)
	return args.String(0), args.Error(1)
}

func (m *MockVaultClient) WriteTokenState(ctx context.Context, path string, state *models.TokenState) error {
	args := m.Called(ctx, path, state)
	return args.Error(0)
}

func (m *MockVaultClient) ReadTokenState(ctx context.Context, path string) (*models.TokenState, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.TokenState), args.Error(1)
}

func TestEngine_ProcessToken_NewToken(t *testing.T) {
	mockLinode := new(MockLinodeClient)
	mockVault := new(MockVaultClient)

	tokenConfig := config.TokenConfig{
		Label:    "new-token",
		Team:     "platform",
		Validity: "90d",
		Scopes:   "*",
		Storage: []config.StorageConfig{
			{Type: "vault", Path: "secret/data/test/new-token"},
		},
	}

	now := time.Now()
	createdToken := &models.Token{
		ID:        123,
		Label:     "new-token",
		Token:     "new-secret-token",
		CreatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
		Scopes:    "*",
		Validity:  90 * 24 * time.Hour,
	}

	// Token doesn't exist yet
	mockLinode.On("FindTokenByLabel", mock.Anything, "new-token").Return(nil, nil)
	mockLinode.On("CreateToken", mock.Anything, "new-token", "*", mock.Anything).Return(createdToken, nil)

	// Vault operations
	mockVault.On("ReadTokenState", mock.Anything, "secret/data/test/new-token").Return(nil, nil)
	mockVault.On("WriteToken", mock.Anything, "secret/data/test/new-token", "", "new-secret-token").Return(nil)
	mockVault.On("WriteTokenState", mock.Anything, "secret/data/test/new-token", mock.Anything).Return(nil)

	engine := &Engine{
		linodeClient: mockLinode,
		vaultClient:  mockVault,
		dryRun:       false,
	}

	ctx := context.Background()
	err := engine.ProcessToken(ctx, tokenConfig, 10)
	require.NoError(t, err)

	mockLinode.AssertExpectations(t)
	mockVault.AssertExpectations(t)
}

func TestEngine_ProcessToken_ExistingToken_NoRotationNeeded(t *testing.T) {
	mockLinode := new(MockLinodeClient)
	mockVault := new(MockVaultClient)

	tokenConfig := config.TokenConfig{
		Label:    "existing-token",
		Team:     "platform",
		Validity: "90d",
		Scopes:   "*",
		Storage: []config.StorageConfig{
			{Type: "vault", Path: "secret/data/test/existing-token"},
		},
	}

	now := time.Now()
	existingToken := &models.Token{
		ID:        123,
		Label:     "existing-token",
		Token:     "",
		CreatedAt: now.Add(-10 * 24 * time.Hour), // Created 10 days ago
		ExpiresAt: now.Add(80 * 24 * time.Hour),  // Expires in 80 days (88% remaining)
		Scopes:    "*",
		Validity:  90 * 24 * time.Hour,
	}

	mockLinode.On("FindTokenByLabel", mock.Anything, "existing-token").Return(existingToken, nil)

	engine := &Engine{
		linodeClient: mockLinode,
		vaultClient:  mockVault,
		dryRun:       false,
	}

	ctx := context.Background()
	err := engine.ProcessToken(ctx, tokenConfig, 10)
	require.NoError(t, err)

	mockLinode.AssertExpectations(t)
	// No vault operations should be called since no rotation is needed
}

func TestEngine_ProcessToken_ExistingToken_NeedsRotation(t *testing.T) {
	mockLinode := new(MockLinodeClient)
	mockVault := new(MockVaultClient)

	tokenConfig := config.TokenConfig{
		Label:    "existing-token",
		Team:     "platform",
		Validity: "90d",
		Scopes:   "*",
		Storage: []config.StorageConfig{
			{Type: "vault", Path: "secret/data/test/existing-token"},
		},
	}

	now := time.Now()
	existingToken := &models.Token{
		ID:        123,
		Label:     "existing-token",
		Token:     "",
		CreatedAt: now.Add(-81 * 24 * time.Hour), // Created 81 days ago
		ExpiresAt: now.Add(9 * 24 * time.Hour),   // Expires in 9 days (10% remaining)
		Scopes:    "*",
		Validity:  90 * 24 * time.Hour,
	}

	newToken := &models.Token{
		ID:        456,
		Label:     "existing-token",
		Token:     "new-rotated-token",
		CreatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
		Scopes:    "*",
		Validity:  90 * 24 * time.Hour,
	}

	existingState := &models.TokenState{
		Label:           "existing-token",
		CurrentLinodeID: 123,
		RotationCount:   0,
	}

	mockLinode.On("FindTokenByLabel", mock.Anything, "existing-token").Return(existingToken, nil)
	mockLinode.On("CreateToken", mock.Anything, "existing-token", "*", mock.Anything).Return(newToken, nil)

	mockVault.On("ReadTokenState", mock.Anything, "secret/data/test/existing-token").Return(existingState, nil)
	mockVault.On("WriteToken", mock.Anything, "secret/data/test/existing-token", "", "new-rotated-token").Return(nil)
	mockVault.On("WriteTokenState", mock.Anything, "secret/data/test/existing-token", mock.MatchedBy(func(state *models.TokenState) bool {
		return state.CurrentLinodeID == 456 &&
			state.PreviousLinodeID == 123 &&
			state.RotationCount == 1
	})).Return(nil)

	engine := &Engine{
		linodeClient: mockLinode,
		vaultClient:  mockVault,
		dryRun:       false,
	}

	ctx := context.Background()
	err := engine.ProcessToken(ctx, tokenConfig, 10)
	require.NoError(t, err)

	mockLinode.AssertExpectations(t)
	mockVault.AssertExpectations(t)
}

func TestEngine_ProcessToken_DryRunMode(t *testing.T) {
	mockLinode := new(MockLinodeClient)
	mockVault := new(MockVaultClient)

	tokenConfig := config.TokenConfig{
		Label:    "dry-run-token",
		Team:     "platform",
		Validity: "90d",
		Scopes:   "*",
		Storage: []config.StorageConfig{
			{Type: "vault", Path: "secret/data/test/dry-run-token"},
		},
	}

	// Token doesn't exist
	mockLinode.On("FindTokenByLabel", mock.Anything, "dry-run-token").Return(nil, nil)

	engine := &Engine{
		linodeClient: mockLinode,
		vaultClient:  mockVault,
		dryRun:       true,
	}

	ctx := context.Background()
	err := engine.ProcessToken(ctx, tokenConfig, 10)
	require.NoError(t, err)

	// Should only check if token exists, but not create anything
	mockLinode.AssertExpectations(t)
	mockVault.AssertNotCalled(t, "WriteToken")
	mockVault.AssertNotCalled(t, "WriteTokenState")
}

func TestEngine_ProcessToken_LinodeCreateFails(t *testing.T) {
	mockLinode := new(MockLinodeClient)
	mockVault := new(MockVaultClient)

	tokenConfig := config.TokenConfig{
		Label:    "new-token",
		Team:     "platform",
		Validity: "90d",
		Scopes:   "*",
		Storage: []config.StorageConfig{
			{Type: "vault", Path: "secret/data/test/new-token"},
		},
	}

	// Token doesn't exist
	mockLinode.On("FindTokenByLabel", mock.Anything, "new-token").Return(nil, nil)
	mockLinode.On("CreateToken", mock.Anything, "new-token", "*", mock.Anything).Return(nil, errors.New("API error"))

	mockVault.On("ReadTokenState", mock.Anything, "secret/data/test/new-token").Return(nil, nil)

	engine := &Engine{
		linodeClient: mockLinode,
		vaultClient:  mockVault,
		dryRun:       false,
	}

	ctx := context.Background()
	err := engine.ProcessToken(ctx, tokenConfig, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create token")

	mockLinode.AssertExpectations(t)
	// Vault write should not be called if Linode creation fails
	mockVault.AssertNotCalled(t, "WriteToken")
}

func TestEngine_ProcessToken_VaultWriteFails_StateTracked(t *testing.T) {
	mockLinode := new(MockLinodeClient)
	mockVault := new(MockVaultClient)

	tokenConfig := config.TokenConfig{
		Label:    "new-token",
		Team:     "platform",
		Validity: "90d",
		Scopes:   "*",
		Storage: []config.StorageConfig{
			{Type: "vault", Path: "secret/data/test/new-token"},
		},
	}

	now := time.Now()
	createdToken := &models.Token{
		ID:        123,
		Label:     "new-token",
		Token:     "new-secret-token",
		CreatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
		Scopes:    "*",
		Validity:  90 * 24 * time.Hour,
	}

	mockLinode.On("FindTokenByLabel", mock.Anything, "new-token").Return(nil, nil)
	mockLinode.On("CreateToken", mock.Anything, "new-token", "*", mock.Anything).Return(createdToken, nil)

	mockVault.On("ReadTokenState", mock.Anything, "secret/data/test/new-token").Return(nil, nil)
	mockVault.On("WriteToken", mock.Anything, "secret/data/test/new-token", "", "new-secret-token").Return(errors.New("vault error"))
	// State should still be written to track that we need to retry Vault write
	mockVault.On("WriteTokenState", mock.Anything, "secret/data/test/new-token", mock.Anything).Return(nil)

	engine := &Engine{
		linodeClient: mockLinode,
		vaultClient:  mockVault,
		dryRun:       false,
	}

	ctx := context.Background()
	err := engine.ProcessToken(ctx, tokenConfig, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to store token in vault")

	mockLinode.AssertExpectations(t)
	mockVault.AssertExpectations(t)
}
