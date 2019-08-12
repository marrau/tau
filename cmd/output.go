package cmd

import (
	"fmt"
	"strings"

	"github.com/avinor/tau/internal/templates"
	"github.com/avinor/tau/pkg/config/loader"
	"github.com/avinor/tau/pkg/helpers/paths"
	"github.com/avinor/tau/pkg/helpers/ui"
	"github.com/avinor/tau/pkg/hooks"
	"github.com/avinor/tau/pkg/shell"
	"github.com/avinor/tau/pkg/shell/processors"
	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type outputCmd struct {
	meta

	loader *loader.Loader

	output string
}

var (
	validOutputFormats = []string{"json", "yaml", "env", "plain"}

	// outputRequiresSingleFile is returned if using output argument and it tries to process multiple files
	outputRequiresSingleFile = errors.Errorf("can only process a single file when using output argument")

	// outputRequiresSingleFile is returned if using output argument and it tries to process multiple files
	invalidOutputFormat = errors.Errorf("invalid output format. Valid formats are %s", validOutputFormats)

	// outputLong is long description of output command
	outputLong = templates.LongDesc(`Print all the output variables from a module.
		If including the --output flag it will print output in specified format.
		It supports json, yaml, env and plain as output format
		`)

	// outputExample is examples for output command
	outputExample = templates.Examples(`
		# Combine output from all files
		tau output

		# Print output from module.hcl in json format
		tau output -f module.hcl --output json
	`)
)

// newOutputCmd creates a new output command
func newOutputCmd() *cobra.Command {
	oc := &outputCmd{}

	outputCmd := &cobra.Command{
		Use:                   "output [-f SORUCE]",
		Short:                 "Print output from module",
		Long:                  outputLong,
		Example:               outputExample,
		DisableFlagsInUseLine: true,
		SilenceUsage:          true,
		SilenceErrors:         true,
		Args:                  cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oc.meta.processArgs(args); err != nil {
				return err
			}

			if err := oc.processArgs(args); err != nil {
				return err
			}

			oc.init()

			return oc.run(args)
		},
	}

	f := outputCmd.Flags()
	f.StringVarP(&oc.output, "output", "o", "plain", "output format of variables")

	oc.addMetaFlags(outputCmd)

	return outputCmd
}

func (oc *outputCmd) init() {
	{
		options := &loader.Options{
			WorkingDirectory: paths.WorkingDir,
			TauDirectory:     oc.TauDir,
			MaxDepth:         1,
		}

		oc.loader = loader.New(options)
	}
}

// processArgs process arguments and checks for invalid options or combination of arguments
func (oc *outputCmd) processArgs(args []string) error {

	oc.output = strings.ToLower(oc.output)

	valid := false
	for _, format := range validOutputFormats {
		if format == oc.output {
			valid = true
		}
	}

	if !valid {
		return invalidOutputFormat
	}

	return nil
}

func (oc *outputCmd) shouldProcessOutput() bool {
	return oc.output != "plain"
}

func (oc *outputCmd) run(args []string) error {
	files, err := oc.loader.Load(oc.file)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		ui.NewLine()
		ui.Warn("No sources found")
		return nil
	}

	// if source defined then it can only deploy a single file, not folder
	if len(files) > 1 && oc.shouldProcessOutput() {
		return outputRequiresSingleFile
	}

	// Verify all modules have been initialized
	if err := files.IsAllInitialized(); err != nil {
		return err
	}

	if err := hooks.RunAll(files, "prepare", "output"); err != nil {
		return err
	}

	// Check if any plans exist, if not then run plan first
	noVariablesExists := true
	for _, file := range files {
		if paths.IsFile(file.VariableFile()) {
			noVariablesExists = false
			continue
		}
	}

	if noVariablesExists {
		oc.resolveDependencies(files)
	}

	var values map[string]cty.Value

	for _, file := range files {
		ui.Separator(file.Name)

		if !paths.IsFile(file.VariableFile()) {
			ui.Warn("No values file exists for %s", file.Name)
			continue
		}

		outputProcessor := oc.Engine.Executor.NewOutputProcessor()

		options := &shell.Options{
			WorkingDirectory: file.ModuleDir(),
			Stdout:           shell.Processors(outputProcessor),
			Stderr:           shell.Processors(processors.NewUI(ui.Error)),
			Env:              file.Env,
		}

		if !oc.shouldProcessOutput() {
			options.Stdout = append(options.Stdout, processors.NewUI(ui.Info))
		}

		extraArgs := getExtraArgs(oc.Engine.Compatibility.GetInvalidArgs("output")...)

		if oc.shouldProcessOutput() {
			extraArgs = append(extraArgs, "-json")
		}

		if err := oc.Engine.Executor.Execute(options, "output", extraArgs...); err != nil {
			return err
		}

		if oc.shouldProcessOutput() {
			output, err := outputProcessor.GetOutput()
			if err != nil {
				return err
			}
			values = output
		}

		paths.Remove(file.VariableFile())
	}

	ui.Separator("")

	if err := hooks.RunAll(files, "finish", "output"); err != nil {
		return err
	}

	ui.NewLine()

	switch oc.output {
	case "json":
		return printJSON(values)
	case "yaml":
		return printYAML(values)
	case "env":
		return printEnv(values)
	default:
		return nil
	}
}

func printJSON(values map[string]cty.Value) error {
	obj := cty.ObjectVal(values)
	bytes, err := ctyjson.Marshal(obj, obj.Type())
	if err != nil {
		return err
	}

	ui.Output("%v", string(bytes))

	return nil
}

func printYAML(values map[string]cty.Value) error {
	obj := cty.ObjectVal(values)
	bytes, err := ctyjson.Marshal(obj, obj.Type())
	if err != nil {
		return err
	}

	yamlBytes, err := yaml.JSONToYAML(bytes)
	if err != nil {
		return err
	}

	ui.Output("%v", string(yamlBytes))

	return nil
}

func printEnv(values map[string]cty.Value) error {
	obj := cty.ObjectVal(values)

	for k, v := range flattenValue(obj, "TAU") {
		ui.Output("%s=\"%s\"", k, v)
	}

	return nil
}

func flattenValue(value cty.Value, prefix string) map[string]string {
	values := map[string]string{}

	for k, v := range value.AsValueMap() {
		if v.CanIterateElements() {
			for fk, fv := range flattenValue(v, k) {
				key := strings.ToUpper(fmt.Sprintf("%s_%s", prefix, fk))
				values[key] = fv
			}
		} else {
			key := strings.ToUpper(fmt.Sprintf("%s_%s", prefix, k))
			values[key] = v.AsString()
		}
	}

	return values
}