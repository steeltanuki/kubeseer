// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/steeltanuki/kubeseer/internal/localprobe"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(64)
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateDir := flags.String("state-dir", "", "absolute local state directory")
	metadata := flags.String("metadata", "", "absolute metadata.v1 path")
	kubeconfig := flags.String("kubeconfig", "", "absolute owned kubeconfig")
	clusterName := flags.String("cluster-name", localprobe.ClusterName, "expected owned kind cluster name")
	contextName := flags.String("context", localprobe.ContextName, "owned kubeconfig context")
	timeout := flags.Duration("timeout", 2*time.Minute, "bounded observation timeout")
	catalog := flags.String("catalog", "examples/catalog.txt", "ordered example catalog")
	example := flags.String("example", "", "one catalog example")
	namespace := flags.String("namespace", "", "one example namespace")
	destination := flags.String("destination", "", "private diagnostics destination")
	if err := flags.Parse(os.Args[2:]); err != nil {
		os.Exit(64)
	}
	if *metadata == "" && *stateDir != "" {
		*metadata = *stateDir + "/metadata.v1"
	}
	if *kubeconfig == "" && *stateDir != "" {
		*kubeconfig = *stateDir + "/kubeconfig"
	}
	if *metadata == "" || *kubeconfig == "" {
		fail(errors.New("--metadata and --kubeconfig (or --state-dir) are required"))
	}
	cfg := localprobe.Config{StateDir: *stateDir, Metadata: *metadata, Kubeconfig: *kubeconfig, ClusterName: *clusterName, Context: *contextName, Timeout: *timeout, Catalog: *catalog, Example: *example, Namespace: *namespace, Destination: *destination}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	switch command {
	case "identity":
		report, err := localprobe.Identity(ctx, cfg)
		if err != nil {
			fail(err)
		}
		output(report)
	case "readiness":
		report, err := localprobe.Readiness(ctx, cfg)
		if err != nil {
			fail(err)
		}
		output(report)
	case "status":
		report, err := localprobe.Status(ctx, cfg)
		if err != nil {
			fail(err)
		}
		output(report)
	case "verify":
		if err := localprobe.Verify(ctx, cfg); err != nil {
			fail(err)
		}
	case "diagnostics":
		path, err := localprobe.Diagnostics(ctx, cfg)
		if err != nil {
			fail(err)
		}
		fmt.Printf("LOCAL_DIAGNOSTICS=%s\n", path)
	default:
		usage()
		os.Exit(64)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: kubeseer-local <identity|readiness|status|verify|diagnostics> [flags]")
}

func output(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(true)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fail(err)
	}
}

func fail(err error) { fmt.Fprintf(os.Stderr, "KUBESEER_LOCAL_PROBE=failed: %v\n", err); os.Exit(1) }
