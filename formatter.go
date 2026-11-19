package prettylog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lmittmann/tint"
)

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
)

func colorEnabled(mode ColorMode, output io.Writer) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
			return false
		}
		if os.Getenv("TERM") == "dumb" {
			return false
		}
		f, ok := output.(*os.File)
		if !ok {
			return false
		}
		info, err := f.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	}
}
func paint(s string, c Color, enabled bool) string {
	if !enabled || c == "" {
		return s
	}
	return string(c) + s + ansiReset
}

// paintLevel optionally renders a compact badge using muted true-color
// backgrounds similar to modern terminal UIs.
func paintLevel(s string, c Color, enabled, background bool) string {
	if !enabled {
		return s
	}
	if !background {
		return paint(s, c, true)
	}
	var style string
	switch c {
	case ColorGray:
		style = "\x1b[1;38;2;203;213;225;48;2;47;55;69m"
	case ColorWhite:
		style = "\x1b[1;38;2;241;245;249;48;2;71;85;105m"
	case ColorGreen:
		style = "\x1b[1;38;2;74;222;128;48;2;20;63;49m"
	case ColorBlue:
		style = "\x1b[1;38;2;96;165;250;48;2;25;48;83m"
	case ColorYellow:
		style = "\x1b[1;38;2;250;204;21;48;2;66;55;25m"
	case ColorRed:
		style = "\x1b[1;38;2;248;113;113;48;2;69;35;43m"
	case ColorMagenta:
		style = "\x1b[1;38;2;216;180;254;48;2;64;40;82m"
	case ColorCyan:
		style = "\x1b[1;38;2;103;232;249;48;2;22;61;70m"
	default:
		// Preserve custom ANSI foreground colors when there is no safe
		// foreground-to-background mapping.
		style = ansiBold + string(c)
	}
	return style + s + ansiReset
}

func levelPadding(s string, width int, centered bool) (left, right string) {
	missing := width - utf8.RuneCountInString(s)
	if missing <= 0 {
		return "", ""
	}
	leftWidth := 0
	if centered {
		leftWidth = missing / 2
	}
	return strings.Repeat(" ", leftWidth), strings.Repeat(" ", missing-leftWidth)
}

