// Package dis provides a small, type-safe singleton service registry.
//
// Applications normally register services while their packages initialize,
// seal the global container during startup, and resolve required services before
// accepting traffic. Sealing freezes registrations; it does not construct
// services or validate the dependency graph:
//
//	func init() {
//		dis.MustRegisterService(&UserService{})
//	}
//
//	func main() {
//		dis.Seal()
//		service, err := dis.GetService[*UserService]()
//		// Handle err.
//		_ = service
//	}
//
// Factories receive a Resolver so that their dependencies are resolved from
// the same container and circular dependency chains can be reported. Factories
// should use GetServiceFrom with that resolver rather than the global getter.
// Service resolution is safe for concurrent use, but registered services are
// responsible for their own synchronization and cleanup.
package dis
