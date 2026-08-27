package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"concrete-specimen-chain-service/internal/domain"
)

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, err error) {
	var business *domain.Error
	if errors.As(err, &business) {
		status := http.StatusUnprocessableEntity
		if business.Code == domain.ErrVersionConflict.Code || business.Code == domain.ErrIdentityConflict.Code {
			status = http.StatusConflict
		} else if business.Retryable {
			status = http.StatusServiceUnavailable
		}
		writeJSON(writer, status, business)
		return
	}
	writeJSON(writer, http.StatusInternalServerError, domain.NewError("INTERNAL", "STORAGE", true))
}

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
