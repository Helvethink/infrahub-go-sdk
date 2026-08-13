package cli

import (
	"fmt"
	"io"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func newCLILogger(output io.Writer, level zapcore.Level) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(output),
		level,
	)
	return zap.New(core)
}

func parseLogLevel(value string) (zapcore.Level, bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return zap.DebugLevel, true, nil
	case "info":
		return zap.InfoLevel, true, nil
	case "warn", "warning":
		return zap.WarnLevel, true, nil
	case "", "error":
		return zap.ErrorLevel, true, nil
	case "off":
		return zap.ErrorLevel, false, nil
	default:
		return zap.ErrorLevel, false, fmt.Errorf("invalid log level %q: use debug, info, warn, error, or off", value)
	}
}
