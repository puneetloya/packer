package hcl2template

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/packer/helper/common"
	"github.com/hashicorp/packer/packer"
	"github.com/zclconf/go-cty/cty"
)

// PackerConfig represents a loaded Packer HCL config. It will contain
// references to all possible blocks of the allowed configuration.
type PackerConfig struct {
	// Directory where the config files are defined
	Basedir string

	// Available Source blocks
	Sources map[SourceRef]*SourceBlock

	// InputVariables and LocalVariables are the list of defined input and
	// local variables. They are of the same type but are not used in the same
	// way. Local variables will not be decoded from any config file, env var,
	// or ect. Like the Input variables will.
	InputVariables Variables
	LocalVariables Variables

	// Builds is the list of Build blocks defined in the config files.
	Builds Builds
}

// EvalContext returns the *hcl.EvalContext that will be passed to an hcl
// decoder in order to tell what is the actual value of a var or a local and
// the list of defined functions.
func (cfg *PackerConfig) EvalContext() *hcl.EvalContext {
	ectx := &hcl.EvalContext{
		Functions: Functions(cfg.Basedir),
		Variables: map[string]cty.Value{
			"var":   cty.ObjectVal(cfg.InputVariables.Values()),
			"local": cty.ObjectVal(cfg.LocalVariables.Values()),
		},
	}
	return ectx
}

// decodeInputVariables looks in the found blocks for 'variables' and
// 'variable' blocks. It should be called firsthand so that other blocks can
// use the variables.
func (c *PackerConfig) decodeInputVariables(f *hcl.File) hcl.Diagnostics {
	var diags hcl.Diagnostics

	content, moreDiags := f.Body.Content(configSchema)
	diags = append(diags, moreDiags...)

	for _, block := range content.Blocks {
		switch block.Type {
		case variableLabel:
			moreDiags := c.InputVariables.decodeVariableBlock(block, nil)
			diags = append(diags, moreDiags...)
		case variablesLabel:
			attrs, moreDiags := block.Body.JustAttributes()
			diags = append(diags, moreDiags...)
			for key, attr := range attrs {
				moreDiags = c.InputVariables.decodeVariable(key, attr, nil)
				diags = append(diags, moreDiags...)
			}
		}
	}
	return diags
}

// parseLocalVariables looks in the found blocks for 'locals' blocks. It
// should be called after parsing input variables so that they can be
// referenced.
func (c *PackerConfig) parseLocalVariables(f *hcl.File) ([]*Local, hcl.Diagnostics) {
	var diags hcl.Diagnostics

	content, moreDiags := f.Body.Content(configSchema)
	diags = append(diags, moreDiags...)
	var allLocals []*Local

	for _, block := range content.Blocks {
		switch block.Type {
		case localsLabel:
			attrs, moreDiags := block.Body.JustAttributes()
			diags = append(diags, moreDiags...)
			locals := make([]*Local, 0, len(attrs))
			for name, attr := range attrs {
				if _, found := c.LocalVariables[name]; found {
					diags = append(diags, &hcl.Diagnostic{
						Severity: hcl.DiagError,
						Summary:  "Duplicate variable",
						Detail:   "Duplicate " + name + " variable definition found.",
						Subject:  attr.NameRange.Ptr(),
						Context:  block.DefRange.Ptr(),
					})
					return nil, diags
				}
				locals = append(locals, &Local{
					Name: name,
					Expr: attr.Expr,
				})
			}
			allLocals = append(allLocals, locals...)
		}
	}

	return allLocals, diags
}

func (c *PackerConfig) evaluateLocalVariables(locals []*Local) hcl.Diagnostics {
	var diags hcl.Diagnostics

	if len(locals) > 0 && c.LocalVariables == nil {
		c.LocalVariables = Variables{}
	}

	var retry, previousL int
	for len(locals) > 0 {
		local := locals[0]
		moreDiags := c.evaluateLocalVariable(local)
		if moreDiags.HasErrors() {
			if len(locals) == 1 {
				// If this is the only local left there's no need
				// to try evaluating again
				return append(diags, moreDiags...)
			}
			if previousL == len(locals) {
				if retry == 100 {
					// To get to this point, locals must have a circle dependency
					return append(diags, moreDiags...)
				}
				retry++
			}
			previousL = len(locals)

			// If local uses another local that has not been evaluated yet this could be the reason of errors
			// Push local to the end of slice to be evaluated later
			locals = append(locals, local)
		} else {
			retry = 0
			diags = append(diags, moreDiags...)
		}
		// Remove local from slice
		locals = append(locals[:0], locals[1:]...)
	}

	return diags
}

func (c *PackerConfig) evaluateLocalVariable(local *Local) hcl.Diagnostics {
	var diags hcl.Diagnostics

	value, moreDiags := local.Expr.Value(c.EvalContext())
	diags = append(diags, moreDiags...)
	if moreDiags.HasErrors() {
		return diags
	}
	c.LocalVariables[local.Name] = &Variable{
		DefaultValue: value,
		Type:         value.Type(),
	}

	return diags
}

