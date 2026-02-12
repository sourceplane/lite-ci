package expand

import (
	"github.com/sourceplane/liteci/internal/model"
)

// Expander handles environment × component expansion and merging
type Expander struct {
	normalized *model.NormalizedIntent
	groups     map[string]model.Group
}

// NewExpander creates a new expander
func NewExpander(normalized *model.NormalizedIntent) *Expander {
	return &Expander{
		normalized: normalized,
		groups:     normalized.Groups,
	}
}

// Expand produces ComponentInstances for each environment × component pair
func (e *Expander) Expand() (map[string][]*model.ComponentInstance, error) {
	result := make(map[string][]*model.ComponentInstance)

	for envName, env := range e.normalized.Environments {
		instances := make([]*model.ComponentInstance, 0)

		// Get applicable components for this environment
		applicableComps := e.getApplicableComponents(env)

		for _, compName := range applicableComps {
			comp, exists := e.normalized.ComponentIndex[compName]
			if !exists {
				continue
			}

			// Skip disabled components
			if !comp.Enabled {
				continue
			}

			// Create instance
			instance := &model.ComponentInstance{
				ComponentName: compName,
				Environment:   envName,
				Type:          comp.Type,
				Domain:        comp.Domain,
				Labels:        comp.Labels,
				Enabled:       comp.Enabled,
			}

			// Merge inputs and policies
			merged := e.mergeInputs(comp, env, envName)
			instance.Inputs = merged

			// Extract and apply policies (cannot be overridden)
			instance.Policies = e.resolvePolicies(comp, envName)

			// Resolve dependencies
			deps := e.resolveDependencies(comp, envName)
			instance.DependsOn = deps

			instances = append(instances, instance)
		}

		result[envName] = instances
	}

	return result, nil
}

// getApplicableComponents returns components that apply to an environment
func (e *Expander) getApplicableComponents(env model.ForEach) []string {
	return env.Selectors.Components
}

// mergeInputs applies the merge precedence order
func (e *Expander) mergeInputs(comp model.Component, env model.ForEach, envName string) map[string]interface{} {
	merged := make(map[string]interface{})

	// 1. Type defaults (empty for now, could come from schema)
	// 2. Group defaults (from domain)
	if comp.Domain != "" {
		if group, exists := e.groups[comp.Domain]; exists {
			if group.Defaults != nil {
				for k, v := range group.Defaults {
					merged[k] = v
				}
			}
		}
	}

	// 3. Environment defaults
	if env.Defaults != nil {
		for k, v := range env.Defaults {
			merged[k] = v
		}
	}

	// 4. Component inputs (highest priority for inputs)
	if comp.Inputs != nil {
		for k, v := range comp.Inputs {
			merged[k] = v
		}
	}

	return merged
}

// resolvePolicies extracts policies that apply to this component in this environment
func (e *Expander) resolvePolicies(comp model.Component, envName string) map[string]interface{} {
	policies := make(map[string]interface{})

	// Get group policies
	if comp.Domain != "" {
		if group, exists := e.groups[comp.Domain]; exists {
			if group.Policies != nil {
				for k, v := range group.Policies {
					policies[k] = v
				}
			}
		}
	}

	// Get environment policies
	if env, exists := e.normalized.Environments[envName]; exists {
		if env.Policies != nil {
			for k, v := range env.Policies {
				policies[k] = v
			}
		}
	}

	return policies
}

// resolveDependencies transforms component dependencies into resolved form
func (e *Expander) resolveDependencies(comp model.Component, envName string) []model.ResolvedDependency {
	resolved := make([]model.ResolvedDependency, 0)

	for _, dep := range comp.DependsOn {
		// Handle same-environment marker
		targetEnv := dep.Environment
		if dep.Environment == "__same__" {
			targetEnv = envName
		}

		resolved = append(resolved, model.ResolvedDependency{
			ComponentName: dep.Component,
			Environment:   targetEnv,
			Scope:         dep.Scope,
			Condition:     dep.Condition,
		})
	}

	return resolved
}

// GetComponentInstance retrieves a specific component instance
func (e *Expander) GetComponentInstance(envName, compName string, instances map[string][]*model.ComponentInstance) *model.ComponentInstance {
	if envInstances, exists := instances[envName]; exists {
		for _, inst := range envInstances {
			if inst.ComponentName == compName {
				return inst
			}
		}
	}
	return nil
}
