package httpapi

import (
	"fmt"
	"net/http"

	"caravel/internal/db"
)

type mapItemResponse struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Category      string  `json:"category"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
	GoogleMapsURL string  `json:"google_maps_url"`
}

func mapItemToResponse(i db.MapItem) mapItemResponse {
	return mapItemResponse{
		ID:            i.ID,
		Title:         i.Title,
		Category:      i.Category,
		Lat:           i.Lat,
		Lng:           i.Lng,
		GoogleMapsURL: fmt.Sprintf("https://www.google.com/maps/search/?api=1&query=%f,%f", i.Lat, i.Lng),
	}
}

func (s *Server) handleGetTripMap(w http.ResponseWriter, r *http.Request) {
	trip, ok := s.loadOwnedTrip(w, r)
	if !ok {
		return
	}

	items, err := s.Store.ListMapItems(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load map")
		return
	}

	resp := make([]mapItemResponse, len(items))
	for i, it := range items {
		resp[i] = mapItemToResponse(it)
	}
	writeJSON(w, http.StatusOK, resp)
}
