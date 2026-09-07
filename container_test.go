package dis_test

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qattidev/dis"
)

type userService struct {
	name string
}

type userRepository interface {
	findUser() string
}

type postgresRepository struct{}

func (*postgresRepository) findUser() string { return "Ada" }

type serviceWithRepository struct {
	repository userRepository
}

type circularA struct{}
type circularB struct{}
type defaultOnlyService struct{}

func TestPackageLevelAPIUsesDefaultContainer(t *testing.T) {
	if os.Getenv("DIS_TEST_DEFAULT_CONTAINER") != "1" {
		command := exec.Command(os.Args[0], "-test.run=^TestPackageLevelAPIUsesDefaultContainer$")
		command.Env = append(os.Environ(), "DIS_TEST_DEFAULT_CONTAINER=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("run isolated default-container test: %v\n%s", err, output)
		}
		return
	}

	expected := &defaultOnlyService{}
	dis.MustRegisterService(expected)
	dis.Seal()

	actual, err := dis.GetService[*defaultOnlyService]()
	if err != nil {
		t.Fatalf("default-container lookup: %v", err)
	}
	if actual != expected {
		t.Fatalf("default-container lookup returned a different singleton")
	}
}

func TestDirectServiceRegistration(t *testing.T) {
	container := dis.NewContainer()
	expected := &userService{name: "users"}

	dis.MustRegisterServiceIn(container, expected)
	container.Seal()

	first, err := dis.GetServiceFrom[*userService](container)
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	second, err := dis.GetServiceFrom[*userService](container)
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}
	if first != expected || second != expected {
		t.Fatalf("expected both lookups to return the registered singleton")
	}
}

func TestFactoryResolvesInterfaceDependency(t *testing.T) {
	container := dis.NewContainer()
	repository := &postgresRepository{}
	dis.MustRegisterServiceIn[userRepository](container, repository)
	dis.MustRegisterFactoryIn(container, func(resolver dis.Resolver) (*serviceWithRepository, error) {
		dependency, err := dis.GetServiceFrom[userRepository](resolver)
		if err != nil {
			return nil, err
		}
		return &serviceWithRepository{repository: dependency}, nil
	})
	container.Seal()

	service, err := dis.GetServiceFrom[*serviceWithRepository](container)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if service.repository != repository {
		t.Fatalf("factory received %T; want registered repository", service.repository)
	}
}

func TestFactoryConstructsOnceForConcurrentLookups(t *testing.T) {
	container := dis.NewContainer()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	dis.MustRegisterFactoryIn(container, func(dis.Resolver) (*userService, error) {
		calls.Add(1)
		close(started)
		<-release
		return &userService{name: "constructed"}, nil
	})
	container.Seal()

	const workers = 20
	results := make([]*userService, workers)
	errs := make([]error, workers)
	var work sync.WaitGroup
	work.Add(workers)
	for i := range workers {
		go func() {
			defer work.Done()
			results[i], errs[i] = dis.GetServiceFrom[*userService](container)
		}()
	}

	<-started
	close(release)
	work.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("factory calls = %d, want 1", got)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("lookup %d: %v", i, err)
		}
		if results[i] != results[0] {
			t.Fatalf("lookup %d returned a different singleton", i)
		}
	}
}

func TestFactoryErrorIsRetried(t *testing.T) {
	container := dis.NewContainer()
	transient := errors.New("database unavailable")
	var calls atomic.Int32

	dis.MustRegisterFactoryIn(container, func(dis.Resolver) (*userService, error) {
		if calls.Add(1) == 1 {
			return nil, transient
		}
		return &userService{name: "available"}, nil
	})
	container.Seal()

	_, err := dis.GetServiceFrom[*userService](container)
	if !errors.Is(err, transient) {
		t.Fatalf("first lookup error = %v, want wrapped %v", err, transient)
	}

	service, err := dis.GetServiceFrom[*userService](container)
	if err != nil {
		t.Fatalf("retry lookup: %v", err)
	}
	if service.name != "available" {
		t.Fatalf("retried service = %#v", service)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("factory calls = %d, want 2", got)
	}
}

func TestLookupRequiresSealingAndReportsMissingService(t *testing.T) {
	container := dis.NewContainer()
	dis.MustRegisterServiceIn(container, &userService{})

	_, err := dis.GetServiceFrom[*userService](container)
	if !errors.Is(err, dis.ErrContainerNotSealed) {
		t.Fatalf("pre-seal lookup error = %v", err)
	}

	container.Seal()
	_, err = dis.GetServiceFrom[*postgresRepository](container)
	if !errors.Is(err, dis.ErrServiceNotFound) {
		t.Fatalf("missing lookup error = %v", err)
	}
	var missing *dis.ServiceNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("missing lookup did not return ServiceNotFoundError: %v", err)
	}
}

