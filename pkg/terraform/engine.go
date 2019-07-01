package terraform

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/apex/log"
	"github.com/avinor/tau/pkg/config"
	"github.com/avinor/tau/pkg/ctytree"
	"github.com/avinor/tau/pkg/paths"
	"github.com/avinor/tau/pkg/terraform/def"
	v012 "github.com/avinor/tau/pkg/terraform/v012"
	"github.com/fatih/color"
	"github.com/zclconf/go-cty/cty"
)

// Engine to process
type Engine struct {
	Version string

	Compatibility def.VersionCompatibility
	Generator     def.Generator
	Processor     def.Processor
	Executor      def.Executor
}

// NewEngine creates a terraform engine for the currently installed terraform version
func NewEngine() *Engine {

	version := Version()

	if version == "" {
		log.Fatal(color.RedString("Could not identify terraform version. Make sure terraform is in PATH."))
	}

	log.Debug(color.New(color.Bold).Sprintf("Terraform version: %s", version))
	log.Debug("")

	var compatibility def.VersionCompatibility
	var generator def.Generator
	var processor def.Processor
	var executor def.Executor

	switch {
	case strings.HasPrefix(version, "0.12"):
		v012Engine := v012.NewEngine()
		compatibility = v012Engine
		generator = v012Engine
		processor = v012Engine
		executor = v012Engine
	default:
		log.Fatal(color.RedString("Unsupported terraform version!"))
	}

	return &Engine{
		Version:       version,
		Compatibility: compatibility,
		Generator:     generator,
		Processor:     processor,
		Executor:      executor,
	}
}

// CreateOverrides create the tau_override file in module folder. This file will overide
// backend settings
func (e *Engine) CreateOverrides(source *config.Source, dest string) error {
	log.Debug(color.New(color.Bold).Sprint("Creating overrides..."))

	content, create, err := e.Generator.GenerateOverrides(source)

	if err != nil {
		return err
	}

	if !create {
		return nil
	}

	file := filepath.Join(dest, "tau_override.tf")

	return ioutil.WriteFile(file, content, os.ModePerm)
}

// ResolveDependencies processes the source file and generates terraform modules for each unique
// source. For each source it will generate output arguments and return the merged values
func (e *Engine) ResolveDependencies(source *config.Source, dest string) (map[string]cty.Value, error) {
	processors, create, err := e.Generator.GenerateDependencies(source)

	if err != nil {
		return nil, err
	}

	if !create {
		return nil, nil
	}

	values := map[string]cty.Value{}

	for _, proc := range processors {
		procDest := filepath.Join(dest, proc.Name())
		paths.EnsureDirectoryExists(procDest)

		file := filepath.Join(procDest, "main.tf")
		if err := ioutil.WriteFile(file, proc.Content(), os.ModePerm); err != nil {
			return nil, err
		}

		vals, err := proc.Process(procDest)
		if err != nil {
			return nil, err
		}

		for key, value := range vals {
			values[key] = value
		}
	}

	return ctytree.CreateTree(values).ToCtyMap(), nil
}

// WriteInputVariables write the terraform.tfvars file into module folder. This file is the parsed and
// processed variables where all dependencies and data source have been resolved and replaced with real
// values
func (e *Engine) WriteInputVariables(source *config.Source, dest string, variables map[string]cty.Value) error {
	content, err := e.Generator.GenerateVariables(source, variables)

	if err != nil {
		return err
	}

	file := filepath.Join(dest, "terraform.tfvars")

	return ioutil.WriteFile(file, content, os.ModePerm)
}
