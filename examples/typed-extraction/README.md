# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Typed extraction

The example extracts a Deployment's `metadata.creationTimestamp`. Kubernetes
exposes that value as an RFC 3339 string; Kubeseer parses it and publishes a
typed timestamp. This distinguishes it from `builtin-resource`, which extracts
an integer replica count. Use the constrained interface:

`make local-example EXAMPLE=typed-extraction ACTION=apply`
`make local-example EXAMPLE=typed-extraction ACTION=inspect`
`make local-example EXAMPLE=typed-extraction ACTION=verify`
`make local-example EXAMPLE=typed-extraction ACTION=down`
The expected outcome is `createdAt` with type `timestamp` and a parsed
timestamp value in a successful public result; no internal controller package
is required.
