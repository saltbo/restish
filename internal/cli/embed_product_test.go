package cli_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	restish "github.com/saltbo/restish/v2"
	"github.com/saltbo/restish/v2/config"
	"github.com/saltbo/restish/v2/internal/cli"
)

func TestProductSurfaceHidesInternalFlagsAndInspectsGeneratedScopes(t *testing.T) {
	app := newTestApp(t)
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	writeTestFile(t, specPath, `{
  "openapi":"3.1.0","info":{"title":"Wallet","version":"1"},
  "components":{"securitySchemes":{"AgentOAuth":{"type":"oauth2","flows":{"clientCredentials":{"tokenUrl":"https://id.example/token","scopes":{"wallet:read":"Read wallet"}}}}}},
  "paths":{"/agent/wallet":{"get":{"operationId":"getWallet","tags":["wallet"],"summary":"Show wallet","security":[{"AgentOAuth":["wallet:read"]}],"responses":{"200":{"description":"ok"}}}}}
}`)
	app.CLI.SetDefaultConfig(&config.Config{APIs: map[string]*config.APIConfig{
		"agent-wallet": {BaseURL: "https://wallet.example", SpecFiles: []string{specPath}, CommandLayout: "tags"},
	}})
	app.CLI.SetCommandName("realmroot toolbox")
	app.CLI.SetCommandSurface(cli.CommandSurface{HTTPMethods: []string{"get"}, RegisteredAPIs: true, IgnoreUserConfig: true, DisablePlugins: true, HideInternalFlags: true})

	inspection, err := app.CLI.InspectAPI(context.Background(), "agent-wallet", "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Operations) != 1 {
		t.Fatalf("operations = %#v", inspection.Operations)
	}
	operation := inspection.Operations[0]
	if strings.Join(operation.Command, " ") != "wallet get-wallet" || operation.ID != "getWallet" {
		t.Fatalf("operation = %#v", operation)
	}
	if got := operation.CredentialAlternatives[0][0].Needs; len(got) != 1 || got[0] != "wallet:read" {
		t.Fatalf("scopes = %#v", got)
	}

	app.Run("agent-wallet", "wallet", "get-wallet", "--help")
	help := app.Stdout.String()
	if strings.Contains(help, "Restish") || strings.Contains(help, "--rsh-") || strings.Contains(help, "--help-all") {
		t.Fatalf("internal engine leaked into help:\n%s", help)
	}
	app.Stdout.Reset()
	app.Run("get", "--help")
	help = app.Stdout.String()
	if strings.Contains(help, "Restish") || strings.Contains(help, "restish") || strings.Contains(help, "--rsh-") {
		t.Fatalf("internal engine leaked into generic help:\n%s", help)
	}
}

func TestInProcessResponseMiddlewareReplacesNormalizedResponse(t *testing.T) {
	app := newTestApp(t)
	app.UseJSONResponse(http.StatusOK, `{"status":"pending"}`)
	app.CLI.AddResponseMiddleware(func(_ context.Context, request *http.Request, response *restish.Response) (restish.ResponseMiddlewareResult, error) {
		if request.Method != http.MethodGet || response.Status != http.StatusOK {
			t.Fatalf("request/response = %s %#v", request.Method, response)
		}
		replacement := *response
		replacement.Body = map[string]any{"status": "approved"}
		return restish.ResponseMiddlewareResult{Response: &replacement}, nil
	})

	app.Run("get", "https://api.example.com/request", "--rsh-output-format", "json", "--rsh-print", "b")
	if got := app.Stdout.String(); !strings.Contains(got, `"status":"approved"`) || strings.Contains(got, "pending") {
		t.Fatalf("output = %s", got)
	}
}

func TestInProcessResponseMiddlewareCanDropOutput(t *testing.T) {
	app := newTestApp(t)
	app.UseJSONResponse(http.StatusOK, `{"status":"pending"}`)
	app.CLI.AddResponseMiddleware(func(context.Context, *http.Request, *restish.Response) (restish.ResponseMiddlewareResult, error) {
		return restish.ResponseMiddlewareResult{Drop: true}, nil
	})
	app.Run("get", "https://api.example.com/request", "--rsh-print", "b")
	if app.Stdout.Len() != 0 {
		t.Fatalf("dropped output = %q", app.Stdout.String())
	}
}

func TestInProcessResponseMiddlewareErrorAbortsCommand(t *testing.T) {
	app := newTestApp(t)
	app.UseJSONResponse(http.StatusOK, `{}`)
	want := errors.New("interaction failed")
	app.CLI.AddResponseMiddleware(func(context.Context, *http.Request, *restish.Response) (restish.ResponseMiddlewareResult, error) {
		return restish.ResponseMiddlewareResult{}, want
	})
	if err := app.RunErr("get", "https://api.example.com/request", "--rsh-print", "b"); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}
