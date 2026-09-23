# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Partial degradation

One Deployment source remains successful while a second `DegradedWidget` source is
made unavailable after its own fixture type is removed. The custom-resource
example's `Widget` type remains installed. Apply creates the fixture
in deterministic order; verify expects `Degraded=True`, a successful sibling,
and the unavailable-source reason. Use the constrained commands via `make local-example EXAMPLE=partial-degradation ACTION=apply`, `ACTION=inspect`,
`ACTION=verify`, or `ACTION=down`.
