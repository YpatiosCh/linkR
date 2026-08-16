package response

import (
	"encoding/json"
	"net/http"
)

// ErrorBody describes the machine-readable error payload returned to clients.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorEnvelope wraps an ErrorBody under an "error" key so error responses share a stable JSON shape.
type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// JSON writes a JSON response with the given status code and payload.
// It sets the Content-Type header to application/json, writes the status code, and encodes data
// via json.NewEncoder; the encoder's error is not returned and is silently dropped.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Error writes a JSON error response with the given HTTP status, a machine-readable code,
// and a human-readable message, wrapping them in an errorEnvelope.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, errorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}

// DecodeJSON decodes r.Body as JSON into v, rejecting any field not present
// in v's type. On failure (malformed JSON, an unknown field, or a body that
// exceeded a size limit set via http.MaxBytesReader upstream) it writes 400
// INVALID_BODY and returns false; callers should return immediately when it
// does.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		Error(w, http.StatusBadRequest, CodeInvalidBody, "request body is malformed")
		return false
	}
	return true
}
