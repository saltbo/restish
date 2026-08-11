package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/saltbo/restish/v2/config"
	"github.com/saltbo/restish/v2/internal/spec"
	"github.com/spf13/cobra"
)

// APIInspection is the generated command and authorization inventory for one
// configured API.
type APIInspection struct {
	Name       string                `json:"name"`
	Summary    string                `json:"summary,omitempty"`
	Operations []OperationInspection `json:"operations"`
}

// OperationInspection describes one generated operation exactly as exposed by
// the configured command layout.
type OperationInspection struct {
	ID                     string                              `json:"id"`
	Command                []string                            `json:"command"`
	Method                 string                              `json:"method"`
	Path                   string                              `json:"path"`
	Summary                string                              `json:"summary,omitempty"`
	NoAuth                 bool                                `json:"noAuth,omitempty"`
	OptionalAuth           bool                                `json:"optionalAuth,omitempty"`
	CredentialAlternatives [][]CredentialRequirementInspection `json:"credentialAlternatives,omitempty"`
}

// CredentialRequirementInspection is the product-relevant subset of one
// OpenAPI security requirement.
type CredentialRequirementInspection struct {
	ID    string   `json:"id"`
	Kind  string   `json:"kind"`
	Needs []string `json:"needs,omitempty"`
}

// InspectAPI returns the same generated operations, command paths, and OpenAPI
// security requirements used by Run. The CLI must not be used concurrently.
func (c *CLI) InspectAPI(ctx context.Context, apiName, profileName string) (APIInspection, error) {
	if profileName == "" {
		profileName = "default"
	}
	cfg := cloneConfigForEmbedding(c.defaultConfig)
	if !c.commandSurface.IgnoreUserConfig {
		var err error
		cfg, err = c.loadConfig()
		if err != nil {
			return APIInspection{}, err
		}
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	c.cfg = cfg
	apiCfg := cfg.APIs[apiName]
	if apiCfg == nil {
		return APIInspection{}, fmt.Errorf("API %q is not configured", apiName)
	}
	timeout := c.commandSurface.MetadataRefreshTimeout
	if timeout <= 0 {
		timeout = staleGeneratedOperationRefreshTimeout
	}
	if err := c.ensureAPIMetadata(ctx, apiName, profileName, apiCfg, timeout); err != nil {
		return APIInspection{}, err
	}
	set, ok, err := c.operationSetForAPI(ctx, apiName, apiCfg, profileName, false)
	if err != nil {
		return APIInspection{}, err
	}
	if !ok {
		return APIInspection{}, fmt.Errorf("generated commands for API %q are unavailable", apiName)
	}
	apiCommand := c.buildAPICommandFromOperationSet(apiName, apiCfg, set, effectiveOperationBase(apiCfg, profileName))
	if apiCommand == nil {
		return APIInspection{}, fmt.Errorf("generated commands for API %q are unavailable", apiName)
	}
	operationsByID := make(map[string]spec.Operation, len(set.Operations))
	for _, operation := range filterExcludedOperations(set.Operations, apiCfg.ExcludedOperationIDs) {
		operationsByID[operation.ID] = operation
	}
	inspection := APIInspection{Name: apiName, Summary: strings.TrimSpace(set.Info.Summary)}
	for _, command := range commandTree(apiCommand) {
		if command == apiCommand || command.Hidden || command.Annotations == nil {
			continue
		}
		id := command.Annotations[generatedOperationIDAnnotation]
		if id == "" {
			continue
		}
		operation, exists := operationsByID[id]
		if !exists {
			continue
		}
		inspection.Operations = append(inspection.Operations, OperationInspection{
			ID: id, Command: relativeCommandPath(apiCommand, command), Method: operation.Method,
			Path: operation.Path, Summary: command.Short, NoAuth: operation.NoAuth,
			OptionalAuth: operation.OptionalAuth, CredentialAlternatives: inspectCredentialAlternatives(operation.CredentialAlternatives),
		})
	}
	return inspection, nil
}

func inspectCredentialAlternatives(alternatives []spec.CredentialAlternative) [][]CredentialRequirementInspection {
	result := make([][]CredentialRequirementInspection, 0, len(alternatives))
	for _, alternative := range alternatives {
		items := make([]CredentialRequirementInspection, 0, len(alternative))
		for _, requirement := range alternative {
			items = append(items, CredentialRequirementInspection{
				ID: requirement.ID, Kind: requirement.Kind, Needs: append([]string(nil), requirement.Needs...),
			})
		}
		result = append(result, items)
	}
	return result
}

func relativeCommandPath(root, command *cobra.Command) []string {
	var reversed []string
	for current := command; current != nil && current != root; current = current.Parent() {
		reversed = append(reversed, current.Name())
	}
	path := make([]string, len(reversed))
	for index := range reversed {
		path[len(reversed)-1-index] = reversed[index]
	}
	return path
}