func TestInvalidRegistrationPanics(t *testing.T) {
	container := dis.NewContainer()
	dis.MustRegisterServiceIn(container, &userService{})

	assertPanicsWith(t, dis.ErrServiceAlreadyRegistered, func() {
		dis.MustRegisterServiceIn(container, &userService{})
	})

	container.Seal()
	assertPanicsWith(t, dis.ErrContainerSealed, func() {
		dis.MustRegisterServiceIn(container, &postgresRepository{})
	})

	other := dis.NewContainer()
	assertPanicsWith(t, dis.ErrNilService, func() {
		dis.MustRegisterServiceIn[*userService](other, nil)
	})
}

func TestCircularFactoryDependencyIsReported(t *testing.T) {
	container := dis.NewContainer()
	dis.MustRegisterFactoryIn(container, func(resolver dis.Resolver) (*circularA, error) {
		_, err := dis.GetServiceFrom[*circularB](resolver)
		if err != nil {
			return nil, err
		}
		return &circularA{}, nil
	})
	dis.MustRegisterFactoryIn(container, func(resolver dis.Resolver) (*circularB, error) {
		_, err := dis.GetServiceFrom[*circularA](resolver)
		if err != nil {
			return nil, err
		}
		return &circularB{}, nil
	})
	container.Seal()

	_, err := dis.GetServiceFrom[*circularA](container)
	if !errors.Is(err, dis.ErrCircularDependency) {
		t.Fatalf("cycle lookup error = %v", err)
	}
	var circular *dis.CircularDependencyError
	if !errors.As(err, &circular) {
		t.Fatalf("cycle lookup did not return CircularDependencyError: %v", err)
	}
	if len(circular.Path) != 3 {
		t.Fatalf("cycle path length = %d, want 3", len(circular.Path))
	}
}

func TestConcurrentCircularFactoryDependencyIsReported(t *testing.T) {
	container := dis.NewContainer()
	aStarted := make(chan struct{})
	bStarted := make(chan struct{})
	release := make(chan struct{})

	dis.MustRegisterFactoryIn(container, func(resolver dis.Resolver) (*circularA, error) {
		close(aStarted)
		<-release
		_, err := dis.GetServiceFrom[*circularB](resolver)
		if err != nil {
			return nil, err
		}
		return &circularA{}, nil
	})
	dis.MustRegisterFactoryIn(container, func(resolver dis.Resolver) (*circularB, error) {
		close(bStarted)
		<-release
		_, err := dis.GetServiceFrom[*circularA](resolver)
		if err != nil {
			return nil, err
		}
		return &circularB{}, nil
	})
	container.Seal()

	errorsFromRoots := make(chan error, 2)
	go func() {
		_, err := dis.GetServiceFrom[*circularA](container)
		errorsFromRoots <- err
	}()
	go func() {
		_, err := dis.GetServiceFrom[*circularB](container)
		errorsFromRoots <- err
	}()

	<-aStarted
	<-bStarted
	close(release)

	for range 2 {
		select {
		case err := <-errorsFromRoots:
			if !errors.Is(err, dis.ErrCircularDependency) {
				t.Fatalf("concurrent cycle error = %v, want circular dependency", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent cycle resolution deadlocked")
		}
	}
}

func TestPanickingFactoryReleasesTheEntryForRetry(t *testing.T) {
	container := dis.NewContainer()
	var calls atomic.Int32
	dis.MustRegisterFactoryIn(container, func(dis.Resolver) (*userService, error) {
		if calls.Add(1) == 1 {
			panic("factory failed")
		}
		return &userService{name: "retried"}, nil
	})
	container.Seal()

	assertPanics(t, func() {
		_, _ = dis.GetServiceFrom[*userService](container)
	})

	service, err := dis.GetServiceFrom[*userService](container)
	if err != nil {
		t.Fatalf("retry lookup: %v", err)
	}
	if service.name != "retried" {
		t.Fatalf("retried service = %#v", service)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("factory calls = %d, want 2", got)
	}
}

func assertPanicsWith(t *testing.T, expected error, fn func()) {
	t.Helper()
	defer func() {
		value := recover()
		if value == nil {
			t.Fatalf("function did not panic")
		}
		err, ok := value.(error)
		if !ok || !errors.Is(err, expected) {
			t.Fatalf("panic = %#v, want error wrapping %v", value, expected)
		}
	}()
	fn()
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("function did not panic")
		}
	}()
	fn()
}
