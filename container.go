package dis

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	// ErrServiceNotFound indicates that no service was registered for a type.
	ErrServiceNotFound = errors.New("dis: service not found")

	// ErrContainerNotSealed indicates that resolution was attempted before the
	// container's registrations were finalized.
	ErrContainerNotSealed = errors.New("dis: container is not sealed")

	// ErrContainerSealed indicates that registration was attempted after Seal.
	ErrContainerSealed = errors.New("dis: container is sealed")

	// ErrServiceAlreadyRegistered indicates that a type was registered twice.
	ErrServiceAlreadyRegistered = errors.New("dis: service already registered")

	// ErrNilService indicates that a nil service value or factory result was
	// supplied.
	ErrNilService = errors.New("dis: nil service")

	// ErrCircularDependency indicates that a factory requested a service already
	// being resolved in its dependency chain.
	ErrCircularDependency = errors.New("dis: circular dependency")
)

// Resolver is supplied to a Factory while it is constructing a service.
// It is intentionally opaque: pass it to GetServiceFrom to resolve another
// typed service from the same container.
type Resolver interface {
	resolveService(reflect.Type) (any, error)
}

// Factory creates one service value. Its result is cached after the first
// successful call. A returned error is not cached, so a later lookup retries
// construction.
type Factory[T any] func(Resolver) (T, error)

// Container holds registrations and lazily-created singleton values.
// A container must be sealed before services can be resolved.
type Container struct {
	mu       sync.RWMutex
	sealed   bool
	services map[reflect.Type]*serviceEntry
}

type serviceEntry struct {
	value   any
	ready   bool
	factory func(Resolver) (any, error)

	mu       sync.Mutex
	inFlight *factoryCall
}

type factoryCall struct {
	done  chan struct{}
	value any
	err   error
}

type resolution struct {
	container *Container
	path      []reflect.Type
}

// ServiceNotFoundError adds the requested type to ErrServiceNotFound.
type ServiceNotFoundError struct {
	Type reflect.Type
}

func (e *ServiceNotFoundError) Error() string {
	return fmt.Sprintf("%s: %s", ErrServiceNotFound, e.Type)
}

func (e *ServiceNotFoundError) Unwrap() error { return ErrServiceNotFound }

// CircularDependencyError adds the dependency path to ErrCircularDependency.
type CircularDependencyError struct {
	Path []reflect.Type
}

func (e *CircularDependencyError) Error() string {
	parts := make([]string, len(e.Path))
	for i, serviceType := range e.Path {
		parts[i] = serviceType.String()
	}
	return fmt.Sprintf("%s: %s", ErrCircularDependency, strings.Join(parts, " -> "))
}

func (e *CircularDependencyError) Unwrap() error { return ErrCircularDependency }

var defaultContainer = NewContainer()

// DefaultContainer returns the process-wide singleton container used by the
// package-level registration and lookup functions.
func DefaultContainer() *Container {
	return defaultContainer
}

// NewContainer creates an independent, unsealed container. It is useful for
// tests and for applications that explicitly need more than the default
// process-wide registry.
func NewContainer() *Container {
	return &Container{services: make(map[reflect.Type]*serviceEntry)}
}

// Seal finalizes registrations in the process-wide container. It is idempotent.
func Seal() {
	defaultContainer.Seal()
}

// Seal finalizes registrations in c. It is idempotent. Once sealed, c accepts
// no further registrations and may resolve services concurrently.
func (c *Container) Seal() {
	if c == nil {
		panic("dis: cannot seal a nil container")
	}

	c.mu.Lock()
	c.sealed = true
	c.mu.Unlock()
}

// MustRegisterService registers value as the singleton for its declared type
// in the process-wide container. It panics for invalid registrations.
//
// To register an implementation under an interface, specify the interface
// explicitly: MustRegisterService[UserRepository](postgresRepository).
func MustRegisterService[T any](value T) {
	MustRegisterServiceIn(defaultContainer, value)
}

// MustRegisterServiceIn registers value as the singleton for its declared type
// in c. It panics for a nil container or value, duplicate type, or sealed
// container.
func MustRegisterServiceIn[T any](c *Container, value T) {
	serviceType := typeOf[T]()
	if isNil(value) {
		panic(fmt.Errorf("%w: %s", ErrNilService, serviceType))
	}

	c.register(serviceType, &serviceEntry{value: value, ready: true})
}

