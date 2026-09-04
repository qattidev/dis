// Package service contains application behavior for the userserver example.
package service

import (
	"fmt"

	"github.com/qattidev/dis"
	"github.com/qattidev/dis/examples/userserver/repository"
)

// UserService provides user-oriented application operations.
type UserService struct {
	repository repository.UserRepository
}

// NewUserService creates a service around repository.
func NewUserService(repository repository.UserRepository) *UserService {
	return &UserService{repository: repository}
}

func init() {
	// The repository package is imported above, so its init function registers
	// the UserRepository before this factory is registered.
	dis.MustRegisterFactory[*UserService](func(resolver dis.Resolver) (*UserService, error) {
		repository, err := dis.GetServiceFrom[repository.UserRepository](resolver)
		if err != nil {
			return nil, fmt.Errorf("resolve user repository: %w", err)
		}
		return NewUserService(repository), nil
	})
}

// ListUsers returns all known users.
func (s *UserService) ListUsers() []repository.User {
	return s.repository.List()
}

// GetUser returns one user by ID.
func (s *UserService) GetUser(id string) (repository.User, error) {
	return s.repository.FindByID(id)
}
