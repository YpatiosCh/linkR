package response

import (
	"errors"
	"net/http"

	"linkMe/internal/msgs"
	"linkMe/internal/utils/logctx"
)

// errorStatusMap maps sentinel errors from internal/msgs to the HTTP status code and response
// code they should be reported with.
var errorStatusMap = map[error]struct {
	Status int
	Code   string
}{
	msgs.ErrInvalidCredentials:   {http.StatusUnauthorized, CodeInvalidCredentials},
	msgs.ErrEmailAlreadyExists:   {http.StatusConflict, CodeEmailAlreadyExists},
	msgs.ErrUserNotFound:         {http.StatusNotFound, CodeUserNotFound},
	msgs.ErrPasswordNotSet:       {http.StatusBadRequest, CodePasswordNotSet},
	msgs.ErrTokenInvalid:         {http.StatusUnauthorized, CodeTokenInvalid},
	msgs.ErrTokenAlreadyUsed:     {http.StatusUnauthorized, CodeTokenAlreadyUsed},
	msgs.ErrSessionRevoked:       {http.StatusUnauthorized, CodeSessionRevoked},
	msgs.ErrTokenReuseDetected:   {http.StatusUnauthorized, CodeTokenReuseDetected},
	msgs.ErrSubscriptionNotFound: {http.StatusInternalServerError, CodeInternalError},
	msgs.ErrInvalidInput:         {http.StatusBadRequest, CodeInvalidInput},
	msgs.ErrPasswordAlreadySet:   {http.StatusConflict, CodePasswordAlreadySet},
	msgs.ErrTooManyLoginAttempts: {http.StatusTooManyRequests, CodeTooManyRequests},
}

// HandleError writes a JSON error response for the given error, associated
// with the originating request r (used to recover the request-scoped
// logger and its request ID for correlation). It matches the error against
// the known sentinel errors with errors.Is; on a match it logs the error at
// Warn (client errors, status < 500) or Error (status >= 500) and responds
// with the mapped status and code using the error's message. An unmatched
// error is always treated as a server fault: it's logged at Error and
// responds with 500 INTERNAL_ERROR and a generic message, regardless of
// what the underlying error says.
func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	logger := logctx.FromContext(r.Context())

	for sentinel, mapped := range errorStatusMap {
		if errors.Is(err, sentinel) {
			if mapped.Status >= http.StatusInternalServerError {
				logger.Error("request failed", "code", mapped.Code, "status", mapped.Status, "error", err)
			} else {
				logger.Warn("request failed", "code", mapped.Code, "status", mapped.Status, "error", err)
			}
			Error(w, mapped.Status, mapped.Code, err.Error())
			return
		}
	}

	logger.Error("unhandled error", "error", err)
	Error(w, http.StatusInternalServerError, CodeInternalError, "something went wrong")
}