// MustRegisterFactory registers factory as the lazy singleton factory for its
// declared result type in the process-wide container. It panics for invalid
// registrations.
func MustRegisterFactory[T any](factory Factory[T]) {
	MustRegisterFactoryIn(defaultContainer, factory)
}

// MustRegisterFactoryIn registers factory as the lazy singleton factory for
// its declared result type in c. Factories must use their Resolver argument to
// obtain dependencies with GetServiceFrom.
func MustRegisterFactoryIn[T any](c *Container, factory Factory[T]) {
	serviceType := typeOf[T]()
	if factory == nil {
		panic(fmt.Errorf("%w: factory for %s", ErrNilService, serviceType))
	}

	c.register(serviceType, &serviceEntry{
		factory: func(resolver Resolver) (any, error) {
			value, err := factory(resolver)
			if err != nil {
				return nil, err
			}
			if isNil(value) {
				return nil, fmt.Errorf("%w: factory result for %s", ErrNilService, serviceType)
			}
			return value, nil
		},
	})
}

// GetService resolves a service by its type from the process-wide container.
// The container must first be sealed with Seal.
func GetService[T any]() (T, error) {
	return GetServiceFrom[T](defaultContainer)
}

// GetServiceFrom resolves a service by type through resolver. In a Factory,
// pass the Resolver supplied to the factory so cyclic dependencies can be
// detected. A *Container may also be passed directly.
func GetServiceFrom[T any](resolver Resolver) (T, error) {
	var zero T
	if resolver == nil {
		return zero, errors.New("dis: nil resolver")
	}

	value, err := resolver.resolveService(typeOf[T]())
	if err != nil {
		return zero, err
	}

	service, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("dis: registered value for %s has unexpected type %T", typeOf[T](), value)
	}
	return service, nil
}

func (c *Container) register(serviceType reflect.Type, entry *serviceEntry) {
	if c == nil {
		panic("dis: cannot register a service in a nil container")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sealed {
		panic(fmt.Errorf("%w: %s", ErrContainerSealed, serviceType))
	}
	if c.services == nil {
		c.services = make(map[reflect.Type]*serviceEntry)
	}
	if _, exists := c.services[serviceType]; exists {
		panic(fmt.Errorf("%w: %s", ErrServiceAlreadyRegistered, serviceType))
	}
	c.services[serviceType] = entry
}

func (c *Container) resolveService(serviceType reflect.Type) (any, error) {
	if c == nil {
		return nil, errors.New("dis: nil container")
	}
	return (&resolution{container: c}).resolveService(serviceType)
}

func (r *resolution) resolveService(serviceType reflect.Type) (any, error) {
	for _, activeType := range r.path {
		if activeType == serviceType {
			path := append(append([]reflect.Type(nil), r.path...), serviceType)
			return nil, &CircularDependencyError{Path: path}
		}
	}

	nextPath := make([]reflect.Type, len(r.path)+1)
	copy(nextPath, r.path)
	nextPath[len(r.path)] = serviceType
	return r.container.resolve(serviceType, &resolution{container: r.container, path: nextPath})
}

func (c *Container) resolve(serviceType reflect.Type, resolver Resolver) (any, error) {
	c.mu.RLock()
	if !c.sealed {
		c.mu.RUnlock()
		return nil, fmt.Errorf("%w: %s", ErrContainerNotSealed, serviceType)
	}
	entry, exists := c.services[serviceType]
	c.mu.RUnlock()

	if !exists {
		return nil, &ServiceNotFoundError{Type: serviceType}
	}
	return entry.get(resolver, serviceType)
}

func (e *serviceEntry) get(resolver Resolver, serviceType reflect.Type) (any, error) {
	e.mu.Lock()
	if e.ready {
		value := e.value
		e.mu.Unlock()
		return value, nil
	}
	if e.inFlight != nil {
		call := e.inFlight
		e.mu.Unlock()
		<-call.done
		return call.value, call.err
	}

	call := &factoryCall{done: make(chan struct{})}
	e.inFlight = call
	factory := e.factory
	e.mu.Unlock()

	value, err := factory(resolver)
	if err != nil {
		err = fmt.Errorf("dis: construct service %s: %w", serviceType, err)
	}

	e.mu.Lock()
	if err == nil {
		e.value = value
		e.ready = true
	}
	e.inFlight = nil
	call.value = value
	call.err = err
	close(call.done)
	e.mu.Unlock()

	return value, err
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

func isNil(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
