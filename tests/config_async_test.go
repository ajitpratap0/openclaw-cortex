package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ajitpratap0/openclaw-cortex/internal/config"
)

func TestAsyncConfigDefaults(t *testing.T) {
	cfg, err := config.Load()
	require.NoError(t, err)

	assert.Equal(t, 2, cfg.Async.WorkerCount)
	assert.Equal(t, 512, cfg.Async.QueueCapacity)
	assert.Equal(t, 3, cfg.Async.MaxRetries)
	assert.Equal(t, 5, cfg.Async.RetryDelaySeconds)
	assert.Equal(t, "", cfg.Async.WALPath)
	assert.Equal(t, 1000, cfg.Async.WALCompactEvery)
	assert.False(t, cfg.Async.Disabled)
}

func TestAsyncConfigEnvOverride(t *testing.T) {
	t.Setenv("OPENCLAW_CORTEX_ASYNC_DISABLED", "true")
	t.Setenv("OPENCLAW_CORTEX_ASYNC_WORKER_COUNT", "8")
	t.Setenv("OPENCLAW_CORTEX_ASYNC_QUEUE_CAPACITY", "1024")

	cfg, err := config.Load()
	require.NoError(t, err)

	assert.True(t, cfg.Async.Disabled)
	assert.Equal(t, 8, cfg.Async.WorkerCount)
	assert.Equal(t, 1024, cfg.Async.QueueCapacity)
}

func TestAsyncConfigValidateWorkerCountZero(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Async.WorkerCount = 0
	cfg.Async.QueueCapacity = 512
	cfg.Async.MaxRetries = 3
	cfg.Async.RetryDelaySeconds = 5
	cfg.Async.WALCompactEvery = 1000

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "async.worker_count")
}

func TestAsyncConfigValidateQueueCapacityZero(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Async.WorkerCount = 2
	cfg.Async.QueueCapacity = 0
	cfg.Async.MaxRetries = 3
	cfg.Async.RetryDelaySeconds = 5
	cfg.Async.WALCompactEvery = 1000

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "async.queue_capacity")
}

func TestAsyncConfigValidateNegativeMaxRetries(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Async.WorkerCount = 2
	cfg.Async.QueueCapacity = 512
	cfg.Async.MaxRetries = -1
	cfg.Async.RetryDelaySeconds = 5
	cfg.Async.WALCompactEvery = 1000

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "async.max_retries")
}

func TestAsyncConfigValidateNegativeRetryDelay(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Async.WorkerCount = 2
	cfg.Async.QueueCapacity = 512
	cfg.Async.MaxRetries = 3
	cfg.Async.RetryDelaySeconds = -1
	cfg.Async.WALCompactEvery = 1000

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "async.retry_delay_seconds")
}

func TestAsyncConfigValidateDisabledSkipsChecks(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Async = config.AsyncConfig{
		Disabled:      true,
		WorkerCount:   0,
		QueueCapacity: 0,
		MaxRetries:    0,
	}

	err := cfg.Validate()
	assert.NoError(t, err, "validation should skip async fields when disabled=true")
}

func TestAsyncConfigValidateValid(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Async = config.AsyncConfig{
		WorkerCount:       2,
		QueueCapacity:     512,
		MaxRetries:        3,
		RetryDelaySeconds: 5,
		WALPath:           "",
		WALCompactEvery:   1000,
		Disabled:          false,
	}

	err := cfg.Validate()
	assert.NoError(t, err)
}
