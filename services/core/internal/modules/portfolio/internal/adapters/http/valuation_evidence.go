package http

import "net/http"

func (s *handler) handleValuationEvidence(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, &appError{Status: http.StatusBadRequest, Body: APIError{
			Code: "invalid_query", Message: "valuation evidence does not accept query parameters",
		}})
		return
	}
	report, err := s.api.ValuationEvidence(r.Context())
	if err != nil {
		writeError(w, portfolioError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"schema_version":                report.SchemaVersion,
		"observation_cutoff":            report.ObservationCutoff,
		"source_availability_at_cutoff": report.SourceAvailabilityAtCutoff,
		"cash":                          report.Cash,
		"holdings":                      publicHoldingValuation(report.Holdings),
		"current":                       report.Current,
		"conversion_fx":                 report.ConversionFX,
	})
}
