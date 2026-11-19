package prettylog

const (
	internalFormatKey        = "_log_format"
	internalPrettyMessageKey = "_pretty_message"
)

type RawMode string

const (
	RawDefault     RawMode = ""
	RawMessageMode RawMode = "raw_message"
	RawAllMode     RawMode = "raw_all"
)

// RawMessage skips control-character sanitization in messages only.
// Sensitive-field redaction remains enabled.
func RawMessage(log *Logger) *Logger { return log.WithField(internalFormatKey, string(RawMessageMode)) }

// RawAll skips sanitization in messages AND fields. Use only with trusted data.
// It never turns off masking/redaction, but ANSI injection becomes possible.
func RawAll(log *Logger) *Logger      { return log.WithField(internalFormatKey, string(RawAllMode)) }
func (l *Logger) RawMessage() *Logger { return RawMessage(l) }
func (l *Logger) RawAll() *Logger     { return RawAll(l) }
