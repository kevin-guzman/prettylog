// Package prettylog provides a Logrus-like fluent API backed by log/slog.
// It includes a compatible-looking emoji console formatter, tint and JSON.
package prettylog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Severity increases with the numeric value (slog semantics).
const (
	TraceLevel   slog.Level = -8
	DebugLevel   slog.Level = -4
	InfoLevel    slog.Level = 0
	SuccessLevel slog.Level = 1
	TimingLevel  slog.Level = 2
	WarnLevel    slog.Level = 4
	ErrorLevel   slog.Level = 8
	FatalLevel   slog.Level = 12
	PanicLevel   slog.Level = 16
)

type Fields map[string]any

type Color string

const (
	ColorGray    Color = "\x1b[90m"
	ColorWhite   Color = "\x1b[97m"
	ColorGreen   Color = "\x1b[92m"
	ColorBlue    Color = "\x1b[94m"
	ColorYellow  Color = "\x1b[93m"
	ColorRed     Color = "\x1b[91m"
	ColorMagenta Color = "\x1b[95m"
	ColorCyan    Color = "\x1b[96m"
)

// ColorMode sets ANSI output policy.
type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

// TagsMode selects the rendering of structured fields in console output.
type TagsMode string

const (
	TagsBrackets TagsMode = "brackets"  // [startup] [tenant_id:123]
	TagsKeyValue TagsMode = "key_value" // module=startup tenant_id=123
)

// TagStyle controls a specific structured key's presentation.
// Priority is ascending; module defaults to -100 and other keys to 0.
type TagStyle struct {
	Label     string
	Color     Color
	ValueOnly bool
	Priority  int
	Hidden    bool
}

type TagOptions struct {
	Mode         TagsMode
	Styles       map[string]TagStyle
	DefaultColor Color
}

// LevelSpec defines a level with a unique case-insensitive name and unique value.
// Custom levels are logged using Log, Logf, LogContext, or LogAttrs.
type LevelSpec struct {
	Name  string
	Value slog.Level
	Emoji string
	Color Color
}

// MaskOptions controls the default obfuscation algorithm.
// SensitiveKeys is an extra list; default sensitive keys remain enabled.
type MaskOptions struct {
	VisibleStart  int
	VisibleEnd    int
	HideAll       bool // masks all characters when counts are zero
	Character     rune
	SensitiveKeys []string
}

// Options holds configuration used by the entire logger family (including WithField children).
type Options struct {
	Output        io.Writer
	Level         string // defaults to TRACE
	CustomLevels  []LevelSpec
	Timezone      string         // IANA name, e.g. America/Bogota; default COT (-05:00)
	Location      *time.Location // takes precedence over Timezone
	FullTimestamp bool
	TimeFormat    string // overrides FullTimestamp
	Colors        ColorMode
	// LevelBackground renders the level as a subtle colored badge when ANSI
	// colors are enabled. When false, the level uses colored text only.
	LevelBackground bool
	// CenterLevel centers the level name within the width reserved for all
	// configured levels. When false, level names are left-aligned.
	CenterLevel bool
	Tags        TagOptions
	Mask        MaskOptions
	Formatter   Formatter // nil selects ModernEmojiFormatter
	ExitFunc    func(int) // defaults to os.Exit; useful for tests
}

// Event is a safe, normalized and redacted snapshot passed to formatters.
type Event struct {
	Time       time.Time
	Level      slog.Level
	LevelName  string
	Emoji      string
	LevelColor Color
	Message    string
	Fields     []Field
	RawMode    RawMode
	// PrettyMessage marks a message assembled from PrettyJSON values. Its JSON
	// indentation newlines are already safe and must remain visible.
	PrettyMessage bool
}

type Field struct {
	Key   string
	Value any
}

// FormatOptions is an immutable-by-convention snapshot provided to formatters.
type FormatOptions struct {
	Location        *time.Location
	TimestampLayout string
	Colors          bool
	LevelBackground bool
	CenterLevel     bool
	// LevelWidth is the display width reserved for the longest configured level.
	// Built-in formatters use it to keep every message in the same column.
	LevelWidth int
	Tags       TagOptions
}

// Formatter lets callers provide custom serialization (like logrus.SetFormatter).
// Format must not mutate its inputs and should append a trailing newline.
type Formatter interface {
	Format(Event, FormatOptions) ([]byte, error)
}

// Existing formatters. The zero value is usable.
type ModernEmojiFormatter struct{}
type TintFormatter struct{}
type JSONFormatter struct{}

