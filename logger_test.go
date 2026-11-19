package prettylog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestLog(t *testing.T, opts Options) (*Logger, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	opts.Output = &out
	if opts.Colors == "" {
		opts.Colors = ColorNever
	}
	l, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return l, &out
}

func TestDefaultFormatMatchesSample(t *testing.T) {
	l, out := newTestLog(t, Options{Level: "TRACE"})
	l.WithField("module", "startup").WithFields(Fields{
		"tenant_id": 123, "address": ":8080", "environment": "develop",
	}).Info("HTTP server starting")
	s := out.String()
	for _, want := range []string{" | INFO    | ℹ️ HTTP server starting | [startup] [address::8080] [environment:develop] [tenant_id:123]"} {
		if !strings.Contains(s, want) {
			t.Fatalf("not found %q in %q", want, s)
		}
	}
	if strings.Contains(s, "_log_format") {
		t.Fatalf("internal field leaked: %q", s)
	}
}

func TestWithFieldsNoOutputOrMutation(t *testing.T) {
	l, out := newTestLog(t, Options{Level: "TRACE"})
	a := l.WithFields(Fields{"module": "A"})
	b := a.WithField("tenant_id", 123)
	if out.Len() != 0 {
		t.Fatal("WithField emitted a log")
	}
	b.Info("B")
	l.Info("base")
	s := out.String()
	if !strings.Contains(s, "[A]") || !strings.Contains(s, "[tenant_id:123]") {
		t.Fatal(s)
	}
	line := strings.Split(strings.TrimSpace(s), "\n")
	if len(line) != 2 {
		t.Fatalf("got %d records: %q", len(line), s)
	}
	if strings.Contains(line[1], "[A]") {
		t.Fatalf("parent mutated: %s", line[1])
	}
}

func TestCustomLevelsAndDynamicSetLevel(t *testing.T) {
	l, out := newTestLog(t, Options{Level: "TRACE", CustomLevels: []LevelSpec{{Name: "DATABASE", Value: -6, Color: ColorBlue, Emoji: "🗄️"}}})
	if err := l.Log("database", "SQL started"); err != nil {
		t.Fatal(err)
	}
	if err := l.Logf("DATABASE", "count=%d", 4); err != nil {
		t.Fatal(err)
	}
	l.Successf("reserved %d", 4)
	l.Timing("elapsed")
	if err := l.Log("NONEXISTENT", "bad"); err == nil {
		t.Fatal("expected error")
	}
	s := out.String()
	for _, want := range []string{"DATABASE", "🗄️ SQL started", "count=4", "SUCCESS", "TIMING"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q: %s", want, s)
		}
	}
	if err := l.SetLevel("ERROR"); err != nil {
		t.Fatal(err)
	}
	before := out.Len()
	l.Info("hidden")
	if out.Len() != before {
		t.Fatalf("INFO was not filtered: %s", out.String())
	}
	l.Error("visible")
	if !strings.Contains(out.String(), "visible") {
		t.Fatal(out.String())
	}
	if l.GetLevel() != "ERROR" {
		t.Fatal(l.GetLevel())
	}
}

