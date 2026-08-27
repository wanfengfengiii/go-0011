package httpapi

import (
	"net/http"

	"concrete-specimen-chain-service/internal/ingest"
)

type batchItem struct {
	Receipt *ingest.Receipt `json:"receipt,omitempty"`
	Error   any             `json:"error,omitempty"`
}

func (s *Server) batchEvents(writer http.ResponseWriter, request *http.Request) {
	var envelopes []ingest.Envelope
	if err := decodeJSON(request, &envelopes); err != nil {
		writeError(writer, validationError())
		return
	}
	results := make([]batchItem, 0, len(envelopes))
	for _, envelope := range envelopes {
		receipt, err := s.events.Submit(request.Context(), envelope)
		if err != nil {
			results = append(results, batchItem{Error: err})
			continue
		}
		results = append(results, batchItem{Receipt: &receipt})
	}
	writeJSON(writer, http.StatusMultiStatus, results)
}
