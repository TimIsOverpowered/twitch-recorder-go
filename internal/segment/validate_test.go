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

	longName := "this_channel_name_is_way_too_long_for_twitch_rules"
	err = ValidateConfig(tempDir, []string{longName})
	assert.NoError(t, err, "Long channel names should be sanitized, not rejected")
}

func TestValidateConfigInvalidCharacters(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		input    string
		expected string
	}{
		{"invalid@channel", "invalid_channel"},
		{"invalid#channel", "invalid_channel"},
		{"invalid channel", "invalid_channel"},
		{"invalid/channel", "invalid_channel"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			err = ValidateConfig(tempDir, []string{tc.input})
			assert.NoError(t, err, "Invalid characters should be sanitized, not rejected")
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
