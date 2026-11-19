# ignite-prettylog

A Go logger with a fluent Logrus-style API backed by **log/slog**, with
**ModernEmoji** (default), **tint**, and **JSON** formatters. Requires Go
**1.27.4**.

> The module name `example.com/ignite-prettylog` is a local placeholder, not a
> published URL. Replace it with the real repository path before publishing.

## Install and run

```bash
# Requires Go >= 1.27.4
go mod download
go run ./examples/basic
go test ./...
go test -race ./...
```

The development formatter produces compact, aligned output. The level width is
calculated automatically from every configured level, including custom levels,
so the emoji and message always start in the same column:

```text
08:34:30 |  INFO   | ℹ️ HTTP server starting | [startup] [address::8080] [tenant:123]
08:34:31 |  ERROR  | ❌ Delivery failed | [agent] [tenant:123] [error:connection timeout]
08:34:32 | TIMING  | 🕒 Agent processing | [agent] [tenant:123] [duration:152ms]
```

With `LevelBackground=true`, a subtle dark background covers only the level
text; alignment spaces remain transparent. The default value is `false`, which
uses colored text without a background. `CenterLevel=true` centers each name in
the reserved level column; the default `false` left-aligns it. Messages at
`ERROR`, `FATAL`, and `PANIC`, as well as `error`/`err` fields, are rendered in
red. `ColorAuto` enables ANSI output in interactive terminals and respects
`NO_COLOR`.

## Create a logger

```go
log := prettylog.MustNew(prettylog.Options{
    Level:           "TRACE",
    Timezone:        "America/Bogota", // Location: *time.Location is also supported
    Colors:          prettylog.ColorAuto,
    LevelBackground: true,
    CenterLevel:     true,
    FullTimestamp:   false,
    Tags: prettylog.TagOptions{
        Mode: prettylog.TagsBrackets,
        Styles: map[string]prettylog.TagStyle{
            "module":    {ValueOnly: true, Priority: -100, Color: prettylog.ColorMagenta},
            "tenant_id": {Label: "tenant", Priority: -90, Color: prettylog.ColorCyan},
        },
    },
    CustomLevels: []prettylog.LevelSpec{
        {Name: "DATABASE", Value: -6, Color: prettylog.ColorBlue, Emoji: "🗄️"},
    },
})
```

To emulate a terminal without colors from the command line without changing
the code:

```bash
NO_COLOR=1 go run ./examples/basic
```

You can also emulate a basic terminal:

```bash
TERM=dumb go run ./examples/basic
```

Both commands require `Colors: prettylog.ColorAuto`, as shown above.
`ColorAlways` deliberately forces ANSI sequences regardless of these settings.

## Logrus-style API

```go
log.Info("Server started")
log.Infof("Listening on %s", ":8080")
log.Debug("Debug details")
log.Debugf("Attempt: %d", 2)
log.Success("Operation completed")
log.Successf("Created %d records", 2)
log.Timing("Processed in 10ms")
log.Timingf("Elapsed: %dms", 42)
log.Warn("Warning")
log.Error("Failure")
log.Errorf("Failure: %v", err)
log.WithError(err).Error("Delivery failed")
log.WithField("module", "startup").Info("Started")
log.WithFields(prettylog.Fields{"module": "agent", "tenant_id": 123}).Success("Ready")
log.JSON("API response", payload)
log.JSONf("Response from %s", payload, "WhatsApp")
log.WithField("response", log.PrettyJSON(payload)).Info("Response received")
_ = log.Log("DATABASE", "Query executed")
_ = log.Logf("DATABASE", "Rows: %d", 3)
_ = log.SetLevel("INFO")
```

### Readable JSON in console output

`JSON` and `JSONf` log an indented `json` field at `INFO` level. The console
formatter displays it across multiple lines, while `JSONFormatter` preserves it
as a real object or array rather than an escaped string:

```go
log.JSON("API response", payload)
log.JSONf("Response from %s", payload, "WhatsApp")

// PrettyJSON also preserves indentation when used as a message argument.
log.Info("Got response", log.PrettyJSON(payload))

log.WithField("response", log.PrettyJSON(payload)).
    Info("Response received")
```

The `JSONf` signature is `JSONf(format, payload, args...)`, so the payload comes
before `"WhatsApp"`. `PrettyJSON` accepts structs, maps, slices, JSON strings,
`[]byte`, and `json.RawMessage`. Combine it with recursive masking when the
payload contains secrets:

```go
log.WithField("response", log.PrettyJSON(log.MaskJSON(payload))).
    Info("Response received")
```

Methods ending in `*f` and `*ln`, `Warning`/`Warningf`/`Warningln`,
`Print`/`Printf`/`Println`, `WithContext`, `WithField`, `WithFields`,
`WithError`, `Fatal`, and `Panic` follow Logrus conventions.
`Info("msg", "key", value)` does **not** add an attribute, matching Logrus;
use `WithField(s)` or `LogAttrs` instead.

