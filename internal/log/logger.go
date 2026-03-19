package log

import (
	"fmt"
	"log/slog"
	"os"
)

var Logger *slog.Logger

func Init() {
	Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func Debug(msg string, args ...any) {
	if Logger != nil {
		Logger.Debug(msg, args...)
	}
}

func Info(msg string, args ...any) {
	if Logger != nil {
		Logger.Info(msg, args...)
	}
}

func Warn(msg string, args ...any) {
	if Logger != nil {
		Logger.Warn(msg, args...)
	}
}

func Error(msg string, args ...any) {
	if Logger != nil {
		Logger.Error(msg, args...)
	}
}

func Debugf(format string, args ...any) {
	if Logger != nil {
		Logger.Debug(fmt.Sprintf(format, args...))
	}
}

func Infof(format string, args ...any) {
	if Logger != nil {
		Logger.Info(fmt.Sprintf(format, args...))
	}
}

func Warnf(format string, args ...any) {
	if Logger != nil {
		Logger.Warn(fmt.Sprintf(format, args...))
	}
}

func Errorf(format string, args ...any) {
	if Logger != nil {
		Logger.Error(fmt.Sprintf(format, args...))
	}
}
