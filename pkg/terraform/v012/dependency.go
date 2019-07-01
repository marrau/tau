package v012

import (
	"path/filepath"

	"github.com/apex/log"
	"github.com/avinor/tau/pkg/config"
	"github.com/avinor/tau/pkg/hooks"
	"github.com/avinor/tau/pkg/shell"
	"github.com/avinor/tau/pkg/shell/processors"
	"github.com/hashicorp/hcl2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

type dependencyProcessor struct {
	Source *config.Source
	File   *hclwrite.File

	executor *Executor
	resolver *Resolver
}

func NewDependencyProcessor(source *config.Source, executor *Executor, resolver *Resolver) *dependencyProcessor {
	f := hclwrite.NewEmptyFile()

	return &dependencyProcessor{
		Source: source,
		File:   f,

		executor: executor,
		resolver: resolver,
	}
}

func (d *dependencyProcessor) Name() string {
	return d.Source.Name
}

func (d *dependencyProcessor) Content() []byte {
	return d.File.Bytes()
}

func (d *dependencyProcessor) Process(dest string) (map[string]cty.Value, error) {
	debugLog := &processors.Log{
		Debug: true,
	}
	errorLog := &processors.Log{}

	if err := hooks.Run(d.Source, "prepare", "init"); err != nil {
		return nil, err
	}

	options := &shell.Options{
		Stdout:           shell.Processors(debugLog),
		Stderr:           shell.Processors(errorLog),
		WorkingDirectory: dest,
		Env:              d.Source.Env,
	}

	base := filepath.Base(dest)

	log.Infof("- Running terraform init on %s", base)
	if err := d.executor.Execute(options, "init"); err != nil {
		return nil, err
	}

	log.Infof("- Running terraform apply on %s", base)
	if err := d.executor.Execute(options, "apply"); err != nil {
		return nil, err
	}

	buffer := &processors.Buffer{}
	options.Stdout = shell.Processors(buffer)

	log.Infof("- Reading output from %s", base)
	if err := d.executor.Execute(options, "output", "-json"); err != nil {
		return nil, err
	}

	return d.resolver.ResolveStateOutput([]byte(buffer.Stdout()))
}
