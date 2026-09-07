# dis

`dis` is a small, type-safe dependency-injection registry for Go singleton
services. It uses explicit registration and generic lookup rather than
reflection-based auto-wiring.

## Install

```sh
go get github.com/qattidev/dis
```

## Use the default container

Register services during package initialization, then seal the container once
application startup has completed.

```go
package users

import "github.com/qattidev/dis"

type Service struct{}

func init() {
	dis.MustRegisterService(&Service{})
}
```

```go
func main() {
	dis.Seal()

	users, err := dis.GetService[*users.Service]()
	if err != nil {
		log.Fatal(err)
	}
	_ = users
}
```

Every type has one registration. To bind an implementation to an interface,
provide the interface as an explicit type parameter:

```go
dis.MustRegisterService[UserRepository](postgresRepository)
```

## Lazy factories

Factories construct a singleton on its first lookup. Use the supplied resolver
for dependencies so that `dis` can keep resolution in the same container and
report circular dependency chains.

```go
dis.MustRegisterFactory(func(r dis.Resolver) (*UserService, error) {
	repository, err := dis.GetServiceFrom[UserRepository](r)
	if err != nil {
		return nil, err
	}
	return NewUserService(repository), nil
})
```

A successful factory result is cached. Failed construction is returned to
current callers and retried by a later lookup.

## Lifecycle and testing

`GetService` returns an error before `Seal` is called. After sealing,
registration is immutable and service resolution is safe for concurrent use.
Invalid registrations, including duplicates, use `Must...` APIs and panic at
startup.

Use an isolated container for tests:

```go
container := dis.NewContainer()
dis.MustRegisterServiceIn(container, &fakeRepository{})
container.Seal()

repository, err := dis.GetServiceFrom[*fakeRepository](container)
```

Version 1 intentionally does not support reflection auto-wiring, named
services, runtime replacement, shutdown hooks, or service scopes other than
singletons.

## Runnable HTTP example

The [`examples/userserver`](examples/userserver) application shows a
repository registered from one package's `init()` function, a `UserService`
created by a resolver-backed factory with startup configuration, and HTTP
handlers that receive the resolved singleton at startup. Its composition root
passes `service.Config{MaxUsers: 2}` to `service.Register` before `dis.Seal()`;
the factory supplies that config to `NewUserService` alongside the resolved
repository. A positive `MaxUsers` value caps the users returned by `GET
/users`; zero or a negative value leaves the list unlimited.

```sh
go run ./examples/userserver
```

With the server running on port 8080:

```sh
curl http://localhost:8080/users
curl http://localhost:8080/users/1
curl -i http://localhost:8080/users/missing
```
