# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Value operator

This example applies the supported `gte` value predicate to a native replica
count. Run `make local-example EXAMPLE=value-operator ACTION=apply`,
`make local-example EXAMPLE=value-operator ACTION=inspect`,
`make local-example EXAMPLE=value-operator ACTION=verify`, or
`make local-example EXAMPLE=value-operator ACTION=down`. Verification expects a non-empty public result
whose integer value is at least `1`.
