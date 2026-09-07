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
	config     Config
}

// Config configures UserService at application startup.
type Config struct {
	// MaxUsers caps the number of users returned by ListUsers. A non-positive
	// value leaves the result unlimited.
	MaxUsers int
}

// NewUserService creates a service around repository using config.
func NewUserService(repository repository.UserRepository, config Config) *UserService {
	return &UserService{repository: repository, config: config}
}

// Register adds the configured UserService factory to the default container.
// Call it during application startup before dis.Seal.
func Register(config Config) {
	dis.MustRegisterFactory[*UserService](func(resolver dis.Resolver) (*UserService, error) {
		repository, err := dis.GetServiceFrom[repository.UserRepository](resolver)
		if err != nil {
			return nil, fmt.Errorf("resolve user repository: %w", err)
		}
		return NewUserService(repository, config), nil
	})
}

// ListUsers returns the configured maximum number of known users.
func (s *UserService) ListUsers() []repository.User {
	users := s.repository.List()
	if s.config.MaxUsers > 0 && len(users) > s.config.MaxUsers {
		return users[:s.config.MaxUsers]
	}
	return users
}

// GetUser returns one user by ID.
func (s *UserService) GetUser(id string) (repository.User, error) {
	return s.repository.FindByID(id)
}
