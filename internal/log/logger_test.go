package log

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	Init("info")
	assert.NotNil(t, Logger)
}

func TestDebug(t *testing.T) {
	Init("debug")
	Debug("test message")
	Debugf("test %s", "formatted")
}

func TestInfo(t *testing.T) {
	Init("info")
	Info("test message")
	Infof("test %s", "formatted")
}

func TestWarn(t *testing.T) {
	Init("warn")
	Warn("test message")
	Warnf("test %s", "formatted")
}

func TestError(t *testing.T) {
	Init("error")
	Error("test message")
	Errorf("test %s", "formatted")
}