An intentional difference from Logrus is that `WithField(s)` returns a new
`*Logger`, not an `*Entry`, and does not emit anything until a logging method is
called. Keep the derived logger on your service:

```go
type Service struct { log *prettylog.Logger }

func NewService(base *prettylog.Logger, tenantID string) *Service {
    return &Service{log: base.WithFields(prettylog.Fields{
        "module": "agent", "tenant_id": tenantID,
    })}
}
```

For services shared across tenants, add `tenant_id` per request instead of
storing it in the constructor.

## Level values

| Level | Value | Method |
| --- | ---: | --- |
| TRACE | -8 | Trace / Tracef / Traceln |
| DEBUG | -4 | Debug / Debugf / Debugln |
| INFO | 0 | Info / Infof / Infoln |
| SUCCESS | 1 | Success / Successf / Successln |
| TIMING | 2 | Timing / Timingf / Timingln |
| WARN | 4 | Warn / Warnf / Warnln |
| ERROR | 8 | Error / Errorf / Errorln |
| FATAL | 12 | Fatal / Fatalf / Fatalln (exits with status 1) |
| PANIC | 16 | Panic / Panicf / Panicln (panics) |

Register custom levels with `CustomLevels: []LevelSpec{...}` and emit them with
`Log("DATABASE", ...)`, `Logf`, or `LogAttrs`. Names and values must be unique
and are validated during construction. Levels use numeric slog threshold
semantics (`event level >= configured minimum`). A custom level below `TRACE`
must itself be selected as the minimum to become visible. Go methods are not
generated dynamically for custom names.

## Formatters

```go
log.SetFormatter(prettylog.ModernEmojiFormatter{}) // default
log.SetFormatter(prettylog.TintFormatter{})        // delegates to tint
log.SetFormatter(prettylog.JSONFormatter{})        // one JSON event per line, no ANSI
```

You can implement `Formatter.Format(Event, FormatOptions) ([]byte, error)` for a
custom format. `SetFormatter` and `SetLevel` affect child loggers that have
already been created. Configure the time zone with an IANA name such as
`America/Bogota` or `UTC`, and configure timestamps with `TimeFormat` or
`FullTimestamp`.

### Tags

- `module` is rendered first and as `[startup]` in magenta by default.
- Other fields are sorted by `Priority` and name and rendered as `[key:value]`.
- `TagStyle.Label` renames a key in text output, `ValueOnly` hides the key,
  `Color` assigns an ANSI color, and `Hidden` omits the field from text/tint.
- `TagsMode=TagsKeyValue` renders `module=startup tenant_id=123`.
- JSON output preserves original key names and ignores visual styles. Sensitive
  fields remain masked.

## Data protection

```go
log.WithField("phone", log.Mask("30945622345")).Info("Phone")
// *******2345

log.WithField("phone", log.MaskKeepLast("30945622345", 2)).Info("Phone")
// *********45

log.WithField("payload", log.MaskJSON(`{
  "access_token":"12345678",
  "user":{"name":"Ana","password":"1234abcd"}
}`)).Info("Payload")

log.WithField("password", "supersecret").Info("Automatically masked")
```

Configure `Options.Mask.VisibleStart`, `.VisibleEnd`, `.Character` (the default
keeps four trailing characters visible), `.HideAll`, and `.SensitiveKeys`.
Nested JSON objects and arrays are traversed recursively. `MaskJSON` preserves
structure under `JSONFormatter`. Secrets with four characters or fewer are
fully masked. Wrappers cannot prevent leaks when sensitive data is written
directly into the message; use structured fields instead.

`SanitizeControl` escapes newlines, tabs, ESC, and other control characters by
default:

```go
log.Info("Hello\nWorld")                       // renders the newline as \n
prettylog.RawMessage(log).Info("Hello\nWorld") // real message newline, sanitized tags
prettylog.RawAll(log).Info("Hello\nWorld")     // no message or tag sanitization
```

Use `RawAll` only with trusted input because it may allow terminal escape
sequence injection. Neither `RawMessage` nor `RawAll` disables masking.

## Framework integration

`log.Slog()` exposes a `*slog.Logger` for components that accept the standard
API, including Gin, Uber FX, and adapters. Avoid `slog.SetDefault` unless you
intend to replace the process-wide logger.

## Behavior and limitations

- `Fatal` logs and calls `os.Exit(1)`, configurable through `ExitFunc` for tests.
  As with Logrus, immediate process exit skips deferred functions.
- `Panic` logs and then panics.
- `Timing` is a log level, not a profiler. Use
  `defer log.Time("operation")()` to record elapsed time.
- Custom levels control severity and cannot be filtered independently by name
  when they share a threshold.
- Automatic TTY detection uses `os.File.Stat()`; force ANSI with `ColorAlways`.
- JSON event fields named `time`, `level`, or `msg` are renamed to
  `field.time`, `field.level`, and `field.msg` to avoid metadata collisions.
- `TintFormatter` uses tint's layout rather than the custom pipe-separated one.
- Custom formatters shared across loggers must be safe for concurrent use.
