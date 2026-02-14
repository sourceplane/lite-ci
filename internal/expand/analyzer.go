package expand

import (
	"github.com/sourceplane/liteci/internal/model"
)

// ComponentAnalyzer provides analysis of components and their resolved properties
type ComponentAnalyzer struct {
	expander *Expander
	instances map[string][]*model.ComponentInstance
}

// NewComponentAnalyzer creates a new component analyzer
func NewComponentAnalyzer(normalized *model.NormalizedIntent) *ComponentAnalyzer {
	expander := NewExpander(normalized)
	return &ComponentAnalyzer{
		expander: expander,
	}
}

// AnalyzeAll expands all components and returns merged instances
func (ca *ComponentAnalyzer) AnalyzeAll() (map[string][]*model.ComponentInstance, error) {
	return ca.expander.Expand()
}

// GetComponent returns merged component data for a specific component across all environments
type ComponentMerged struct {
	Name         string
	Type         string
	Domain       string
	Enabled      bool
	Instances    []*model.ComponentInstance
	Dependencies []string
}

// GetComponentByName returns merged component info for a single component
func (ca *ComponentAnalyzer) GetComponentByName(compName string) (*ComponentMerged, error) {
	instances, err := ca.AnalyzeAll()
	if err != nil {
		return nil, err
	}

	comp := &ComponentMerged{
		Name:         compName,
		Instances:    make([]*model.ComponentInstance, 0),
		Dependencies: make([]string, 0),
	}

	// Collect all instances of this component across environments
	for _, envInstances := range instances {
		for _, inst := range envInstances {
			if inst.ComponentName == compName {
				// Set component-level info from first instance
				if comp.Type == "" {
					comp.Type = inst.Type
					comp.Domain = inst.Domain
					comp.Enabled = inst.Enabled

					// Collect dependencies
					for _, dep := range inst.DependsOn {
						comp.Dependencies = append(comp.Dependencies, dep.ComponentName)
					}
				}
				comp.Instances = append(comp.Instances, inst)
			}
		}
	}

	return comp, nil
}

// ListAll lists all components with their merged properties
func (ca *ComponentAnalyzer) ListAll() ([]*ComponentMerged, error) {
	instances, err := ca.AnalyzeAll()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var result []*ComponentMerged

	// Collect unique component names
	for _, envInstances := range instances {
		for _, inst := range envInstances {
			if !seen[inst.ComponentName] {
				seen[inst.ComponentName] = true
				comp, _ := ca.GetComponentByName(inst.ComponentName)
				result = append(result, comp)
			}
		}
	}

	return result, nil
}
