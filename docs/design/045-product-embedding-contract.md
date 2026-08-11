# Product Embedding Contract

Status: implemented locally; upstream review pending.

## Product Frame

Problem:
Organizations can embed Restish's OpenAPI command generation and HTTP runtime,
but a curated product still has to expose Restish-branded flags, scrape Cobra
help to discover operations, or use a subprocess response plugin. That makes an
embedded CLI feel like a renamed Restish distribution and keeps product logic
coupled to an external plugin protocol.

Goals:

- Let an embedder inspect the same generated operation inventory used by command
  dispatch, including exact command paths and OpenAPI security requirements.
- Let an embedder hide engine-owned global controls while translating a small,
  product-owned flag surface to the existing runtime flags internally.
- Let trusted in-process product code inspect or replace normalized responses
  without installing or launching a plugin binary.
- Preserve the stock Restish command, flags, plugin protocol, and output
  behavior unchanged.

Non-goals:

- Restish does not interpret Realmroot profiles or open approval pages.
- Restish does not own an embedder's authorization request workflow.
- The embedding API does not expose the mutable Cobra tree.
- The response hook does not add an independent HTTP client or retry policy.

Primary workflow:

1. The host constructs a `CLI`, installs authoritative in-memory config and a
   curated command surface, and registers its auth handler.
2. The host calls `InspectAPI` to render product-owned operation and scope help.
3. The host translates its public flags to hidden Restish runtime flags and
   calls `Run` for an operation.
4. A registered response middleware receives the normalized request/response.
   It may preserve, replace, or suppress that response. Product-specific
   interaction and polling remain in the host and may call `FetchResponse` so
   authentication and transport behavior stay owned by Restish.

## User-Facing Behavior

`CommandSurface.HideInternalFlags` hides every `rsh-*` flag and `--help-all`
from ordinary and expanded help, including generated operations. Hidden flags
remain parseable so a host can translate product flags without adding a second
request/output implementation. Restish-specific prose is also omitted from the
curated help surface.

`CommandSurface.CompactOperationHelp` lets a product keep ordinary generated
operation help bounded. The help retains the operation summary, required
argument descriptions, ordinary operation flags, and OAuth scope alternatives,
but omits the full OpenAPI description, credential scheme names, schemas,
examples, and response models. This is an opt-in product surface; stock Restish
continues to render complete operation reference material.

`InspectAPI(ctx, api, profile)` returns only operations exposed by the API's
configured exclusion and hidden-operation policy. Each item includes its exact
generated command path, operation ID, method, path, summary, and OpenAPI
credential alternatives. The method uses the same discovery, cache, profile,
server-variable, command-layout, and naming code as `Run`; it never reparses a
parallel OpenAPI model in the host.

In-process response middleware runs after normalization and before pagination
or rendering, in registration order. Middleware receives the request and a
normalized response and returns a replacement response, a drop decision, or an
error. An error aborts the command. Middleware is trusted code in the same
process and therefore sees the same response material as a custom formatter.
It is disabled for `FetchResponse`, which remains the safe primitive a
middleware can use for bounded follow-up reads without recursive invocation.

## Proposed Public API

```go
type APIInspection struct {
    Name       string
    Summary    string
    Operations []OperationInspection
}

type OperationInspection struct {
    ID                     string
    Command                []string
    Method                 string
    Path                   string
    Summary                string
    NoAuth                 bool
    OptionalAuth           bool
    CredentialAlternatives [][]CredentialRequirementInspection
}

type CommandSurface struct {
    // Existing fields omitted.
    CompactOperationHelp bool
}

func (c *CLI) InspectAPI(ctx context.Context, apiName, profileName string) (APIInspection, error)

type ResponseMiddleware func(context.Context, *http.Request, *Response) (ResponseMiddlewareResult, error)

type ResponseMiddlewareResult struct {
    Response *Response
    Drop     bool
}

func (c *CLI) AddResponseMiddleware(ResponseMiddleware)
```

The returned command is a token slice relative to the configured API group.
For a tag layout it contains both the tag command and operation command. The
embedder prepends its own product command path when presenting it.

## Compatibility

All behavior is opt-in. The zero `CommandSurface` value, stock binary, public
plugin protocol, generated operation paths, and existing flags remain
unchanged. The additions are source-compatible public Go APIs.

## Security, Privacy, And Failure Modes

- Operation inspection performs the same bounded metadata refresh configured
  for curated APIs and returns discovery errors rather than an incomplete list.
- Hidden flags are not a security boundary. They are a product UX boundary;
  embedders must still validate which product flags they translate.
- Middleware is trusted in-process code. A nil replacement preserves the
  current response. Returning both `Drop` and a replacement is invalid.
- Follow-up requests are explicitly initiated by the host through
  `FetchResponse`, which retains URL matching, authentication, DPoP proof, TLS,
  and cancellation behavior. Restish does not copy request credentials to a
  middleware-selected origin.
- Middleware and inspection errors retain their cause and abort at the CLI
  execution boundary.

## Testing Plan

- CLI behavior tests prove hidden engine flags never appear in root, API, or
  operation help while remaining internally parseable.
- Compact-help tests prove the product surface retains scope and argument
  discovery without exposing credential scheme names or large schema material.
- Inspection tests compare returned command paths and security scopes with the
  generated command tree for root and tag layouts, exclusions, and hidden
  operations.
- HTTP behavior tests prove response preservation, replacement, drop, ordering,
  error propagation, and that `FetchResponse` does not recurse through hooks.
- Existing stock CLI, integration, binary-build, docs-generation, and docs-site
  gates remain unchanged.

## Documentation Impact

The root package Go documentation and the embedding design record document the
new contract. Stock end-user documentation does not advertise the API as a new
Restish command or flag.

## Alternatives

- Exposing Cobra would make command discovery easy but permanently couples
  embedders to Restish's internal tree and mutable lifecycle.
- Asking hosts to parse OpenAPI duplicates naming, exclusion, tag layout,
  server variables, security, and cache semantics.
- Keeping subprocess-only response hooks preserves process isolation but forces
  a separately installed binary and JSON protocol for trusted product code.
- Renaming every internal Restish flag expands the public compatibility surface;
  hiding them and translating a curated host surface is smaller and clearer.
