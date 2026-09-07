package dis_test

import (
	"fmt"

	"github.com/qattidev/dis"
)

type exampleGreeter interface {
	Greet() string
}

type exampleEnglishGreeter struct{}

func (exampleEnglishGreeter) Greet() string { return "hello" }

func ExampleContainer() {
	container := dis.NewContainer()
	dis.MustRegisterServiceIn[exampleGreeter](container, exampleEnglishGreeter{})
	container.Seal()

	greeter, err := dis.GetServiceFrom[exampleGreeter](container)
	if err != nil {
		panic(err)
	}
	fmt.Println(greeter.Greet())

	// Output: hello
}
