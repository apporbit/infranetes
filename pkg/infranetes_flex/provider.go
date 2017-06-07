package infranetes_flex

import (
	"fmt"
)

type DevProvider interface {
	Provision(size uint64) (id *string, e error)
	Attach(id *string) (dev *string, e error)
	Detach(id *string) error
}

var (
	DevProviders devProviderRegistry
)

func init() {
	DevProviders.devProviderMap = make(map[string]func() (DevProvider, error))
}

type devProviderRegistry struct {
	devProviderMap map[string]func() (DevProvider, error)
}

func (p devProviderRegistry) RegisterProvider(name string, provider func() (DevProvider, error)) error {
	if _, ok := p.devProviderMap[name]; ok == true {
		return fmt.Errorf("%v already registered as a provider", name)
	}

	p.devProviderMap[name] = provider

	return nil
}

func (p devProviderRegistry) findProvider(name string) (func() (DevProvider, error), error) {
	if provider, ok := p.devProviderMap[name]; ok == true {
		return provider, nil
	}

	return nil, fmt.Errorf("%v is an unknown provider", name)
}

func NewDevProvider(provider string) (DevProvider, error) {
	podProvider, err := DevProviders.findProvider(provider)
	if err != nil {
		return nil, err
	}

	return podProvider()
}
