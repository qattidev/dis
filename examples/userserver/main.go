// userserver is a small HTTP application demonstrating dis package usage.
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/qattidev/dis"
	"github.com/qattidev/dis/examples/userserver/repository"
	"github.com/qattidev/dis/examples/userserver/service"
)

type userHandler struct {
	service *service.UserService
}

type errorResponse struct {
	Error string `json:"error"`
}

func main() {
	// All package init registrations are now complete. Lock the registry before
	// resolving the service graph and accepting requests.
	dis.Seal()
	userService, err := dis.GetService[*service.UserService]()
	if err != nil {
		log.Fatalf("resolve user service: %v", err)
	}

	handler := userHandler{service: userService}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", handler.listUsers)
	mux.HandleFunc("GET /users/{id}", handler.getUser)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("userserver listening on http://localhost%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (h userHandler) listUsers(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, h.service.ListUsers())
}

func (h userHandler) getUser(writer http.ResponseWriter, request *http.Request) {
	user, err := h.service.GetUser(request.PathValue("id"))
	if errors.Is(err, repository.ErrUserNotFound) {
		writeJSON(writer, http.StatusNotFound, errorResponse{Error: err.Error()})
		return
	}
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}
	writeJSON(writer, http.StatusOK, user)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
