# Copyright 2026 Alessandro Rontani
#
# Licensed under the Apache License, Version 2.0.

## Authorization denial

The request is structurally valid but asks for `Service`. The local profile
admits Service long enough for creation, then verification narrows the active
`installation-access-ceiling` before asserting the negative result. The
namespace is allowed, so the outcome exercises runtime logical policy
revalidation rather than a missing author RBAC shortcut. Apply/inspect/verify/down through `make local-example
EXAMPLE=authorization-denial ACTION=apply`, `ACTION=inspect`, `ACTION=verify`,
or `ACTION=down`; verification expects the exact
`AuthorizationDenied` public condition.
