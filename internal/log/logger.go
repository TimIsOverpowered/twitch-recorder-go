package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

var Logger *slog.Logger

type customHandler struct {
	opts *slog.HandlerOptions
	w    io.Writer
}

func (h *customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *customHandler) Handle(ctx context.Context, r slog.Record) error {
	var buf bytes.Buffer

	timeStr := r.Time.UTC().Format("2006/01/02 15:04:05")
	buf.WriteString(timeStr)
	buf.WriteByte(' ')

	levelStr := strings.ToUpper(r.Level.String())
	buf.WriteString("[")
	buf.WriteString(levelStr)
	buf.WriteString("]")

	channelAttr := h.getChannelAttr(r)
	if channelAttr != "" {
		buf.WriteString(" [")
		buf.WriteString(channelAttr)
		buf.WriteString("]")
	}

	buf.WriteByte(' ')
	buf.WriteString(r.Message)

	if r.NumAttrs() > 0 {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "channel" {
				return true
			}
			buf.WriteByte(' ')
			buf.WriteString(a.Key)
			buf.WriteByte('=')
			writeValue(&buf, a.Value)
			return true
		})
	}

	buf.WriteByte('\n')
	_, err := h.w.Write(buf.Bytes())
	return err
}

func (h *customHandler) getChannelAttr(r slog.Record) string {
	var channel string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "channel" {
			channel = a.Value.String()
		}
		return true
	})
	return channel
}

func (h *customHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	opts := *h.opts
	return &customHandler{opts: &opts, w: h.w}
}

func (h *customHandler) WithGroup(name string) slog.Handler {
	return nil
}

func writeValue(buf *bytes.Buffer, v slog.Value) {
	switch v.Kind() {
	case slog.KindString:
		buf.WriteString(v.String())
	case slog.KindInt64:
		fmt.Fprintf(buf, "%d", v.Int64())
	case slog.KindUint64:
		fmt.Fprintf(buf, "%d", v.Uint64())
	case slog.KindFloat64:
		fmt.Fprintf(buf, "%v", v.Float64())
	case slog.KindBool:
		fmt.Fprintf(buf, "%t", v.Bool())
	default:
		buf.WriteString(v.String())
	}
}

func Init(level string) {
	var slogLevel slog.Level
	switch level {
	case "error":
		slogLevel = slog.LevelError
	case "warn":
		slogLevel = slog.LevelWarn
	case "debug":
		slogLevel = slog.LevelDebug
	default:
		slogLevel = slog.LevelInfo
	}

	Logger = slog.New(&customHandler{
		opts: &slog.HandlerOptions{
			Level: slogLevel,
		},
		w: os.Stdout,
	})
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

func DebugC(channel, msg string, args ...any) {
	if Logger != nil {
		allArgs := append([]any{"channel", channel}, args...)
		Logger.Debug(msg, allArgs...)
	}
}

func InfoC(channel, msg string, args ...any) {
	if Logger != nil {
		allArgs := append([]any{"channel", channel}, args...)
		Logger.Info(msg, allArgs...)
	}
}

func WarnC(channel, msg string, args ...any) {
	if Logger != nil {
		allArgs := append([]any{"channel", channel}, args...)
		Logger.Warn(msg, allArgs...)
	}
}

func ErrorC(channel, msg string, args ...any) {
	if Logger != nil {
		allArgs := append([]any{"channel", channel}, args...)
		Logger.Error(msg, allArgs...)
	}
}

func DebugfC(channel, format string, args ...any) {
	if Logger != nil {
		Logger.Debug(fmt.Sprintf(format, args...), "channel", channel)
	}
}

func InfofC(channel, format string, args ...any) {
	if Logger != nil {
		Logger.Info(fmt.Sprintf(format, args...), "channel", channel)
	}
}

func WarnfC(channel, format string, args ...any) {
	if Logger != nil {
		Logger.Warn(fmt.Sprintf(format, args...), "channel", channel)
	}
}

func ErrorfC(channel, format string, args ...any) {
	if Logger != nil {
		Logger.Error(fmt.Sprintf(format, args...), "channel", channel)
	}
}
