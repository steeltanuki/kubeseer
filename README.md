# Kubeseer

Kubeseer is a Kubernetes operator that exposes a declarative Custom Resource for inspecting, extracting, filtering, and aggregating information from Kubernetes resources.

A Kubeseer resource can observe both built-in Kubernetes objects and Custom Resources, select objects across one or more namespaces, extract values through JSONPath, convert them into typed outputs, and publish the resulting view in its status.

The operator is designed around an administrator-defined access boundary. During installation, administrators define which namespaces and resource types Kubeseer may inspect. Individual Kubeseer resources can further restrict that scope, but cannot expand it.

## Main goals

- Inspect built-in Kubernetes resources and Custom Resources.
- Select resources by name, namespace, labels, and other supported selectors.
- Extract fields through a controlled JSONPath-based model.
- Preserve and expose typed values.
- Filter, group, and aggregate values from multiple resources.
- Aggregate information across namespaces.
- Publish deterministic results through the Kubeseer resource status.
- Enforce installation-level access policies and avoid privilege escalation.
- Reconcile efficiently, updating status only when the semantic result changes.

The current functional breakdown and proposed implementation roadmap are documented in [SPECIFICATIONS.md](SPECIFICATIONS.md).

The canonical Helm package and lifecycle procedures are documented in
[docs/installation.md](docs/installation.md); chart defaults and the values
contract are in [charts/kubeseer/README.md](charts/kubeseer/README.md).

The persistent kind-on-rootless-Podman contributor workflow, bundled examples,
and diagnostics are documented in [docs/local-development.md](docs/local-development.md).

## Walden and AI agent experimentation

Kubeseer is also an experimental project for exploring spec-driven software delivery with [Walden](https://github.com/andrearaponi/walden) and AI coding agents.

The project is intentionally divided into small, independently reviewable and verifiable features. Each feature is expected to progress through Walden's requirements, design, task-planning, execution, and evidence workflow.

This repository is used to experiment with:

- requirements written before implementation;
- explicit traceability between requirements, design decisions, tasks, and tests;
- human-reviewed specifications produced with the assistance of AI agents;
- executable verification proofs for implementation tasks;
- durable evidence that binds completed work to the approved specification and code;
- collaboration between multiple AI agents while preserving deterministic project gates;
- the strengths and limitations of agent-assisted development on a real Kubernetes operator.

AI agents may help analyse the problem, propose specifications, design the architecture, implement code, and prepare verification steps. Architectural decisions, approvals, and responsibility for the final result remain with the human maintainer.

## Project status

The packaging-and-installation specification is implemented through the
canonical Helm chart, explicit CRD upgrade gate, dual certificate modes, and
separate confirmed purge path. The remaining product roadmap continues to be
tracked in the Walden specifications.
