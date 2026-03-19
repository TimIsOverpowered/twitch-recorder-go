package segment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfigValid(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = ValidateConfig(tempDir, []string{"valid_channel"})
	assert.NoError(t, err)
}

func TestValidateConfigCreateDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	newDir := filepath.Join(tempDir, "new_directory")

	err = ValidateConfig(newDir, []string{"valid_channel"})
	assert.NoError(t, err)

	_, err = os.Stat(newDir)
	assert.NoError(t, err)
}

func TestValidateConfigEmptyVodDirectory(t *testing.T) {
	err := ValidateConfig("", []string{"valid_channel"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vod_directory is required")
}

func TestValidateConfigEmptyChannels(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = ValidateConfig(tempDir, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one channel must be specified")
}

func TestValidateConfigEmptyChannelName(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = ValidateConfig(tempDir, []string{""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "channel name cannot be empty")
}

func TestValidateConfigChannelTooLong(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = ValidateConfig(tempDir, []string{"this_channel_name_is_way_too_long_for_twitch_rules"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum length")
}

func TestValidateConfigInvalidCharacters(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []string{
		"invalid@channel",
		"invalid#channel",
		"invalid channel",
		"invalid/channel",
	}

	for _, channel := range tests {
		t.Run(channel, func(t *testing.T) {
			err = ValidateConfig(tempDir, []string{channel})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "contains invalid characters")
		})
	}
}

func TestValidateConfigValidCharacters(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	validChannels := []string{
		"valid_channel",
		"ValidChannel123",
		"channel_with_underscores",
		"a1b2c3",
	}

	for _, channel := range validChannels {
		t.Run(channel, func(t *testing.T) {
			err = ValidateConfig(tempDir, []string{channel})
			assert.NoError(t, err)
		})
	}
}

func TestValidateConfigNotWritable(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	readOnlyDir := filepath.Join(tempDir, "readonly")
	err = os.MkdirAll(readOnlyDir, 0555)
	require.NoError(t, err)

	err = ValidateConfig(readOnlyDir, []string{"valid_channel"})
	if err != nil {
		assert.Contains(t, err.Error(), "not writable")
	}
}
