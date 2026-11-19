package prettylog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Logger wraps slog.Logger; WithField and WithFields return Logger, not an entry.
// A logger can be stored as a field on a long-lived service or per-request object.
type Logger struct {
	core   *slog.Logger
	shared *shared
	ctx    context.Context
}

// Slog provides access for libraries that accept a standard *slog.Logger.
func (l *Logger) Slog() *slog.Logger { return l.core }

func (l *Logger) WithField(key string, value any) *Logger {
	return &Logger{core: l.core.With(key, value), shared: l.shared, ctx: l.ctx}
}
func (l *Logger) WithFields(fields Fields) *Logger {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]any, 0, len(keys)*2)
	for _, k := range keys {
		args = append(args, k, fields[k])
	}
	return &Logger{core: l.core.With(args...), shared: l.shared, ctx: l.ctx}
}

// WithContext binds a default context to this logger and its children.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if ctx == nil {
		ctx = context.Background()
	}
	child := *l
	child.ctx = ctx
	return &child
}
func (l *Logger) defaultContext() context.Context {
	if l.ctx != nil {
		return l.ctx
	}
	return context.Background()
}

func (l *Logger) WithError(err error) *Logger    { return l.WithField("error", err) }
func (l *Logger) WithModule(name string) *Logger { return l.WithField("module", name) }
func (l *Logger) WithTenant(id any) *Logger      { return l.WithField("tenant_id", id) }
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{core: l.core.WithGroup(name), shared: l.shared, ctx: l.ctx}
}

// SetLevel/SetFormatter affect existing child loggers, like the shared Logrus configuration.
func (l *Logger) SetLevel(name string) error {
	level, ok := l.shared.byName[strings.ToUpper(strings.TrimSpace(name))]
	if !ok {
		return fmt.Errorf("unknown level %q", name)
	}
	l.shared.min.Set(level)
	return nil
}
func (l *Logger) GetLevel() string {
	if spec, ok := l.shared.byValue[l.shared.min.Level()]; ok {
		return spec.Name
	}
	return l.shared.min.Level().String()
}
func (l *Logger) SetFormatter(format Formatter) {
	if format == nil {
		format = ModernEmojiFormatter{}
	}
	l.shared.mu.Lock()
	l.shared.opts.Formatter = format
	l.shared.mu.Unlock()
}
func (l *Logger) SetOutput(output io.Writer) error {
	if output == nil {
		return fmt.Errorf("output must not be nil")
	}
	l.shared.mu.Lock()
	l.shared.opts.Output = output
	l.shared.mu.Unlock()
	return nil
}

// Logrus-compatible message methods: arguments are joined with fmt.Sprint.
// Use WithField(s) for structured attributes (not Info("msg", "key", value)).
func (l *Logger) log(ctx context.Context, lvl slog.Level, args ...any) {
	message, pretty := formatMessageArgs(args)
	if pretty {
		l.core.With(internalPrettyMessageKey, true).Log(ctx, lvl, message)
		return
	}
	l.core.Log(ctx, lvl, message)
}

