# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Built-in resource

This example selects one public `apps/v1 Deployment` and exposes its replica
count with provenance. Apply it with `make local-example EXAMPLE=builtin-resource ACTION=apply`, inspect with `ACTION=inspect`, verify with `ACTION=verify`, and remove only this example with `ACTION=down`.

The commands use the owned `kind-kubeseer-local` context through the local
workflow. The expected public outcome is `Ready=True`, one selected
Deployment, and an integer `replicas` value of `1`.
