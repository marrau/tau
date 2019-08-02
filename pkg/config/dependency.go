package config

import "github.com/pkg/errors"

var (
	dependencySourceMustBeSet = errors.Errorf("dependency source must be set")
)

// Dependency towards another tau deployment. Source can either be a relative / absolute path
// (start with . or / in that case) to a file or a directory.
//
// For each dependency it will create a remote_state data source to retrieve the values from
// dependency. Backend configuration will be read from the dependency file. To override attributes
// define the backend block in dependency and only define the attributes that should be overridden.
// For instance it can be useful to override token attribute if current module and dependency module
// use different token's for authentication
type Dependency struct {
	Name   string `hcl:"name,label"`
	Source string `hcl:"source,attr"`

	Backend *Backend `hcl:"backend,block"`
}

// Merge dependency with source dependency.
func (d *Dependency) Merge(src *Dependency) error {
	if src == nil {
		return nil
	}

	// do not merge dependencies that do not match
	if d.Name != src.Name {
		return nil
	}

	if src.Source != "" {
		d.Source = src.Source
	}

	if d.Backend == nil && src.Backend != nil {
		d.Backend = src.Backend
		return nil
	}

	if err := d.Backend.Merge(src.Backend); err != nil {
		return err
	}

	return nil
}

// Validate that source is set on dependency.
func (d *Dependency) Validate() (bool, error) {
	if d.Source == "" {
		return false, dependencySourceMustBeSet
	}

	return true, nil
}