func TestInvalidConfig(t *testing.T) {
	variants := []Options{
		{Timezone: "Not/A_Zone"},
		{Level: "NOT_A_LEVEL"},
		{Colors: "banana"},
		{Tags: TagOptions{Mode: "banana"}},
		{CustomLevels: []LevelSpec{{Name: "ERROR", Value: 20}}},
		{CustomLevels: []LevelSpec{{Name: "OTHER", Value: ErrorLevel}}},
		{Mask: MaskOptions{VisibleStart: -1}},
	}
	for i, opts := range variants {
		if _, err := New(opts); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestCustomTagsAndColors(t *testing.T) {
	l, out := newTestLog(t, Options{
		Colors: ColorAlways,
		Tags: TagOptions{Styles: map[string]TagStyle{
			"module":    {Color: ColorMagenta, ValueOnly: true, Priority: -100},
			"tenant_id": {Color: ColorCyan, Label: "tenant", Priority: -80},
			"hidden":    {Hidden: true},
		}},
	})
	l.WithFields(Fields{"module": "startup", "tenant_id": 123, "hidden": "nope"}).WithError(errors.New("connection timeout")).Error("Failed")
	got := out.String()
	for _, want := range []string{string(ColorMagenta) + "[startup]", string(ColorCyan) + "[tenant:123]", string(ColorRed) + "[error:connection timeout]", string(ColorRed) + "Failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "hidden") {
		t.Fatal(got)
	}
}

func TestTagsKeyValue(t *testing.T) {
	l, out := newTestLog(t, Options{Tags: TagOptions{Mode: TagsKeyValue}})
	l.WithField("module", "startup").Info("started")
	if !strings.Contains(out.String(), "module=startup") {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), "[startup]") {
		t.Fatal(out.String())
	}
}

func TestTimezone(t *testing.T) {
	l, out := newTestLog(t, Options{Timezone: "UTC", FullTimestamp: true})
	l.Info("clock")
	got := out.String()
	now := time.Now().UTC().Format("2006-01-02")
	if !strings.HasPrefix(got, now) {
		t.Fatalf("time not UTC date: %q", got)
	}
}

func TestSanitizeAndRawModes(t *testing.T) {
	l, out := newTestLog(t, Options{})
	input := "one\ntwo\t\x1b[31m"
	l.WithField("k", input).Info(input)
	s := out.String()
	if strings.Contains(s, "\x1b") {
		t.Fatalf("unsanitized ansi: %q", s)
	}
	if !strings.Contains(s, `one\ntwo\t\x1b`) {
		t.Fatalf("not escaped: %q", s)
	}
	out.Reset()
	RawMessage(l.WithField("k", input)).Info(input)
	s = out.String()
	if !strings.Contains(s, input) || !strings.Contains(s, `[k:one\ntwo\t\x1b`) {
		t.Fatalf("RawMessage: %q", s)
	}
	out.Reset()
	RawAll(l.WithField("k", input)).Info(input)
	s = out.String()
	if !strings.Contains(s, "[k:"+input+"]") {
		t.Fatalf("RawAll: %q", s)
	}
	if strings.Contains(s, "_log_format") {
		t.Fatal(s)
	}
}

func TestPrettyJSONAsMessageArgument(t *testing.T) {
	l, out := newTestLog(t, Options{})
	payload := map[string]any{
		"contact": map[string]any{"id": 123, "name": "Ana"},
		"status":  "ok",
	}
	l.WithField("module", "agent").Info("Got response", l.PrettyJSON(payload))

	got := out.String()
	want := "Got response\n{\n  \"contact\": {\n    \"id\": 123,\n    \"name\": \"Ana\"\n  },\n  \"status\": \"ok\"\n}"
	if !strings.Contains(got, want) {
		t.Fatalf("pretty JSON message not found in %q", got)
	}
	if strings.Contains(got, `Got response{\n`) || strings.Contains(got, internalPrettyMessageKey) {
		t.Fatalf("pretty JSON was escaped or internal field leaked: %q", got)
	}

	out.Reset()
	l.Info("unsafe\n\x1b[31m", l.PrettyJSON(payload))
	got = out.String()
	if strings.Contains(got, "\x1b") || !strings.Contains(got, `unsafe\n\x1b[31m`+"\n{") {
		t.Fatalf("non-JSON message content was not sanitized: %q", got)
	}
}

func TestMaskingAndShortSecrets(t *testing.T) {
	l, out := newTestLog(t, Options{})
	if got := l.Mask("30945622345").String(); got != "*******2345" {
		t.Fatal(got)
	}
	if got := l.MaskKeepLast("30945622345", 2).String(); got != "*********45" {
		t.Fatal(got)
	}
	if got := l.Mask("abc").String(); got != "***" {
		t.Fatalf("short secret visible: %s", got)
	}
	full, _ := New(Options{Output: out, Mask: MaskOptions{HideAll: true}, Colors: ColorNever})
	if got := full.Mask("abc123").String(); got != "******" {
		t.Fatal(got)
	}
	l.WithField("password", "abc").Info("login")
	if strings.Contains(out.String(), "abc") || !strings.Contains(out.String(), "[password:***]") {
		t.Fatal(out.String())
	}
	l.WithField("token", "sensitiveTOKEN").Info("secure")
	if strings.Contains(out.String(), "sensitiveTOKEN") {
		t.Fatal(out.String())
	}
}

func TestDisableAutoMaskingKeepsExplicitMasking(t *testing.T) {
	l, out := newTestLog(t, Options{DisableAutoMasking: true})
	l.WithField("password", "visible-secret").Info("automatic masking disabled")
	l.WithField("token", l.Mask("explicit-secret")).Info("explicit masking")

	got := out.String()
	if !strings.Contains(got, "[password:visible-secret]") {
		t.Fatalf("sensitive field was still automatically masked: %q", got)
	}
	if strings.Contains(got, "explicit-secret") || !strings.Contains(got, "[token:***********cret]") {
		t.Fatalf("explicit masking did not remain active: %q", got)
	}
}

func TestDisableAutoMaskingStillNormalizesErrors(t *testing.T) {
	l, out := newTestLog(t, Options{
		DisableAutoMasking: true,
		Formatter:          JSONFormatter{},
	})
	l.WithError(errors.New("connection failed")).Error("request failed")

	var record map[string]any
	if err := json.Unmarshal(out.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["error"] != "connection failed" {
		t.Fatalf("error was not normalized: %#v", record)
	}
}

func TestMaskJSONDeepRedaction(t *testing.T) {
	l, out := newTestLog(t, Options{Mask: MaskOptions{VisibleEnd: 4, Character: '*', SensitiveKeys: []string{"pin"}}, Formatter: JSONFormatter{}})
	secret := `{"token":"topsecret123", "profile":{"password":"abcdefg", "name":"Ana"},"items":[{"pin":"555555","id":2}]}`
	l.WithField("payload", l.MaskJSON(secret)).Info("payload")
	if strings.Contains(out.String(), "topsecret123") || strings.Contains(out.String(), "abcdefg") || strings.Contains(out.String(), "555555") {
		t.Fatal(out.String())
	}
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	payload, ok := obj["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is not structured: %#v", obj)
	}
	if payload["token"] != "********t123" {
		t.Fatalf("redaction: %#v", payload)
	}
	if payload["profile"].(map[string]any)["name"] != "Ana" {
		t.Fatal(payload)
	}
}

func TestJSONFormatterAndKeyCollision(t *testing.T) {
	l, out := newTestLog(t, Options{Formatter: JSONFormatter{}, Timezone: "America/Bogota"})
	l.WithFields(Fields{"level": "overridden", "msg": "custom", "module": "agent"}).Info("actual")
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	if obj["level"] != "INFO" || obj["msg"] != "actual" || obj["field.level"] != "overridden" || obj["field.msg"] != "custom" {
		t.Fatal(obj)
	}
	if !strings.Contains(obj["time"].(string), "-05:00") {
		t.Fatal(obj)
	}
}

func TestSetFormatterPropagates(t *testing.T) {
	l, out := newTestLog(t, Options{})
	child := l.WithField("module", "agent")
	l.SetFormatter(JSONFormatter{})
	child.Info("works")
	var obj map[string]any
	if err := json.Unmarshal(out.Bytes(), &obj); err != nil {
		t.Fatalf("not JSON: %s", out.String())
	}
	if obj["module"] != "agent" {
		t.Fatal(obj)
	}
}

func TestLogrusMethodFamilies(t *testing.T) {
	l, out := newTestLog(t, Options{Level: "TRACE"})
	l.Tracef("T%d", 1)
	l.Traceln("T", 2)
	l.Debugf("D%d", 1)
	l.Debugln("D", 2)
	l.Infof("I%d", 1)
	l.Infoln("I", 2)
	l.Successf("S%d", 1)
	l.Successln("S", 2)
	l.Timingf("P%d", 1)
	l.Timingln("P", 2)
	l.Warnf("W%d", 1)
	l.Warnln("W", 2)
	l.Warning("warning")
	l.Warningf("warning %d", 2)
	l.Errorf("E%d", 1)
	l.Errorln("E", 2)
	if strings.Count(out.String(), "\n") != 16 {
		t.Fatalf("method count: %q", out.String())
	}
}

func TestTimingDefer(t *testing.T) {
	l, out := newTestLog(t, Options{})
	func() { defer l.Time("process")(); time.Sleep(time.Millisecond) }()
	s := out.String()
	if !strings.Contains(s, "TIMING") || !strings.Contains(s, "[duration:") {
		t.Fatal(s)
	}
}

func TestFatalAndPanic(t *testing.T) {
	exitCode := 0
	l, out := newTestLog(t, Options{ExitFunc: func(code int) { exitCode = code }})
	l.Fatalf("fatal %d", 1)
	if exitCode != 1 || !strings.Contains(out.String(), "FATAL") {
		t.Fatal(exitCode, out.String())
	}
	func() {
		defer func() {
			if value := recover(); value == nil || value != "panic 42" {
				t.Errorf("wrong panic: %v", value)
			}
		}()
		l.Panicf("panic %d", 42)
	}()
	if !strings.Contains(out.String(), "PANIC") {
		t.Fatal(out.String())
	}
}

func TestTintFormatterInvoked(t *testing.T) {
	l, out := newTestLog(t, Options{Formatter: TintFormatter{}})
	l.WithField("module", "startup").Success("done")
	if !strings.Contains(out.String(), "done") {
		t.Fatal(out.String())
	}
}

func TestConcurrentLoggers(t *testing.T) {
	// Avoid bytes.Buffer here, since it isn't concurrency-safe even for reads in tests.
	var mu sync.Mutex
	counter := 0
	w := &countWriter{mu: &mu, count: &counter}
	l := MustNew(Options{Output: w, Formatter: ModernEmojiFormatter{}, Colors: ColorNever})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			child := l.WithField("tenant_id", i)
			for k := 0; k < 20; k++ {
				child.Infof("record %d", k)
			}
		}(i)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if counter != 400 {
		t.Fatalf("got %d writes, want 400", counter)
	}
}

