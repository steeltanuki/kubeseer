# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Authorization denial

The request is structurally valid but asks for `Service`, a kind deliberately
outside `installation-access-ceiling`. The namespace is allowed, so the
negative result exercises logical policy rather than a missing author RBAC
shortcut. Apply/inspect/verify/down through `make local-example
EXAMPLE=authorization-denial ACTION=apply`, `ACTION=inspect`, `ACTION=verify`,
or `ACTION=down`; verification expects the exact
`AuthorizationDenied` public condition.
