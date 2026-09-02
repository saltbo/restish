# Automatic Idempotency Keys for Embedded Products

Status: Accepted

## Problem

Some resource servers require an `Idempotency-Key` header on mutating
operations. Making that header a required positional argument exposes a
transport concern to every caller and makes agent-generated commands harder to
use correctly. Retrying a mutating request is also unsafe unless every attempt
reuses the same key.

This is an embedded-product concern rather than a new stock Restish default.
Restish cannot assume that an arbitrary server implements idempotency merely
because a request uses POST, PUT, PATCH, or DELETE.

## Decision

`CommandSurface` has an opt-in automatic-idempotency capability. The zero value
continues to preserve stock Restish behavior.

When enabled, an OpenAPI operation whose required header parameter is named
`Idempotency-Key`, matched case-insensitively, behaves as follows:

- the header is represented by an optional `--idempotency-key` flag rather
  than a required positional argument;
- an explicit flag value wins unchanged;
- when the flag is absent, Restish generates a cryptographically random RFC
  8941 string value immediately before constructing the request;
- one logical command invocation generates one value, and the retry transport
  reuses that value for every attempt;
- retries for that operation may include otherwise unsafe HTTP methods because
  the published contract declares idempotency;
- help, completion, inspection, and body-example generation do not generate a
  key or perform request work.

OpenAPI inspection exposes whether an operation declares the required header.
This lets an embedding product apply the same policy to its generic HTTP
command without reparsing or partially reimplementing OpenAPI.

## Ownership

Restish owns the mechanics: operation recognition, per-invocation key
generation, header construction, and safe reuse across its internal retries.

The embedding product owns policy. Realmroot Toolbox enables the capability and
uses the inspection signal before adding an automatic key to a generic command.
It must not add keys or enable unsafe retries for operations that do not publish
the required header contract. After matching that contract, the host marks the
generic invocation as idempotency-protected through Restish's trusted
in-process execution options; this fact cannot be supplied through raw argv.
Restish can then enable method retries without emitting its unsafe-retry
warning.

## Key Format

Generated values contain 128 bits of randomness encoded as lowercase
hexadecimal and wrapped as an RFC 8941 string, for example
`"0123456789abcdef0123456789abcdef"`. The quoting is part of the header value.

## Failure Behavior

Entropy failures abort before network I/O. Restish never silently sends a
mutating request without the required key. A caller may provide a previously
known key explicitly when recovering from an outcome that remained unknown
after retries.

## Tests

The contract is proven at two boundaries:

- generated-command tests prove omission from positional arguments,
  generation, explicit override, retry enablement, and same-key reuse;
- inspection tests prove that only a required `Idempotency-Key` header is
  advertised to embedding products.

Realmroot CLI tests separately prove that generic resource-first commands only
receive an automatic key and unsafe-method retry permission when the selected
operation advertises this contract.
