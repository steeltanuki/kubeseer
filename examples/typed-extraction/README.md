# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Typed extraction

The example extracts native Deployment availability and preserves it as a
boolean. Use the constrained interface:

`make local-example EXAMPLE=typed-extraction ACTION=apply`
`make local-example EXAMPLE=typed-extraction ACTION=inspect`
`make local-example EXAMPLE=typed-extraction ACTION=verify`
`make local-example EXAMPLE=typed-extraction ACTION=down`
The expected outcome is a typed integer replica value and a successful public
result; no internal controller package is required.