type countWriter struct {
	mu    *sync.Mutex
	count *int
}

func (w *countWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	*w.count++
	w.mu.Unlock()
	return len(data), nil
}

func TestWithSlog(t *testing.T) {
	l, out := newTestLog(t, Options{})
	l.WithField("module", "grpc").Slog().InfoContext(context.Background(), "slog compatible", "count", 1)
	if !strings.Contains(out.String(), "[grpc]") || !strings.Contains(out.String(), "[count:1]") {
		t.Fatal(out.String())
	}
}
func ExampleLogger_WithFields() {
	var b bytes.Buffer
	log := MustNew(Options{Output: &b, Colors: ColorNever})
	log.WithFields(Fields{"module": "agent", "tenant_id": 123}).Success("ready")
	fmt.Println(strings.Contains(b.String(), "[agent]"))
	// Output: true
}

func TestWithContextPreserved(t *testing.T) {
	l, out := newTestLog(t, Options{})
	bound := l.WithContext(context.WithValue(context.Background(), "context-key", "value")).WithField("module", "startup")
	if bound.ctx == nil || bound.ctx.Value("context-key") != "value" {
		t.Fatal("bound context not preserved")
	}
	bound.Info("start")
	if !strings.Contains(out.String(), "[startup]") {
		t.Fatal(out.String())
	}
}
