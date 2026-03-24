package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/wbh1/latr/internal/config"
)

// MockEngine is a mock of the rotation engine
type MockEngine struct {
	mock.Mock
}

func (m *MockEngine) ProcessToken(ctx context.Context, tokenConfig config.TokenConfig, thresholdPercent int) error {
	args := m.Called(ctx, tokenConfig, thresholdPercent)
	return args.Error(0)
}

func TestScheduler_RunOnce(t *testing.T) {
	mockEngine := new(MockEngine)

	tokens := []config.TokenConfig{
		{Label: "token1", Team: "team1", Validity: "90d", Scopes: "*", Storage: []config.StorageConfig{{Type: "vault", Path: "path1"}}},
		{Label: "token2", Team: "team2", Validity: "180d", Scopes: "*", Storage: []config.StorageConfig{{Type: "vault", Path: "path2"}}},
	}

	accounts := []AccountEntry{
		{
			Account:  config.AccountConfig{Label: "production"},
			Tokens:   tokens,
			Engine:   mockEngine,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
	}

	mockEngine.On("ProcessToken", mock.Anything, tokens[0], 10).Return(nil)
	mockEngine.On("ProcessToken", mock.Anything, tokens[1], 10).Return(nil)

	sched := NewScheduler(
		config.DaemonConfig{Mode: "one-shot"},
		config.RotationConfig{ThresholdPercent: 10},
		accounts,
	)

	ctx := context.Background()
	err := sched.Run(ctx)
	require.NoError(t, err)

	mockEngine.AssertExpectations(t)
}

func TestScheduler_RunOnce_MultipleAccounts(t *testing.T) {
	mockEngine1 := new(MockEngine)
	mockEngine2 := new(MockEngine)

	tokens1 := []config.TokenConfig{
		{Label: "prod-token", Validity: "90d", Scopes: "*", Storage: []config.StorageConfig{{Type: "vault", Path: "path1"}}},
	}
	tokens2 := []config.TokenConfig{
		{Label: "staging-token", Validity: "90d", Scopes: "*", Storage: []config.StorageConfig{{Type: "vault", Path: "path2"}}},
	}

	accounts := []AccountEntry{
		{
			Account:  config.AccountConfig{Label: "production"},
			Tokens:   tokens1,
			Engine:   mockEngine1,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
		{
			Account:  config.AccountConfig{Label: "staging"},
			Tokens:   tokens2,
			Engine:   mockEngine2,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
	}

	mockEngine1.On("ProcessToken", mock.Anything, tokens1[0], 10).Return(nil)
	mockEngine2.On("ProcessToken", mock.Anything, tokens2[0], 10).Return(nil)

	sched := NewScheduler(
		config.DaemonConfig{Mode: "one-shot"},
		config.RotationConfig{ThresholdPercent: 10},
		accounts,
	)

	ctx := context.Background()
	err := sched.Run(ctx)
	require.NoError(t, err)

	// Each engine should have been called with its own account's tokens
	mockEngine1.AssertExpectations(t)
	mockEngine2.AssertExpectations(t)
}

func TestScheduler_RunDaemon(t *testing.T) {
	mockEngine := new(MockEngine)

	tokens := []config.TokenConfig{
		{Label: "token1", Team: "team1", Validity: "90d", Scopes: "*", Storage: []config.StorageConfig{{Type: "vault", Path: "path1"}}},
	}

	accounts := []AccountEntry{
		{
			Account:  config.AccountConfig{Label: "production"},
			Tokens:   tokens,
			Engine:   mockEngine,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
	}

	mockEngine.On("ProcessToken", mock.Anything, tokens[0], 10).Return(nil)

	sched := NewScheduler(
		config.DaemonConfig{Mode: "daemon", CheckInterval: "100ms"},
		config.RotationConfig{ThresholdPercent: 10},
		accounts,
	)

	// Create a context that will be cancelled after a short time
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	err := sched.Run(ctx)
	// Context cancellation is expected
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// ProcessToken should have been called at least twice (initial + at least one interval)
	callCount := 0
	for _, call := range mockEngine.Calls {
		if call.Method == "ProcessToken" {
			callCount++
		}
	}
	assert.GreaterOrEqual(t, callCount, 2, "ProcessToken should have been called at least twice")
}

func TestScheduler_RunDaemon_GracefulShutdown(t *testing.T) {
	mockEngine := new(MockEngine)

	tokens := []config.TokenConfig{
		{Label: "token1", Team: "team1", Validity: "90d", Scopes: "*", Storage: []config.StorageConfig{{Type: "vault", Path: "path1"}}},
	}

	accounts := []AccountEntry{
		{
			Account:  config.AccountConfig{Label: "production"},
			Tokens:   tokens,
			Engine:   mockEngine,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
	}

	mockEngine.On("ProcessToken", mock.Anything, tokens[0], 10).Return(nil)

	sched := NewScheduler(
		config.DaemonConfig{Mode: "daemon", CheckInterval: "1s"},
		config.RotationConfig{ThresholdPercent: 10},
		accounts,
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context immediately after starting
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := sched.Run(ctx)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Should have run at least once
	mockEngine.AssertCalled(t, "ProcessToken", mock.Anything, tokens[0], 10)
}

func TestScheduler_TokenRotationThresholdOverride(t *testing.T) {
	mockEngine := new(MockEngine)

	tokens := []config.TokenConfig{
		{
			Label:             "token-with-override",
			Team:              "team1",
			Validity:          "90d",
			Scopes:            "*",
			RotationThreshold: 20, // Token-specific override
			Storage:           []config.StorageConfig{{Type: "vault", Path: "path1"}},
		},
	}

	accounts := []AccountEntry{
		{
			Account:  config.AccountConfig{Label: "production"},
			Tokens:   tokens,
			Engine:   mockEngine,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
	}

	// Should use the token-specific threshold (20) instead of account-level (10)
	mockEngine.On("ProcessToken", mock.Anything, tokens[0], 20).Return(nil)

	sched := NewScheduler(
		config.DaemonConfig{Mode: "one-shot"},
		config.RotationConfig{ThresholdPercent: 10},
		accounts,
	)

	ctx := context.Background()
	err := sched.Run(ctx)
	require.NoError(t, err)

	mockEngine.AssertExpectations(t)
}

func TestScheduler_NoTokensConfigured(t *testing.T) {
	mockEngine := new(MockEngine)
	accounts := []AccountEntry{
		{
			Account:  config.AccountConfig{Label: "empty"},
			Tokens:   []config.TokenConfig{},
			Engine:   mockEngine,
			Rotation: config.RotationConfig{ThresholdPercent: 10},
		},
	}

	sched := NewScheduler(
		config.DaemonConfig{Mode: "one-shot"},
		config.RotationConfig{ThresholdPercent: 10},
		accounts,
	)

	ctx := context.Background()
	err := sched.Run(ctx)
	require.NoError(t, err)

	mockEngine.AssertNotCalled(t, "ProcessToken", mock.Anything, mock.Anything, mock.Anything)
}
