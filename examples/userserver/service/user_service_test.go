package service

import (
	"errors"
	"testing"

	"github.com/qattidev/dis/examples/userserver/repository"
)

type testRepository struct {
	users []repository.User
}

func (r testRepository) List() []repository.User {
	users := make([]repository.User, len(r.users))
	copy(users, r.users)
	return users
}

func (testRepository) FindByID(string) (repository.User, error) {
	return repository.User{}, errors.New("not implemented")
}

func TestListUsersHonorsMaxUsers(t *testing.T) {
	users := []repository.User{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	service := NewUserService(testRepository{users: users}, Config{MaxUsers: 2})

	actual := service.ListUsers()
	if len(actual) != 2 {
		t.Fatalf("ListUsers returned %d users, want 2", len(actual))
	}
	if actual[0].ID != "1" || actual[1].ID != "2" {
		t.Fatalf("ListUsers returned %#v, want the first two users", actual)
	}
}

func TestListUsersIsUnlimitedForNonPositiveMaxUsers(t *testing.T) {
	users := []repository.User{{ID: "1"}, {ID: "2"}, {ID: "3"}}

	for _, maxUsers := range []int{0, -1} {
		t.Run(testName(maxUsers), func(t *testing.T) {
			service := NewUserService(testRepository{users: users}, Config{MaxUsers: maxUsers})

			if got := len(service.ListUsers()); got != len(users) {
				t.Fatalf("ListUsers returned %d users, want %d", got, len(users))
			}
		})
	}
}

func testName(maxUsers int) string {
	if maxUsers == 0 {
		return "zero"
	}
	return "negative"
}