// SanitizeControl makes newlines, tabs, ESC and other control characters visible.
func SanitizeControl(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		case '\x1b':
			out.WriteString(`\x1b`)
		default:
			if r < 32 || r == 127 {
				fmt.Fprintf(&out, `\x%02X`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

func rawFlags(mode RawMode) (sanitizeMessage, sanitizeTags bool) {
	switch mode {
	case RawAllMode:
		return false, false
	case RawMessageMode:
		return false, true
	default:
		return true, true
	}
}
func fieldString(value any) string {
	if value == nil {
		return "<nil>"
	}
	switch v := value.(type) {
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(value)
	}
}

func isErrorKey(key string) bool {
	key = strings.ToLower(key)
	return key == "err" || key == "error" || strings.HasSuffix(key, ".err") || strings.HasSuffix(key, ".error")
}

func sortFields(fields []Field, styles map[string]TagStyle) []Field {
	result := append([]Field(nil), fields...)
	sort.SliceStable(result, func(i, j int) bool {
		a, b := styles[result[i].Key].Priority, styles[result[j].Key].Priority
		if a != b {
			return a < b
		}
		return result[i].Key < result[j].Key
	})
	return result
}

// ModernEmojiFormatter reproduces the user's Logrus console layout by default.
func (ModernEmojiFormatter) Format(event Event, cfg FormatOptions) ([]byte, error) {
	var b strings.Builder
	sanitizeMessage, sanitizeTags := rawFlags(event.RawMode)
	b.WriteString(paint(event.Time.Format(cfg.TimestampLayout), ColorGray, cfg.Colors))
	b.WriteString(" |")
	levelWidth := cfg.LevelWidth
	if levelWidth < utf8.RuneCountInString(event.LevelName) {
		levelWidth = utf8.RuneCountInString(event.LevelName)
	}
	left, right := levelPadding(event.LevelName, levelWidth, cfg.CenterLevel)
	b.WriteByte(' ')
	b.WriteString(left)
	b.WriteString(paintLevel(event.LevelName, event.LevelColor, cfg.Colors, cfg.LevelBackground))
	b.WriteString(right)
	b.WriteByte(' ')
	b.WriteString("| ")
	b.WriteString(event.Emoji)
	b.WriteByte(' ')
	msg := event.Message
	if sanitizeMessage && !event.PrettyMessage {
		msg = SanitizeControl(msg)
	}
	if cfg.Colors {
		msgColor := ColorWhite
		if event.Level >= ErrorLevel {
			msgColor = ColorRed
		}
		b.WriteString(ansiBold)
		b.WriteString(string(msgColor))
		b.WriteString(msg)
		b.WriteString(ansiReset)
	} else {
		b.WriteString(msg)
	}

	ordered := sortFields(event.Fields, cfg.Tags.Styles)
	anyTags := false
	for _, field := range ordered {
		if !cfg.Tags.Styles[field.Key].Hidden {
			anyTags = true
			break
		}
	}
	if anyTags {
		b.WriteString(" |")
	}
	for _, field := range ordered {
		style := cfg.Tags.Styles[field.Key]
		if style.Hidden {
			continue
		}
		k, v := field.Key, fieldString(field.Value)
		prettyJSON, isPrettyJSON := getPrettyJSONText(field.Value)
		if isPrettyJSON {
			v = prettyJSON
		}
		if style.Label != "" {
			k = style.Label
		}
		if sanitizeTags {
			k = SanitizeControl(k)
			// PrettyJSONValue is produced by encoding/json: its indentation
			// newlines are safe and JSON control characters remain escaped.
			if !isPrettyJSON {
				v = SanitizeControl(v)
			}
		}
		var content string
		if isPrettyJSON && cfg.Tags.Mode == TagsKeyValue {
			content = k + "=\n" + v
		} else if isPrettyJSON && style.ValueOnly {
			content = "[\n" + v + "\n]"
		} else if isPrettyJSON {
			content = "[" + k + ":\n" + v + "\n]"
		} else if cfg.Tags.Mode == TagsKeyValue {
			content = k + "=" + v
		} else if style.ValueOnly {
			content = "[" + v + "]"
		} else {
			content = "[" + k + ":" + v + "]"
		}
		tagColor := style.Color
		if tagColor == "" {
			tagColor = cfg.Tags.DefaultColor
		}
		if isErrorKey(field.Key) {
			tagColor = ColorRed
		}
		b.WriteByte(' ')
		b.WriteString(paint(content, tagColor, cfg.Colors))
	}
	b.WriteByte('\n')
	return []byte(b.String()), nil
}

// JSONFormatter emits valid single-line structured JSON, without ANSI escapes.
// The original key names are preserved; console-only tag styles do not affect JSON.
func (JSONFormatter) Format(event Event, cfg FormatOptions) ([]byte, error) {
	obj := map[string]any{
		"time":  event.Time.Format("2006-01-02T15:04:05.999999999-07:00"),
		"level": event.LevelName,
		"msg":   event.Message,
	}
	for _, field := range event.Fields {
		// Avoid overwriting the reserved metadata keys.
		switch field.Key {
		case "time", "level", "msg":
			obj["field."+field.Key] = field.Value
		default:
			obj[field.Key] = field.Value
		}
	}
	return appendJSONLine(obj)
}
func appendJSONLine(obj any) ([]byte, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// TintFormatter delegates rendering to lmittmann/tint. Its output is tint's own
// layout, not the custom pipe/emoji ModernEmojiFormatter layout.
func (TintFormatter) Format(event Event, cfg FormatOptions) ([]byte, error) {
	var buf bytes.Buffer
	// Set the low threshold so our custom levels are not filtered twice.
	h := tint.NewTextHandler(&buf, &tint.Options{
		Level:      slog.Level(-1 << 25),
		TimeFormat: cfg.TimestampLayout,
		NoColor:    !cfg.Colors,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.LevelKey {
				a = slog.String(a.Key, event.LevelName)
				if cfg.Colors {
					return tint.Attr(ansiColorNumber(event.LevelColor), a)
				}
			}
			if a.Key == slog.MessageKey && event.Level >= ErrorLevel && cfg.Colors {
				return tint.Attr(9, a)
			}
			if cfg.Colors {
				style := cfg.Tags.Styles[a.Key]
				if isErrorKey(a.Key) {
					return tint.Attr(9, a)
				}
				if style.Color != "" {
					return tint.Attr(ansiColorNumber(style.Color), a)
				}
			}
			return a
		},
	})
	record := slog.NewRecord(event.Time, event.Level, event.Message, 0)
	sm, st := rawFlags(event.RawMode)
	if sm && !event.PrettyMessage {
		record.Message = SanitizeControl(record.Message)
	}
	for _, f := range event.Fields {
		if cfg.Tags.Styles[f.Key].Hidden {
			continue
		}
		key, value := f.Key, fieldString(f.Value)
		if st {
			key, value = SanitizeControl(key), SanitizeControl(value)
		}
		record.AddAttrs(slog.String(key, value))
	}
	if err := h.Handle(nil, record); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// tint.Attr uses ANSI color numbers (8-15 for bright foreground colors).
func ansiColorNumber(c Color) uint8 {
	switch c {
	case ColorWhite:
		return 15
	case ColorGreen:
		return 10
	case ColorBlue:
		return 12
	case ColorYellow:
		return 11
	case ColorRed:
		return 9
	case ColorMagenta:
		return 13
	case ColorCyan:
		return 14
	default:
		return 8
	}
}
