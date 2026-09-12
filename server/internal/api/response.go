package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// ErrorCode is a stable, machine-readable identifier for a failure. The
// frontend switches on these; the human-readable message may change freely.
type ErrorCode string

const (
	CodeBadRequest   ErrorCode = "bad_request"
	CodeValidation   ErrorCode = "validation_failed"
	CodeUnauthorized ErrorCode = "unauthorized"
	CodeForbidden    ErrorCode = "forbidden"
	CodeNotFound     ErrorCode = "not_found"
	CodeNotAllowed   ErrorCode = "method_not_allowed"
	CodeConflict     ErrorCode = "conflict"
	CodeRateLimited  ErrorCode = "rate_limited"
	CodeInternal     ErrorCode = "internal_error"
	CodeUnavailable  ErrorCode = "unavailable"
)

// Error is the single shape every failing endpoint returns, so the client has
// exactly one error path to implement.
type Error struct {
	Code    ErrorCode         `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`

	status int
	cause  error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// Status reports the HTTP status this error maps to.
func (e *Error) Status() int {
	if e.status == 0 {
		return http.StatusInternalServerError
	}
	return e.status
}

// WithCause attaches the underlying error. The cause is logged server-side and
// never serialised, so internal detail cannot leak to a client.
func (e *Error) WithCause(err error) *Error {
	clone := *e
	clone.cause = err
	return &clone
}

func newError(status int, code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), status: status}
}

func BadRequest(format string, args ...any) *Error {
	return newError(http.StatusBadRequest, CodeBadRequest, format, args...)
}

func Unauthorized(format string, args ...any) *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, format, args...)
}

func Forbidden(format string, args ...any) *Error {
	return newError(http.StatusForbidden, CodeForbidden, format, args...)
}

func NotFound(format string, args ...any) *Error {
	return newError(http.StatusNotFound, CodeNotFound, format, args...)
}

func MethodNotAllowed(method string) *Error {
	return newError(http.StatusMethodNotAllowed, CodeNotAllowed, "Method %s is not allowed here.", method)
}

func Conflict(format string, args ...any) *Error {
	return newError(http.StatusConflict, CodeConflict, format, args...)
}

func Unavailable(format string, args ...any) *Error {
	return newError(http.StatusServiceUnavailable, CodeUnavailable, format, args...)
}

func RateLimited(format string, args ...any) *Error {
	return newError(http.StatusTooManyRequests, CodeRateLimited, format, args...)
}

// Internal is deliberately vague to the caller; the real error goes to the log.
func Internal(err error) *Error {
	return newError(http.StatusInternalServerError, CodeInternal, "Something went wrong on our end.").WithCause(err)
}

// Invalid reports per-field validation failures.
func Invalid(fields map[string]string) *Error {
	e := newError(http.StatusUnprocessableEntity, CodeValidation, "Some fields need attention.")
	e.Fields = fields
	return e
}

// Handler is an http.Handler that may fail. Returning an error instead of
// writing one keeps every handler's exit path uniform and makes it impossible
// to forget the log-then-respond step.
type Handler func(http.ResponseWriter, *http.Request) error

// Wrap adapts a Handler for use with a standard router.
func Wrap(log *slog.Logger, h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}

		var apiErr *Error
		if !errors.As(err, &apiErr) {
			apiErr = Internal(err)
		}

		entry := log.With(
			"method", r.Method,
			"path", r.URL.Path,
			"code", apiErr.Code,
			"status", apiErr.Status(),
		)
		if cause := apiErr.Unwrap(); cause != nil {
			entry = entry.With("error", cause)
		}
		if apiErr.Status() >= 500 {
			entry.Error("request failed")
		} else {
			entry.Info("request rejected")
		}

		writeJSON(w, log, apiErr.Status(), map[string]any{"error": apiErr})
	}
}

// JSON writes a success response.
func JSON(w http.ResponseWriter, log *slog.Logger, status int, body any) error {
	writeJSON(w, log, status, body)
	return nil
}

// NoContent ends a request with 204.
func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		// Marshalling our own response is a programming error, and the status
		// line may already be committed, so all we can do is record it.
		log.Error("encode response", "error", err)
		http.Error(w, `{"error":{"code":"internal_error","message":"Something went wrong on our end."}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := w.Write(payload); err != nil {
		log.Debug("write response", "error", err)
	}
}

// DecodeJSON reads a JSON request body, rejecting oversized and malformed
// input with a client-facing error rather than a 500.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	const maxBody = 1 << 20 // 1 MiB

	if ct := r.Header.Get("Content-Type"); ct != "" &&
		ct != "application/json" && ct != "application/json; charset=utf-8" {
		return BadRequest("Expected a JSON request body.")
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return BadRequest("Request body is larger than %d bytes.", maxBody)
		}
		return BadRequest("Request body is not valid JSON.").WithCause(err)
	}
	if dec.More() {
		return BadRequest("Request body must contain a single JSON object.")
	}
	return nil
}
