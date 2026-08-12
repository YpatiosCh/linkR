package response

import (
	"errors"
	"log"
	"net/http"

	"linkMe/internal/msgs"
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
}

// HandleError writes a JSON error response for the given error.
// It matches the error against the known sentinel errors with errors.Is; on a match it responds
// with the mapped status and code using the error's message, otherwise it logs the unexpected
// error and responds with 500 INTERNAL_ERROR and a generic message.
func HandleError(w http.ResponseWriter, err error) {
	for sentinel, mapped := range errorStatusMap {
		if errors.Is(err, sentinel) {
			Error(w, mapped.Status, mapped.Code, err.Error())
			return
		}
	}

	log.Printf("unhandled error: %v", err)
	Error(w, http.StatusInternalServerError, CodeInternalError, "something went wrong")
}
