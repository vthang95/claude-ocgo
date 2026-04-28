package routes

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/vthang95/claude-ocgo/internal/config"
	"github.com/vthang95/claude-ocgo/internal/logger"
	"github.com/vthang95/claude-ocgo/internal/router"
	"github.com/vthang95/claude-ocgo/services"
)

func RegisterMessages(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"type":    "invalid_request",
					"message": "Invalid JSON body",
				},
			})
			return
		}

		model := ""
		if m, ok := body["model"].(string); ok {
			model = m
		}

		// Overwrite model if flag is set
		originalModel := model
		if config.OVERWRITE_MODEL {
			body["model"] = config.DEFAULT_MODEL
			model = config.DEFAULT_MODEL
			logger.WriteLog("MODEL_OVERWRITTEN", map[string]any{"from": originalModel, "to": model})
		}

		route := router.RouteModel(model)

		// Set model on wrapped responseWriter for logging
		if setter, ok := w.(interface{ SetModel(string) }); ok {
			setter.SetModel(model)
		}

		logger.WriteLog("ROUTE", map[string]any{"model": model, "route": route})

		// Try with fallback if enabled
		if config.WITH_FALLBACK && len(config.FallbackModels) > 0 {
			fallbackErr := tryWithFallback(body, w, r, route)
			if fallbackErr != nil {
				logger.WriteLog(route+"_ERROR", map[string]any{"message": fallbackErr.Error()})
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"type":    "api_error",
						"message": fallbackErr.Error(),
					},
				})
			}
			return
		}

		// Original behavior without fallback
		var err error
		if route == "anthropic" {
			err = services.ForwardMiniMax(body, w, r)
		} else {
			err = services.ForwardOpenAI(body, w, r)
		}

		if err != nil {
			logger.WriteLog(route+"_ERROR", map[string]any{"message": err.Error()})
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"type":    "api_error",
					"message": err.Error(),
				},
			})
		}
	})
}

// tryWithFallback attempts to forward to the primary service,
// and retries with fallback models on failure
func tryWithFallback(body map[string]any, w http.ResponseWriter, r *http.Request, route string) error {
	// Try primary route first
	err := tryForward(body, w, r, route)
	if err == nil {
		return nil
	}

	logger.WriteLog("FALLBACK_TRIGGERED", map[string]any{
		"primary": route,
		"error":   err.Error(),
		"count":   len(config.FallbackModels),
	})

	// Try each fallback model
	for i, fallbackModel := range config.FallbackModels {
		logger.WriteLog("FALLBACK_ATTEMPT", map[string]any{
			"index": i,
			"model": fallbackModel,
		})

		// Create a modified body with the fallback model
		fallbackBody := make(map[string]any)
		for k, v := range body {
			fallbackBody[k] = v
		}
		fallbackBody["model"] = fallbackModel

		fallbackRoute := router.RouteModel(fallbackModel)
		err = tryForward(fallbackBody, w, r, fallbackRoute)
		if err == nil {
			logger.WriteLog("FALLBACK_SUCCESS", map[string]any{
				"model": fallbackModel,
				"route": fallbackRoute,
			})
			return nil
		}

		logger.WriteLog("FALLBACK_FAILED", map[string]any{
			"model": fallbackModel,
			"error": err.Error(),
		})
	}

	return fmt.Errorf("all models failed, last error: %s", err.Error())
}

// tryForward attempts to forward to a service and returns the error
func tryForward(body map[string]any, w http.ResponseWriter, r *http.Request, route string) error {
	var err error
	if route == "anthropic" {
		err = services.ForwardMiniMax(body, w, r)
	} else {
		err = services.ForwardOpenAI(body, w, r)
	}

	if err != nil {
		// Check if we got a successful response (even with error status from upstream)
		// by checking if we already wrote to the response writer
		return err
	}

	// If no error, request was successful
	return nil
}