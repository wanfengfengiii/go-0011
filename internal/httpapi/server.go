// Package httpapi exposes the documented write, evaluation, and readiness seams.
package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"concrete-specimen-chain-service/internal/domain"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

type EventService interface {
	Submit(context.Context, ingest.Envelope) (ingest.Receipt, error)
	Advance(specimenID string, until time.Time) []ingest.Event
}

type Server struct {
	events      EventService
	repository  ingest.Repository
	catalog     catalogRepository
	evaluations evaluationRepository
	groups      *groupRegistry
	handler     http.Handler
}

func New(events EventService, repository ingest.Repository) *Server {
	server := &Server{events: events, repository: repository, groups: newGroupRegistry()}
	server.catalog, _ = repository.(catalogRepository)
	server.evaluations, _ = repository.(evaluationRepository)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/ready", server.ready)
	mux.HandleFunc("POST /v1/projects", server.createProject)
	mux.HandleFunc("POST /v1/pour-sections", server.createPourSection)
	mux.HandleFunc("POST /v1/mix-designs", server.createMixDesign)
	mux.HandleFunc("POST /v1/inspection-rules", server.createInspectionRule)
	mux.HandleFunc("POST /v1/sample-groups", server.createSampleGroup)
	mux.HandleFunc("POST /v1/events:batch", server.batchEvents)
	mux.HandleFunc("POST /v1/specimens/", server.specimenEvent)
	mux.HandleFunc("GET /v1/specimens/", server.specimenChain)
	mux.HandleFunc("POST /v1/sample-groups/", server.groupAction)
	mux.HandleFunc("GET /v1/sample-groups/", server.groupAction)
	server.handler = mux
	return server
}

type catalogRepository interface {
	CreateProject(context.Context, domain.Project) error
	CreatePourSection(context.Context, domain.PourSection) error
	CreateMixDesign(context.Context, domain.MixDesign) error
	CreateInspectionRule(context.Context, domain.InspectionRule) error
	InspectionRule(context.Context, string, int) (domain.InspectionRule, error)
	MixDesign(context.Context, string) (domain.MixDesign, error)
	PourSection(context.Context, string) (domain.PourSection, error)
	CreateSampleGroup(context.Context, storage.GroupCreation) error
	SampleGroup(context.Context, string) (domain.SampleGroup, []domain.Specimen, error)
	Chain(context.Context, string) ([]domain.EventRecord, error)
	PressureResult(context.Context, string) (*domain.PressureResult, error)
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.handler.ServeHTTP(writer, request)
}

func (s *Server) ready(writer http.ResponseWriter, request *http.Request) {
	status, err := s.repository.Ready(request.Context())
	if err != nil {
		writeError(writer, err)
		return
	}
	httpStatus := http.StatusOK
	if !status.Ready {
		httpStatus = http.StatusServiceUnavailable
	}
	writeJSON(writer, httpStatus, status)
}

func (s *Server) specimenEvent(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, "/v1/specimens/")
	specimenID, suffix, found := strings.Cut(path, "/")
	if !found || suffix != "events" || specimenID == "" {
		http.NotFound(writer, request)
		return
	}
	var envelope ingest.Envelope
	if err := decodeJSON(request, &envelope); err != nil || envelope.SpecimenID != specimenID {
		writeError(writer, validationError())
		return
	}
	receipt, err := s.events.Submit(request.Context(), envelope)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, receipt)
}

func validationError() error {
	return domain.NewError("VALIDATION", "VALIDATION", false)
}