// New validates configuration instead of silently accepting an invalid timezone/level.
func New(opts Options) (*Logger, error) {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}
	if opts.Level == "" {
		opts.Level = "TRACE"
	}
	if opts.Colors == "" {
		opts.Colors = ColorAuto
	}
	if opts.Colors != ColorAuto && opts.Colors != ColorAlways && opts.Colors != ColorNever {
		return nil, fmt.Errorf("unknown color mode %q", opts.Colors)
	}
	if opts.Location == nil {
		if opts.Timezone == "" {
			opts.Location = time.FixedZone("COT", -5*60*60)
		} else {
			loc, err := time.LoadLocation(opts.Timezone)
			if err != nil {
				return nil, fmt.Errorf("timezone %q: %w", opts.Timezone, err)
			}
			opts.Location = loc
		}
	}
	if opts.Tags.Mode == "" {
		opts.Tags.Mode = TagsBrackets
	}
	if opts.Tags.Mode != TagsBrackets && opts.Tags.Mode != TagsKeyValue {
		return nil, fmt.Errorf("unknown tags mode %q", opts.Tags.Mode)
	}
	if opts.Tags.DefaultColor == "" {
		opts.Tags.DefaultColor = ColorGray
	}
	tagStyles := map[string]TagStyle{
		"module": {Color: ColorMagenta, ValueOnly: true, Priority: -100},
	}
	for key, style := range opts.Tags.Styles {
		tagStyles[key] = style
	}
	opts.Tags.Styles = tagStyles
	if opts.Mask.VisibleStart < 0 || opts.Mask.VisibleEnd < 0 {
		return nil, fmt.Errorf("mask character counts must be >= 0")
	}
	opts.Mask.SensitiveKeys = append([]string(nil), opts.Mask.SensitiveKeys...)
	if opts.Mask.Character == 0 {
		opts.Mask.Character = '*'
	}
	if !opts.Mask.HideAll && opts.Mask.VisibleStart == 0 && opts.Mask.VisibleEnd == 0 {
		opts.Mask.VisibleEnd = 4
	}
	if opts.Formatter == nil {
		opts.Formatter = ModernEmojiFormatter{}
	}
	if opts.ExitFunc == nil {
		opts.ExitFunc = os.Exit
	}

	byName := make(map[string]slog.Level)
	byValue := make(map[slog.Level]LevelSpec)
	levelWidth := 0
	defaults := []LevelSpec{
		{"TRACE", TraceLevel, "🔬", ColorCyan},
		{"DEBUG", DebugLevel, "🔍", ColorBlue},
		{"INFO", InfoLevel, "ℹ️", ColorGreen},
		{"SUCCESS", SuccessLevel, "✅", ColorGreen},
		{"TIMING", TimingLevel, "🕒", ColorCyan},
		{"WARN", WarnLevel, "⚠️", ColorYellow},
		{"ERROR", ErrorLevel, "❌", ColorRed},
		{"FATAL", FatalLevel, "💀", ColorRed},
		{"PANIC", PanicLevel, "🚨", ColorRed},
	}
	for _, level := range append(defaults, opts.CustomLevels...) {
		name := strings.ToUpper(strings.TrimSpace(level.Name))
		if name == "" {
			return nil, fmt.Errorf("level name cannot be empty")
		}
		if _, ok := byName[name]; ok {
			return nil, fmt.Errorf("duplicate level name %q", name)
		}
		if previous, ok := byValue[level.Value]; ok {
			return nil, fmt.Errorf("level %q value %d conflicts with %q", name, level.Value, previous.Name)
		}
		level.Name = name
		if level.Emoji == "" {
			level.Emoji = "📋"
		}
		if level.Color == "" {
			level.Color = ColorGray
		}
		byName[name] = level.Value
		byValue[level.Value] = level
		if width := utf8.RuneCountInString(name); width > levelWidth {
			levelWidth = width
		}
	}
	min, ok := byName[strings.ToUpper(strings.TrimSpace(opts.Level))]
	if !ok {
		return nil, fmt.Errorf("unknown configured log level %q", opts.Level)
	}
	core := &shared{
		opts:       opts,
		byName:     byName,
		byValue:    byValue,
		levelWidth: levelWidth,
		exitFunc:   opts.ExitFunc,
	}
	core.min.Set(min)
	h := &handler{shared: core}
	return &Logger{core: slog.New(h), shared: core}, nil
}

func MustNew(opts Options) *Logger {
	l, err := New(opts)
	if err != nil {
		panic(err)
	}
	return l
}
