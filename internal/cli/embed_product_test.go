package cli_test

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	restish "github.com/saltbo/restish/v2"
	"github.com/saltbo/restish/v2/config"
	"github.com/saltbo/restish/v2/internal/cli"
)

func TestAutomaticIdempotencyForGeneratedCommand(t *testing.T) {
	app := newTestApp(t)
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	writeTestFile(t, specPath, automaticIdempotencySpec)
	app.CLI.SetDefaultConfig(&config.Config{APIs: map[string]*config.APIConfig{
		"svc": {BaseURL: "https://api.example.com", SpecFiles: []string{specPath}},
	}})
	app.CLI.SetCommandSurface(cli.CommandSurface{RegisteredAPIs: true, IgnoreUserConfig: true, AutomaticIdempotencyKeys: true})
	app.CLI.Hooks().RetryBaseDelay = 0

	var keys []string
	app.UseTransport(func(request *http.Request) (*http.Response, error) {
		keys = append(keys, request.Header.Get("Idempotency-Key"))
		status := http.StatusCreated
		if len(keys) == 1 {
			status = http.StatusServiceUnavailable
		}
		return jsonResponse(status, `{}`), nil
	})

	app.Run("svc", "create-item")
	if len(keys) != 2 {
		t.Fatalf("request attempts = %d, want POST retry after 503", len(keys))
	}
	if !regexp.MustCompile(`^"[0-9a-f]{32}"$`).MatchString(keys[0]) {
		t.Fatalf("generated Idempotency-Key = %q, want RFC 8941 quoted 128-bit hex string", keys[0])
	}
	if keys[1] != keys[0] {
		t.Fatalf("retry Idempotency-Key = %q, want original %q", keys[1], keys[0])
	}

	const explicit = `"caller-selected-key"`
	app.Run("svc", "create-item", "--idempotency-key", explicit)
	if got := keys[len(keys)-1]; got != explicit {
		t.Fatalf("explicit Idempotency-Key = %q, want unchanged %q", got, explicit)
	}
}

func TestInspectAPIRequiresIdempotencyKeyOnlyForRequiredHeader(t *testing.T) {
	app := newTestApp(t)
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	writeTestFile(t, specPath, automaticIdempotencySpec)
	app.CLI.SetDefaultConfig(&config.Config{APIs: map[string]*config.APIConfig{
		"svc": {BaseURL: "https://api.example.com", SpecFiles: []string{specPath}},
	}})
	app.CLI.SetCommandSurface(cli.CommandSurface{RegisteredAPIs: true, IgnoreUserConfig: true, AutomaticIdempotencyKeys: true})

	inspection, err := app.CLI.InspectAPI(context.Background(), "svc", "default")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"createItem": true, "createOptionalItem": false, "createOtherHeaderItem": false}
	if len(inspection.Operations) != len(want) {
		t.Fatalf("operations = %#v", inspection.Operations)
	}
	for _, operation := range inspection.Operations {
		if operation.RequiresIdempotencyKey != want[operation.ID] {
			t.Errorf("%s RequiresIdempotencyKey = %t, want %t", operation.ID, operation.RequiresIdempotencyKey, want[operation.ID])
		}
	}
}

const automaticIdempotencySpec = `{
  "openapi":"3.1.0",
  "info":{"title":"Idempotent API","version":"1"},
  "paths":{
    "/items":{"post":{"operationId":"createItem","parameters":[{"name":"Idempotency-Key","in":"header","required":true,"schema":{"type":"string"}}],"responses":{"201":{"description":"created"},"503":{"description":"unavailable"}}}},
    "/optional-items":{"post":{"operationId":"createOptionalItem","parameters":[{"name":"idempotency-key","in":"header","required":false,"schema":{"type":"string"}}],"responses":{"201":{"description":"created"}}}},
    "/other-header-items":{"post":{"operationId":"createOtherHeaderItem","parameters":[{"name":"Request-Key","in":"header","required":true,"schema":{"type":"string"}}],"responses":{"201":{"description":"created"}}}}
  }
}`

func TestAutomaticIdempotencyZeroValuePreservesRequiredHeaderArgument(t *testing.T) {
	app := newTestApp(t)
	specPath := filepath.Join(t.TempDir(), "openapi.json")
	writeTestFile(t, specPath, automaticIdempotencySpec)
	app.CLI.SetDefaultConfig(&config.Config{APIs: map[string]*config.APIConfig{
		"svc": {BaseURL: "https://api.example.com", SpecFiles: []string{specPath}},
	}})
	app.CLI.SetCommandSurface(cli.CommandSurface{RegisteredAPIs: true, IgnoreUserConfig: true})
	var requests int
	app.UseTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if got := request.Header.Get("Idempotency-Key"); got != `"caller-key"` {
			t.Fatalf("Idempotency-Key = %q, want positional value", got)
		}
		return jsonResponse(http.StatusCreated, `{}`), nil
	})

	err := app.RunErr("svc", "create-item")
	if err == nil || !strings.Contains(err.Error(), "missing required argument(s): idempotency-key") {
		t.Fatalf("missing positional Idempotency-Key error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("missing positional header sent %d requests", requests)
	}
	app.Run("svc", "create-item", `"caller-key"`)
	if requests != 1 {
		t.Fatalf("positional header sent %d requests, want 1", requests)
	}
}

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
	app.CLI.SetCommandSurface(cli.CommandSurface{HTTPMethods: []string{"get"}, RegisteredAPIs: true, IgnoreUserConfig: true, DisablePlugins: true, HideInternalFlags: true, CompactOperationHelp: true})

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
	for _, hidden := range []string{"AgentOAuth", "oauth2", "Response 200", "Argument Schema"} {
		if strings.Contains(help, hidden) {
			t.Fatalf("compact product help exposed %q:\n%s", hidden, help)
		}
	}
	for _, expected := range []string{"Show wallet", "Required scopes: wallet:read"} {
		if !strings.Contains(help, expected) {
			t.Fatalf("compact product help omitted %q:\n%s", expected, help)
		}
	}
	if strings.Contains(help, "\n\n\nRequired scopes:") {
		t.Fatalf("compact product help contains an empty section:\n%s", help)
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
