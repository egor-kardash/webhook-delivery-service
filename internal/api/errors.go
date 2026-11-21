package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/egor-kardash/webhook-delivery-service/internal/domain"
)

const (
	codeValidationFailed     = "validation_failed"
	codeInternalError        = "internal_error"
	codeServiceUnavailable   = "service_unavailable"
	codeNotFound             = "not_found"
	codeMethodNotAllowed     = "method_not_allowed"
	codeRequestTooLarge      = "request_too_large"
	codeUnsupportedMediaType = "unsupported_media_type"
	codeInvalidJSON          = "invalid_json"
	codeDatabaseUnavailable  = "database_unavailable"
)

type errorBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

func writeServiceError(ctx context.Context, w http.ResponseWriter, err error) {
	var ve *domain.ValidationError

	switch {
	case errors.As(err, &ve):
		WriteErrorFields(ctx, w, http.StatusBadRequest, codeValidationFailed, "request validation failed", ve.Fields)
	case errors.Is(err, context.Canceled):
		LoggerFrom(ctx).Debug("request canceled", "error", err)
		WriteError(ctx, w, http.StatusServiceUnavailable, codeServiceUnavailable, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		LoggerFrom(ctx).Error("request timed out", "error", err)
		WriteError(ctx, w, http.StatusServiceUnavailable, codeServiceUnavailable, "service unavailable")
	default:
		LoggerFrom(ctx).Error("request failed", "error", err)
		WriteError(ctx, w, http.StatusInternalServerError, codeInternalError, "internal server error")
	}
}

func writeDecodeError(ctx context.Context, w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		WriteError(ctx, w, http.StatusRequestEntityTooLarge,
			codeRequestTooLarge, "request body is too large")
		return
	}

	if errors.Is(err, ErrUnsupportedMediaType) {
		WriteError(ctx, w, http.StatusUnsupportedMediaType,
			codeUnsupportedMediaType, "content type must be application/json")
		return
	}

	WriteError(ctx, w, http.StatusBadRequest,
		codeInvalidJSON, "request body must be a single JSON object")
}

func WriteError(ctx context.Context, w http.ResponseWriter, status int, code, message string) {
	WriteErrorFields(ctx, w, status, code, message, nil)
}

func WriteErrorFields(
	ctx context.Context,
	w http.ResponseWriter,
	status int,
	code, message string,
	fields map[string]string,
) {
	body, err := json.Marshal(errorEnvelope{
		Error: errorBody{
			Code:    code,
			Message: message,
			Fields:  fields,
		},
	})
	if err != nil {
		LoggerFrom(ctx).Error("encode error response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	writeBody(ctx, w, status, body)
}
