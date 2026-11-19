package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	logger "github.com/kevin-guzman/prettylog"
)

type AgentService struct{ log *logger.Logger }

func NewAgentService(base *logger.Logger, tenantID int) *AgentService {
	return &AgentService{log: base.WithFields(logger.Fields{
		"module": "agent", "tenant_id": tenantID,
	})}
}
func (s *AgentService) Process(ctx context.Context) {
	log := s.log.WithField("request_id", "req-001")
	defer log.TimeContext(ctx, "Processing complete")()
	log.Trace("Entering agent")
	log.Debugf("Tenant: %d", 123)
	log.Info("Message received")
	log.Success("Reservation created")
	log.Timingf("Tokens consumed: %d", 542)
	_ = log.Log("DATABASE", "Connecting to PostgreSQL")
	log.WithField("phone", log.MaskKeepLast("30945622345", 4)).Info("Customer found")
	log.WithField("payload", log.MaskJSON(map[string]any{
		"contact":  map[string]any{"name": "Ana", "token": "a1b2c3d4e5", "password": "topsecret123"},
		"products": []any{map[string]any{"id": 1, "api_key": "TOKEN-12345678"}},
	})).Info("Secure payload")
	log.WithFields(logger.Fields{"password": "mysecret!", "duration": 42 * time.Millisecond}).Info("Protected data")

	apiResponse := map[string]any{
		"status":   "ok",
		"contact":  map[string]any{"id": 123, "name": "Ana"},
		"messages": []string{"created", "delivered"},
	}
	log.JSON("API response", apiResponse)
	log.JSONf("Response from %s", apiResponse, "WhatsApp")
	log.Info("Got response", log.PrettyJSON(apiResponse))
	log.WithField("response", log.PrettyJSON(apiResponse)).
		Info("Response received")
	log.WithField("response", log.PrettyJSON(log.MaskJSON(map[string]any{
		"name":  "Ana",
		"token": "topsecret123",
	}))).Info("Response received with protected data")

	log.WithError(errors.New("connection timeout")).Error("Delivery failed")
	logger.RawMessage(log).Info("Multiline response:\nHello")
	log.Info("Sanitized response:\nHello")
}

func main() {
	root := logger.MustNew(logger.Options{
		Level:           "TRACE",
		Timezone:        "America/Bogota",
		Colors:          logger.ColorAuto,
		LevelBackground: true,
		CenterLevel:     true,
		Tags: logger.TagOptions{
			Mode: logger.TagsBrackets,
			Styles: map[string]logger.TagStyle{
				"module":     {Color: logger.ColorMagenta, ValueOnly: true, Priority: -100},
				"tenant_id":  {Color: logger.ColorCyan, Label: "tenant", Priority: -90},
				"request_id": {Color: logger.ColorBlue, Priority: -80},
				"duration":   {Color: logger.ColorYellow},
			},
		},
		Mask:         logger.MaskOptions{VisibleEnd: 4, Character: '*'},
		CustomLevels: []logger.LevelSpec{{Name: "DATABASE", Value: -6, Emoji: "🗄️", Color: logger.ColorBlue}},
	})
	root.WithField("module", "startup").Info("Server started")
	agent := NewAgentService(root, 123)
	agent.Process(context.Background())

	fmt.Println("\n--- Shared formatter change across child loggers ---")
	root.SetFormatter(logger.TintFormatter{})
	agent.log.Success("Output via tint")
	root.SetFormatter(logger.JSONFormatter{})
	agent.log.WithError(errors.New("timeout")).Error("JSON output")
}
