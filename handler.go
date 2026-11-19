package prettylog

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// shared is deliberately shared by every WithField-derived logger.
type shared struct {
	mu         sync.RWMutex
	writeMu    sync.Mutex
	opts       Options
	byName     map[string]slog.Level
	byValue    map[slog.Level]LevelSpec
	levelWidth int
	min        slog.LevelVar
	exitFunc   func(int)
}

type handler struct {
	shared *shared
	fields []Field
	groups []string
}

var _ slog.Handler = (*handler)(nil)

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.shared.min.Level()
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	nh.fields = append([]Field(nil), h.fields...)
	for _, attr := range attrs {
		flattenAttr(strings.Join(h.groups, "."), attr, &nh.fields)
	}
	return &nh
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := *h
	nh.groups = append(append([]string(nil), h.groups...), name)
	return &nh
}

func flattenAttr(prefix string, attr slog.Attr, fields *[]Field) {
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Key == "" && attr.Value.Kind() != slog.KindGroup {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		path := prefix
		if attr.Key != "" {
			if path != "" {
				path += "."
			}
			path += attr.Key
		}
		for _, child := range attr.Value.Group() {
			flattenAttr(path, child, fields)
		}
		return
	}
	key := attr.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	*fields = append(*fields, Field{Key: key, Value: slogValue(attr.Value)})
}

func slogValue(v slog.Value) any {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindAny:
		return v.Any()
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration()
	case slog.KindTime:
		return v.Time()
	default:
		return v.Any()
	}
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	h.shared.mu.RLock()
	opts := h.shared.opts
	h.shared.mu.RUnlock()

	spec, found := h.shared.byValue[record.Level]
	if !found {
		spec = LevelSpec{Name: record.Level.String(), Value: record.Level, Emoji: "📋", Color: ColorGray}
	}
	fields := append([]Field(nil), h.fields...)
	record.Attrs(func(a slog.Attr) bool {
		flattenAttr(strings.Join(h.groups, "."), a, &fields)
		return true
	})
	// Merge duplicate field names with the last value taking precedence.
	merged := make(map[string]any, len(fields))
	for _, field := range fields {
		merged[field.Key] = field.Value
	}

	rawMode := RawDefault
	if raw, ok := merged[internalFormatKey]; ok {
		switch fmt.Sprint(raw) {
		case string(RawMessageMode):
			rawMode = RawMessageMode
		case string(RawAllMode):
			rawMode = RawAllMode
		}
		delete(merged, internalFormatKey)
	}
	prettyMessage := false
	if pretty, ok := merged[internalPrettyMessageKey]; ok {
		prettyMessage, _ = pretty.(bool)
		delete(merged, internalPrettyMessageKey)
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	safeFields := make([]Field, 0, len(keys))
	for _, key := range keys {
		safeFields = append(safeFields, Field{Key: key, Value: redactField(key, merged[key], opts.Mask)})
	}

	evt := Event{
		Time: record.Time.In(opts.Location), Level: record.Level,
		LevelName: spec.Name, Emoji: spec.Emoji, LevelColor: spec.Color,
		Message: record.Message, Fields: safeFields, RawMode: rawMode,
		PrettyMessage: prettyMessage,
	}
	layout := opts.TimeFormat
	if layout == "" {
		layout = "15:04:05"
		if opts.FullTimestamp {
			layout = "2006-01-02 15:04:05"
		}
	}
	fopts := FormatOptions{
		Location: opts.Location, TimestampLayout: layout,
		Colors:          colorEnabled(opts.Colors, opts.Output),
		LevelBackground: opts.LevelBackground,
		CenterLevel:     opts.CenterLevel,
		LevelWidth:      h.shared.levelWidth,
		Tags:            opts.Tags,
	}
	data, err := opts.Formatter.Format(evt, fopts)
	if err != nil {
		return err
	}
	h.shared.writeMu.Lock()
	defer h.shared.writeMu.Unlock()
	_, err = opts.Output.Write(data)
	return err
}
