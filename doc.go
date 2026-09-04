// Package dis provides a small, type-safe singleton service registry.
//
// Applications normally register services while their packages initialize,
// seal the global container during startup, and resolve services afterwards:
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
package dis
