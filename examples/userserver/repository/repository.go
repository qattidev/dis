// Package repository supplies the data-access layer for the userserver example.
package repository

import (
	"errors"

	"github.com/qattidev/dis"
)

// ErrUserNotFound is returned when no user has the requested ID.
var ErrUserNotFound = errors.New("user not found")

// User is the example application's user representation.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// UserRepository defines the data required by the user service.
type UserRepository interface {
	List() []User
	FindByID(id string) (User, error)
}

type inMemoryRepository struct {
	usersByID map[string]User
	users     []User
}

// NewInMemory creates the example's seeded repository implementation.
func NewInMemory() UserRepository {
	users := []User{
		{ID: "1", Name: "Ada Lovelace", Email: "ada@example.com"},
		{ID: "2", Name: "Grace Hopper", Email: "grace@example.com"},
	}
	usersByID := make(map[string]User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}

	return &inMemoryRepository{usersByID: usersByID, users: users}
}

func init() {
	// Register the implementation under its interface so consumers depend on
	// the abstraction, not this in-memory implementation.
	dis.MustRegisterService[UserRepository](NewInMemory())
}

func (r *inMemoryRepository) List() []User {
	users := make([]User, len(r.users))
	copy(users, r.users)
	return users
}

func (r *inMemoryRepository) FindByID(id string) (User, error) {
	user, exists := r.usersByID[id]
	if !exists {
		return User{}, ErrUserNotFound
	}
	return user, nil
}