// getCoreBuildProvisioners takes a list of provisioner block, starts according
// provisioners and sends parsed HCL2 over to it.
func (p *Parser) getCoreBuildProvisioners(blocks []*ProvisionerBlock, ectx *hcl.EvalContext, generatedVars map[string]string) ([]packer.CoreBuildProvisioner, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	res := []packer.CoreBuildProvisioner{}
	for _, pb := range blocks {
		provisioner, moreDiags := p.startProvisioner(pb, ectx, generatedVars)
		diags = append(diags, moreDiags...)
		if moreDiags.HasErrors() {
			continue
		}
		res = append(res, packer.CoreBuildProvisioner{
			PType:       pb.PType,
			PName:       pb.PName,
			Provisioner: provisioner,
		})
	}
	return res, diags
}

// getCoreBuildProvisioners takes a list of post processor block, starts
// according provisioners and sends parsed HCL2 over to it.
func (p *Parser) getCoreBuildPostProcessors(blocks []*PostProcessorBlock, ectx *hcl.EvalContext, generatedVars map[string]string) ([]packer.CoreBuildPostProcessor, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	res := []packer.CoreBuildPostProcessor{}
	for _, ppb := range blocks {
		postProcessor, moreDiags := p.startPostProcessor(ppb, ectx, generatedVars)
		diags = append(diags, moreDiags...)
		if moreDiags.HasErrors() {
			continue
		}
		res = append(res, packer.CoreBuildPostProcessor{
			PostProcessor: postProcessor,
			PName:         ppb.PName,
			PType:         ppb.PType,
		})
	}

	return res, diags
}

// getBuilds will return a list of packer Build based on the HCL2 parsed build
// blocks. All Builders, Provisioners and Post Processors will be started and
// configured.
func (p *Parser) getBuilds(cfg *PackerConfig) ([]packer.Build, hcl.Diagnostics) {
	res := []packer.Build{}
	var diags hcl.Diagnostics

	for _, build := range cfg.Builds {
		for _, from := range build.Sources {
			src, found := cfg.Sources[from]
			if !found {
				diags = append(diags, &hcl.Diagnostic{
					Summary:  "Unknown " + sourceLabel + " " + from.String(),
					Subject:  build.HCL2Ref.DefRange.Ptr(),
					Severity: hcl.DiagError,
				})
				continue
			}
			builder, moreDiags, generatedVars := p.startBuilder(src, cfg.EvalContext())
			diags = append(diags, moreDiags...)
			if moreDiags.HasErrors() {
				continue
			}

			// If the builder has provided a list of to-be-generated variables that
			// should be made accessible to provisioners, pass that list into
			// the provisioner prepare() so that the provisioner can appropriately
			// validate user input against what will become available. Otherwise,
			// only pass the default variables, using the basic placeholder data.
			generatedPlaceholderMap := packer.BasicPlaceholderData()
			if generatedVars != nil {
				for _, k := range generatedVars {
					generatedPlaceholderMap[k] = fmt.Sprintf("Build_%s. "+
						common.PlaceholderMsg, k)
				}
			}

			provisioners, moreDiags := p.getCoreBuildProvisioners(build.ProvisionerBlocks, cfg.EvalContext(), generatedPlaceholderMap)
			diags = append(diags, moreDiags...)
			if moreDiags.HasErrors() {
				continue
			}
			postProcessors, moreDiags := p.getCoreBuildPostProcessors(build.PostProcessors, cfg.EvalContext(), generatedPlaceholderMap)
			pps := [][]packer.CoreBuildPostProcessor{}
			if len(postProcessors) > 0 {
				pps = [][]packer.CoreBuildPostProcessor{postProcessors}
			}
			diags = append(diags, moreDiags...)
			if moreDiags.HasErrors() {
				continue
			}

			pcb := &packer.CoreBuild{
				Type:           src.Type,
				Builder:        builder,
				Provisioners:   provisioners,
				PostProcessors: pps,
				Prepared:       true,
			}
			res = append(res, pcb)
		}
	}
	return res, diags
}

// Parse will parse HCL file(s) in path. Path can be a folder or a file.
//
// Parse will first parse variables and then the rest; so that interpolation
// can happen.
//
// For each build block a packer.Build will be started, and for each builder,
// all provisioners and post-processors will be started.
//
// Parse then return a slice of packer.Builds; which are what packer core uses
// to run builds.
func (p *Parser) Parse(path string, vars map[string]string) ([]packer.Build, hcl.Diagnostics) {
	cfg, diags := p.parse(path, vars)
	if diags.HasErrors() {
		return nil, diags
	}

	builds, moreDiags := p.getBuilds(cfg)
	return builds, append(diags, moreDiags...)
}
