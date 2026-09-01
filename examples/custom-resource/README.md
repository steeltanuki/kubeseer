# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Structural custom resource

The lightweight `Widget` CRD is applied before its Custom Resource and is
selected through the public discovery path. Use `make local-example
EXAMPLE=custom-resource ACTION=apply`, `ACTION=inspect`, `ACTION=verify`, or
`ACTION=down`; cleanup removes the Widget before the fixture CRD. The expected outcome is a typed `size`
value with Widget provenance.
