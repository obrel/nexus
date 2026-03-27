package http

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse is the standard RFC-compliant error response envelope.
type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// respondSuccess writes a successful JSON response with the given status code and data.
func respondSuccess(w http.ResponseWriter, statusCode int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(data)
}

// respondError writes an error JSON response with the given status code and message.
func respondError(w http.ResponseWriter, statusCode int, message string) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(ErrorResponse{Success: false, Message: message})
}
