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

	// ErrFactoryPanicked indicates that a factory panicked while constructing a
	// service. The goroutine that invoked the factory receives the original
	// panic; concurrent callers receive an error wrapping this sentinel.
	ErrFactoryPanicked = errors.New("dis: factory panicked")
)

// Resolver is supplied to a Factory while it is constructing a service.
// It is intentionally opaque: pass it to GetServiceFrom to resolve another
// typed service from the same container.
type Resolver interface {
	resolveService(reflect.Type) (any, error)
}

// Factory creates one service value. Its result is cached after the first
// successful call. A returned error is not cached, so a later lookup retries
// construction. If a factory panics, the caller that ran it receives that
// panic, concurrent callers receive FactoryPanicError, and a later lookup
// retries construction.
type Factory[T any] func(Resolver) (T, error)

// Container holds registrations and lazily-created singleton values. A
// Container must not be copied after first use. A container must be sealed
// before services can be resolved.
type Container struct {
	mu       sync.RWMutex
	waitMu   sync.Mutex
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
	done        chan struct{}
	serviceType reflect.Type
	value       any
	err         error

	// waitingFor is protected by Container.waitMu. A factory can resolve
	// dependencies from multiple goroutines, so each active wait is retained.
	waitingFor map[*factoryCall]struct{}
}

type resolution struct {
	container *Container
	path      []reflect.Type
	call      *factoryCall
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

// FactoryPanicError identifies the service whose factory panicked. It is
// returned to callers that were waiting for another goroutine to construct the
// service.
type FactoryPanicError struct {
	Type reflect.Type
}

func (e *FactoryPanicError) Error() string {
	return fmt.Sprintf("%s: %s", ErrFactoryPanicked, e.Type)
}

func (e *FactoryPanicError) Unwrap() error { return ErrFactoryPanicked }

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
// no further registrations and may resolve services concurrently. Seal does
// not construct services or validate their dependency graph.
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
	return r.container.resolve(serviceType, &resolution{
		container: r.container,
		path:      nextPath,
		call:      r.call,
	})
}

func (c *Container) resolve(serviceType reflect.Type, resolver *resolution) (any, error) {
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

func (e *serviceEntry) get(resolver *resolution, serviceType reflect.Type) (value any, err error) {
	e.mu.Lock()
	if e.ready {
		value := e.value
		e.mu.Unlock()
		return value, nil
	}
	if e.inFlight != nil {
		call := e.inFlight
		e.mu.Unlock()
		if cycle := resolver.container.beginWait(resolver.call, call); cycle != nil {
			return nil, &CircularDependencyError{Path: cycle}
		}
		<-call.done
		resolver.container.endWait(resolver.call, call)
		return call.value, call.err
	}

	call := &factoryCall{done: make(chan struct{}), serviceType: serviceType}
	e.inFlight = call
	factory := e.factory
	e.mu.Unlock()

	completed := false
	defer func() {
		if completed {
			e.finish(call, value, err)
			return
		}

		// A factory panic must not strand waiters. The initiating caller receives
		// the original panic, while callers waiting on this factory receive the
		// typed error published here and may retry construction.
		e.finish(call, nil, &FactoryPanicError{Type: serviceType})
	}()

	value, err = factory(&resolution{
		container: resolver.container,
		path:      resolver.path,
		call:      call,
	})
	completed = true
	if err != nil {
		err = fmt.Errorf("dis: construct service %s: %w", serviceType, err)
	}
	return value, err
}

func (e *serviceEntry) finish(call *factoryCall, value any, err error) {
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
}

func (c *Container) beginWait(waiter, target *factoryCall) []reflect.Type {
	if waiter == nil {
		return nil
	}

	c.waitMu.Lock()
	defer c.waitMu.Unlock()

	if path := c.waitPath(target, waiter, make(map[*factoryCall]struct{})); path != nil {
		return append([]reflect.Type{waiter.serviceType}, path...)
	}
	if waiter.waitingFor == nil {
		waiter.waitingFor = make(map[*factoryCall]struct{})
	}
	waiter.waitingFor[target] = struct{}{}
	return nil
}

func (c *Container) endWait(waiter, target *factoryCall) {
	if waiter == nil {
		return
	}

	c.waitMu.Lock()
	delete(waiter.waitingFor, target)
	if len(waiter.waitingFor) == 0 {
		waiter.waitingFor = nil
	}
	c.waitMu.Unlock()
}

func (c *Container) waitPath(from, target *factoryCall, seen map[*factoryCall]struct{}) []reflect.Type {
	if from == target {
		return []reflect.Type{from.serviceType}
	}
	if _, exists := seen[from]; exists {
		return nil
	}
	seen[from] = struct{}{}
	defer delete(seen, from)

	for next := range from.waitingFor {
		if path := c.waitPath(next, target, seen); path != nil {
			return append([]reflect.Type{from.serviceType}, path...)
		}
	}
	return nil
}

func typeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

func isNil(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	//nolint:exhaustive // Only kinds accepted by reflect.Value.IsNil belong in this case.
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