func formatMessageArgs(args []any) (string, bool) {
	hasPrettyJSON := false
	for _, arg := range args {
		if _, ok := getPrettyJSONText(arg); ok {
			hasPrettyJSON = true
			break
		}
	}
	if !hasPrettyJSON {
		return fmt.Sprint(args...), false
	}

	segments := make([]string, 0, len(args))
	regular := make([]any, 0, len(args))
	flushRegular := func() {
		if len(regular) == 0 {
			return
		}
		segments = append(segments, SanitizeControl(fmt.Sprint(regular...)))
		regular = regular[:0]
	}
	for _, arg := range args {
		if pretty, ok := getPrettyJSONText(arg); ok {
			flushRegular()
			segments = append(segments, pretty)
			continue
		}
		regular = append(regular, arg)
	}
	flushRegular()
	return strings.Join(segments, "\n"), true
}
func (l *Logger) logf(ctx context.Context, lvl slog.Level, format string, args ...any) {
	l.core.Log(ctx, lvl, fmt.Sprintf(format, args...))
}
func (l *Logger) logln(ctx context.Context, lvl slog.Level, args ...any) {
	l.core.Log(ctx, lvl, strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
}

func (l *Logger) Trace(args ...any) { l.log(l.defaultContext(), TraceLevel, args...) }
func (l *Logger) Tracef(format string, args ...any) {
	l.logf(l.defaultContext(), TraceLevel, format, args...)
}
func (l *Logger) Traceln(args ...any) { l.logln(l.defaultContext(), TraceLevel, args...) }
func (l *Logger) Debug(args ...any)   { l.log(l.defaultContext(), DebugLevel, args...) }
func (l *Logger) Debugf(format string, args ...any) {
	l.logf(l.defaultContext(), DebugLevel, format, args...)
}
func (l *Logger) Debugln(args ...any) { l.logln(l.defaultContext(), DebugLevel, args...) }
func (l *Logger) Info(args ...any)    { l.log(l.defaultContext(), InfoLevel, args...) }
func (l *Logger) Infof(format string, args ...any) {
	l.logf(l.defaultContext(), InfoLevel, format, args...)
}
func (l *Logger) Infoln(args ...any) { l.logln(l.defaultContext(), InfoLevel, args...) }

// JSON logs an INFO message with an indented JSON field in console formatters.
// JSONFormatter keeps the field as structured JSON rather than a string.
func (l *Logger) JSON(message string, payload any) {
	l.WithField("json", l.PrettyJSON(payload)).Info(message)
}

// JSONf is the formatted-message variant of JSON. The payload intentionally
// precedes the formatting arguments: JSONf(format, payload, args...).
func (l *Logger) JSONf(format string, payload any, args ...any) {
	l.WithField("json", l.PrettyJSON(payload)).Infof(format, args...)
}

// Print, Printf and Println are Logrus-compatible aliases for INFO.
func (l *Logger) Print(args ...any)                 { l.Info(args...) }
func (l *Logger) Printf(format string, args ...any) { l.Infof(format, args...) }
func (l *Logger) Println(args ...any)               { l.Infoln(args...) }

func (l *Logger) Success(args ...any) { l.log(l.defaultContext(), SuccessLevel, args...) }
func (l *Logger) Successf(format string, args ...any) {
	l.logf(l.defaultContext(), SuccessLevel, format, args...)
}
func (l *Logger) Successln(args ...any) { l.logln(l.defaultContext(), SuccessLevel, args...) }
func (l *Logger) Timing(args ...any)    { l.log(l.defaultContext(), TimingLevel, args...) }
func (l *Logger) Timingf(format string, args ...any) {
	l.logf(l.defaultContext(), TimingLevel, format, args...)
}
func (l *Logger) Timingln(args ...any) { l.logln(l.defaultContext(), TimingLevel, args...) }
func (l *Logger) Warn(args ...any)     { l.log(l.defaultContext(), WarnLevel, args...) }
func (l *Logger) Warnf(format string, args ...any) {
	l.logf(l.defaultContext(), WarnLevel, format, args...)
}
func (l *Logger) Warning(args ...any)                 { l.Warn(args...) }
func (l *Logger) Warningf(format string, args ...any) { l.Warnf(format, args...) }
func (l *Logger) Warningln(args ...any)               { l.Warnln(args...) }
func (l *Logger) Warnln(args ...any)                  { l.logln(l.defaultContext(), WarnLevel, args...) }
func (l *Logger) Error(args ...any)                   { l.log(l.defaultContext(), ErrorLevel, args...) }
func (l *Logger) Errorf(format string, args ...any) {
	l.logf(l.defaultContext(), ErrorLevel, format, args...)
}
func (l *Logger) Errorln(args ...any) { l.logln(l.defaultContext(), ErrorLevel, args...) }
func (l *Logger) Fatal(args ...any) {
	l.log(l.defaultContext(), FatalLevel, args...)
	l.shared.exitFunc(1)
}
func (l *Logger) Fatalf(format string, args ...any) {
	l.logf(l.defaultContext(), FatalLevel, format, args...)
	l.shared.exitFunc(1)
}
func (l *Logger) Fatalln(args ...any) {
	l.logln(l.defaultContext(), FatalLevel, args...)
	l.shared.exitFunc(1)
}
func (l *Logger) Panic(args ...any) {
	msg := fmt.Sprint(args...)
	l.log(l.defaultContext(), PanicLevel, msg)
	panic(msg)
}
func (l *Logger) Panicf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.log(l.defaultContext(), PanicLevel, msg)
	panic(msg)
}
func (l *Logger) Panicln(args ...any) {
	msg := strings.TrimSuffix(fmt.Sprintln(args...), "\n")
	l.log(l.defaultContext(), PanicLevel, msg)
	panic(msg)
}

// Context variants forward the context to the slog.Handler.
func (l *Logger) TraceContext(ctx context.Context, args ...any)   { l.log(ctx, TraceLevel, args...) }
func (l *Logger) DebugContext(ctx context.Context, args ...any)   { l.log(ctx, DebugLevel, args...) }
func (l *Logger) InfoContext(ctx context.Context, args ...any)    { l.log(ctx, InfoLevel, args...) }
func (l *Logger) SuccessContext(ctx context.Context, args ...any) { l.log(ctx, SuccessLevel, args...) }
func (l *Logger) TimingContext(ctx context.Context, args ...any)  { l.log(ctx, TimingLevel, args...) }
func (l *Logger) WarnContext(ctx context.Context, args ...any)    { l.log(ctx, WarnLevel, args...) }
func (l *Logger) ErrorContext(ctx context.Context, args ...any)   { l.log(ctx, ErrorLevel, args...) }

// Arbitrary custom levels registered through Options.CustomLevels.
func (l *Logger) Log(levelName string, args ...any) error {
	return l.LogContext(l.defaultContext(), levelName, args...)
}
func (l *Logger) LogContext(ctx context.Context, levelName string, args ...any) error {
	lvl, ok := l.shared.byName[strings.ToUpper(strings.TrimSpace(levelName))]
	if !ok {
		return fmt.Errorf("unknown log level %q", levelName)
	}
	l.log(ctx, lvl, args...)
	return nil
}
func (l *Logger) Logf(levelName, format string, args ...any) error {
	lvl, ok := l.shared.byName[strings.ToUpper(strings.TrimSpace(levelName))]
	if !ok {
		return fmt.Errorf("unknown log level %q", levelName)
	}
	l.logf(l.defaultContext(), lvl, format, args...)
	return nil
}
func (l *Logger) LogAttrs(ctx context.Context, levelName, message string, attrs ...slog.Attr) error {
	lvl, ok := l.shared.byName[strings.ToUpper(strings.TrimSpace(levelName))]
	if !ok {
		return fmt.Errorf("unknown log level %q", levelName)
	}
	l.core.LogAttrs(ctx, lvl, message, attrs...)
	return nil
}

// Time starts measuring an operation and returns a func suitable for defer.
func (l *Logger) Time(operation string) func() {
	return l.TimeContext(l.defaultContext(), operation)
}
func (l *Logger) TimeContext(ctx context.Context, operation string) func() {
	start := time.Now()
	return func() {
		l.WithField("duration", time.Since(start)).TimingContext(ctx, operation)
	}
}
