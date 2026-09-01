# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Cross-namespace aggregation

Two labelled Deployments in separate namespaces are selected and counted in a
deterministic aggregate. Apply, inspect, verify, and clean up only this entry
with:

`make local-example EXAMPLE=cross-namespace-aggregation ACTION=apply`
`make local-example EXAMPLE=cross-namespace-aggregation ACTION=inspect`
`make local-example EXAMPLE=cross-namespace-aggregation ACTION=verify`
`make local-example EXAMPLE=cross-namespace-aggregation ACTION=down`
The expected public outcome is one deterministic aggregate with contributors
from both exact namespaces.
