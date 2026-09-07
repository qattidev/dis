# dis (Dependency Injection System)

`dis` is a small, type-safe dependency-injection registry for Go singleton
services. It uses explicit registration and generic lookup rather than
reflection-based auto-wiring.

## Install

```sh
go get github.com/qattidev/dis
```

## Use the default container

Register services during package initialization, then seal the container once
application startup has completed. `Seal` freezes registrations; it does not
construct services or validate the dependency graph. Resolve required services
at startup before accepting traffic.

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

Every exact type has one registration. Pointer, concrete, and interface types
are distinct. To bind an implementation to an interface, provide the interface
as an explicit type parameter:

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
current callers and retried by a later lookup. If a factory panics, the caller
that runs it receives the original panic; concurrent callers receive an error
matching `ErrFactoryPanicked`, and a later lookup retries construction.

## Lifecycle and testing

`GetService` returns an error before `Seal` is called. After sealing,
registration is immutable and service resolution is safe for concurrent use.
Concurrent factory dependency cycles are reported as `ErrCircularDependency`
rather than waiting indefinitely. Registered services remain responsible for
their own thread safety, timeouts, and cleanup. Invalid registrations,
including duplicates, use `Must...` APIs and panic at startup.

Use an isolated container for tests:

```go
container := dis.NewContainer()
dis.MustRegisterServiceIn(container, &fakeRepository{})
container.Seal()

repository, err := dis.GetServiceFrom[*fakeRepository](container)
```

The process-wide default container cannot be reset or copied. Prefer an
application-owned container when a composition root needs configurable
registrations; use fresh containers in tests.

Version 1 intentionally does not support reflection auto-wiring, named
services, runtime replacement, shutdown hooks, cancellation, or service scopes
other than singletons.

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

## Development checks

GitHub Actions runs all checks sequentially in one Ubuntu job. Go 1.24 checks
minimum-version compatibility, module integrity and tidiness, vet, and tests
with race detection. Go 1.27 runs the analysis suite and an additional test pass.
Every check is blocking; independent checks still run after an earlier failure.

The analysis tools are pinned to golangci-lint v2.13.2, govulncheck v1.7.0, and
actionlint v1.7.12. With Go 1.27 installed, install golangci-lint from its
[official release](https://github.com/golangci/golangci-lint/releases/tag/v2.13.2),
then install the remaining tools:

```sh
go install golang.org/x/vuln/cmd/govulncheck@v1.7.0
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
```

Run the checks from the repository root, with the tool binaries on your `PATH`:

```sh
go mod verify
go mod tidy -diff
go vet ./...
go test -race -shuffle=on -count=2 ./...
golangci-lint config verify
golangci-lint run ./...
govulncheck ./...
actionlint -color
```

`.golangci.yml` explicitly enables 58 linters covering correctness, security,
performance, tests, and common style, plus `gofmt` and `goimports` formatting
checks. Tests and examples are included. Apply formatting locally with
`golangci-lint fmt`; CI only checks it. Any `nolint` suppression must name its
linter and explain why it is needed.

Vulnerability scanning uses the Go 1.27 toolchain's standard library. Passing
the Go 1.24 compatibility tests does not imply that older Go releases have
the same security fixes.
