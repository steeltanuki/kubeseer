// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type object = map[string]interface{}

func check(ok bool, message string) {
	if !ok {
		fmt.Fprintln(os.Stderr, "RELEASE_WORKFLOW_POLICY=failed:", message)
		os.Exit(1)
	}
}

func obj(value interface{}) object {
	result, _ := value.(map[string]interface{})
	return result
}

func list(value interface{}) []interface{} {
	result, _ := value.([]interface{})
	return result
}

func str(value interface{}) string {
	result, _ := value.(string)
	return result
}

func workflow(path string) object {
	data, err := os.ReadFile(path)
	check(err == nil, "cannot read "+path)
	var result object
	err = yaml.Unmarshal(data, &result)
	check(err == nil && result != nil, "cannot parse workflow YAML: "+path)
	return result
}

func steps(job object) []object {
	var result []object
	for _, value := range list(job["steps"]) {
		result = append(result, obj(value))
	}
	return result
}

func runs(job object) string {
	var result []string
	for _, step := range steps(job) {
		if run := str(step["run"]); run != "" {
			result = append(result, run)
		}
	}
	return strings.Join(result, "\n")
}

func runCount(job object) int {
	count := 0
	for _, step := range steps(job) {
		if str(step["run"]) != "" {
			count++
		}
	}
	return count
}

func exactRunLineCount(job object, command string) int {
	count := 0
	for _, step := range steps(job) {
		for _, line := range strings.Split(str(step["run"]), "\n") {
			if strings.TrimSpace(line) == command {
				count++
			}
		}
	}
	return count
}

const ripgrepSetup = "sudo apt-get update\nsudo apt-get install -y --no-install-recommends ripgrep\nrg --version"

func requireRipgrepSetup(job object, installName, beforeName string) {
	installIndex, targetIndex, installCount := -1, -1, 0
	for index, step := range steps(job) {
		if str(step["name"]) == installName {
			installCount++
			check(strings.TrimSpace(str(step["run"])) == ripgrepSetup, installName+" must install and confirm ripgrep")
			installIndex = index
		}
		if str(step["name"]) == beforeName {
			targetIndex = index
		}
	}
	check(installCount == 1 && targetIndex >= 0 && installIndex < targetIndex, installName+" must precede "+beforeName)
}

func permissions(value interface{}) map[string]string {
	result := map[string]string{}
	for key, value := range obj(value) {
		result[key] = str(value)
	}
	return result
}

func hasPermissions(value interface{}, expected map[string]string) bool {
	got := permissions(value)
	if len(got) != len(expected) {
		return false
	}
	for key, permission := range expected {
		if got[key] != permission {
			return false
		}
	}
	return true
}

func actionSteps(workflow object, prefix string) []object {
	var result []object
	for _, rawJob := range obj(workflow["jobs"]) {
		for _, step := range steps(obj(rawJob)) {
			if strings.HasPrefix(str(step["uses"]), prefix+"@") {
				result = append(result, step)
			}
		}
	}
	return result
}

func verifyActionPins(workflow object) {
	pinned := regexp.MustCompile("^[^@]+@[0-9a-f]{40}$")
	count := 0
	for jobName, rawJob := range obj(workflow["jobs"]) {
		for _, step := range steps(obj(rawJob)) {
			uses := str(step["uses"])
			if uses != "" {
				count++
				check(pinned.MatchString(uses), jobName+" uses an action not pinned to a full commit SHA: "+uses)
				check(strings.HasPrefix(uses, "actions/checkout@") || strings.HasPrefix(uses, "actions/setup-go@"), jobName+" introduces an unapproved action dependency: "+uses)
			}
		}
	}
	if str(workflow["name"]) == "Continuous integration" {
		check(count == 2, "CI must use only checkout and Go setup actions")
	} else {
		check(count == 3, "release workflow must use only checkout and Go setup actions")
	}
}

func stringsFrom(value interface{}) []string {
	var result []string
	for _, item := range list(value) {
		result = append(result, str(item))
	}
	return result
}

func ciTriggerMatches(on object, event, ref string) bool {
	if event == "pull_request" {
		_, found := on["pull_request"]
		return found
	}
	if event != "push" || obj(on["push"]) == nil || !strings.HasPrefix(ref, "refs/heads/") {
		return false
	}
	branch := strings.TrimPrefix(ref, "refs/heads/")
	for _, pattern := range stringsFrom(obj(on["push"])["branches"]) {
		if pattern == "**" || pattern == branch {
			return true
		}
	}
	return false
}

func releaseTriggerMatches(on object, event, ref string) bool {
	if event != "push" || obj(on["push"]) == nil || !strings.HasPrefix(ref, "refs/tags/") {
		return false
	}
	tag := strings.TrimPrefix(ref, "refs/tags/")
	for _, pattern := range stringsFrom(obj(on["push"])["tags"]) {
		if pattern == "v*" && strings.HasPrefix(tag, "v") {
			return true
		}
	}
	return false
}

var stableTag = regexp.MustCompile("^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")

func mayPublish(on object, event, ref string, gatesPassed, nonVacuous, sameSHA bool) bool {
	if !releaseTriggerMatches(on, event, ref) || !gatesPassed || !nonVacuous || !sameSHA {
		return false
	}
	return stableTag.MatchString(strings.TrimPrefix(ref, "refs/tags/"))
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	ci := workflow(filepath.Join(root, ".github", "workflows", "ci.yml"))
	release := workflow(filepath.Join(root, ".github", "workflows", "release.yml"))
	check(str(ci["name"]) == "Continuous integration" && str(release["name"]) == "Official release distribution", "unexpected workflow names")
	verifyActionPins(ci)
	verifyActionPins(release)
	check(len(actionSteps(ci, "actions/checkout")) == 1 && len(actionSteps(ci, "actions/setup-go")) == 1, "CI must use pinned first-party checkout and Go setup")
	check(len(actionSteps(release, "actions/checkout")) == 2 && len(actionSteps(release, "actions/setup-go")) == 1, "release must pin both checkouts and Go setup")
	fmt.Println("PASS release-distribution/workflow-actions-pinned")

	ciOn := obj(ci["on"])
	check(len(ciOn) == 2, "CI must have only pull_request and push triggers")
	if _, ok := ciOn["pull_request"]; !ok {
		check(false, "CI must validate pull requests")
	}
	check(strings.Join(stringsFrom(obj(ciOn["push"])["branches"]), ",") == "**", "CI push trigger must cover ordinary branches")
	check(hasPermissions(ci["permissions"], map[string]string{"contents": "read"}), "CI default token must be read-only")
	ciJobs := obj(ci["jobs"])
	check(len(ciJobs) == 1, "CI must have only its validation job")
	ciJob := obj(ciJobs["validate"])
	check(hasPermissions(ciJob["permissions"], map[string]string{"contents": "read"}), "CI job token must be read-only")
	requireRipgrepSetup(ciJob, "Install ripgrep for repository verification", "Verify generated files, package rules, and repository boundaries")
	ciRuns := runs(ciJob)
	check(runCount(ciJob) == 4, "CI must have one ripgrep setup and exactly three validation commands")
	for _, command := range []string{"make verify", "make test", "make test-release-distribution SCENARIO=policy"} {
		check(exactRunLineCount(ciJob, command) == 1, "CI must invoke exactly once: "+command)
	}
	for _, forbidden := range []string{"podman push", "helm push", "gh release create", "publish-image", "release-distribution.sh publish "} {
		check(!strings.Contains(ciRuns, forbidden), "CI includes a public release write: "+forbidden)
	}
	fmt.Println("PASS release-distribution/ci-validates-with-read-only-permissions")

	releaseOn := obj(release["on"])
	check(len(releaseOn) == 1 && len(obj(releaseOn["push"])) == 1, "release workflow must trigger only from tag pushes")
	check(strings.Join(stringsFrom(obj(releaseOn["push"])["tags"]), ",") == "v*", "release tag trigger must be the coarse v* pattern")
	check(len(obj(release["permissions"])) == 0, "release workflow must deny permissions by default")
	check(strings.Contains(str(obj(release["concurrency"])["group"]), "github.ref"), "release runs must serialize by source tag")
	check(obj(release["concurrency"])["cancel-in-progress"] == false, "release reruns must not cancel a same-version publisher")
	fmt.Println("PASS release-distribution/release-trigger-is-tag-only-and-serialized")

	jobs := obj(release["jobs"])
	check(len(jobs) == 2, "release workflow must contain only gates and publish jobs")
	gates, publish := obj(jobs["gates"]), obj(jobs["publish"])
	check(hasPermissions(gates["permissions"], map[string]string{"contents": "read"}), "release gates must have only contents:read")
	check(hasPermissions(publish["permissions"], map[string]string{"contents": "write", "packages": "write"}), "publish must have only contents:write and packages:write")
	needs := stringsFrom(publish["needs"])
	check(len(needs) == 1 && needs[0] == "gates", "publish must depend on the successful gates job")
	fmt.Println("PASS release-distribution/permissions-are-job-scoped")

	gateRuns := runs(gates)
	requireRipgrepSetup(gates, "Install ripgrep for repository verification", "Verify generated files and package contracts")
	for _, command := range []string{
		"./hack/release-distribution.sh validate --tag \"$GITHUB_REF_NAME\" --source-sha \"$source_sha\"",
		"./hack/local-environment.sh check",
		"make verify",
		"make test",
		"make test-compatibility",
		"make test-package-compatibility",
		"./hack/e2e-harness-acceptance.sh",
		"make e2e",
	} {
		check(exactRunLineCount(gates, command) == 1, "release gates must run exactly once: "+command)
	}
	check(runCount(gates) == 9, "release gate job must contain one ripgrep setup and the approved eight commands")
	for _, duplicate := range []string{"make build", "make verify-package", "make test-integration"} {
		check(!strings.Contains(gateRuns, duplicate), "release workflow duplicates an existing verification contract: "+duplicate)
	}
	check(strings.Contains(str(obj(gates["outputs"])["source_sha"]), "steps.source.outputs.sha"), "gates must export their verified full source SHA")
	check(strings.Contains(gateRuns, "git rev-parse --verify") && strings.Contains(gateRuns, "GITHUB_REF") && strings.Contains(gateRuns, "GITHUB_SHA"), "gates must peel and confirm the exact tag commit")
	fmt.Println("PASS release-distribution/mandatory-release-gates")

	publishRuns := runs(publish)
	requireRipgrepSetup(publish, "Install ripgrep for release source checks", "Publish and verify the immutable release artifacts")
	check(runCount(publish) == 3, "release publisher must contain ripgrep setup and the two approved source and publish commands")
	check(strings.Contains(publishRuns, "release-distribution.sh publish "), "publisher must invoke the canonical release orchestrator")
	check(strings.Contains(publishRuns, "RELEASE_GATE_SOURCE_SHA") && strings.Contains(publishRuns, "GITHUB_SHA"), "publisher must compare the gated SHA with the event SHA")
	check(strings.Contains(publishRuns, "git rev-parse HEAD"), "publisher must independently confirm its checked-out source revision")
	check(strings.Contains(str(obj(publish["env"])["RELEASE_GATE_SOURCE_SHA"]), "needs.gates.outputs.source_sha"), "publisher SHA must come from the gates output")
	check(strings.Contains(str(obj(publish["env"])["GITHUB_TOKEN"]), "secrets.GITHUB_TOKEN"), "publisher must use the built-in GITHUB_TOKEN")
	publishCheckouts := 0
	for _, step := range steps(publish) {
		if strings.HasPrefix(str(step["uses"]), "actions/checkout@") {
			publishCheckouts++
			check(strings.Contains(str(obj(step["with"])["ref"]), "needs.gates.outputs.source_sha"), "publisher checkout must use the gates SHA")
			check(obj(step["with"])["persist-credentials"] == false, "privileged checkout credentials must not persist")
		}
	}
	check(publishCheckouts == 1, "publisher must check out exactly one gated source revision")
	fmt.Println("PASS release-distribution/publisher-uses-the-verified-sha")

	check(obj(actionSteps(ci, "actions/checkout")[0]["with"])["persist-credentials"] == false, "CI checkout credentials must not persist")
	gatesCheckout := obj(actionSteps(release, "actions/checkout")[0]["with"])
	check(strings.Contains(str(gatesCheckout["ref"]), "github.sha") && gatesCheckout["fetch-depth"] == 0, "gates must check out the tag SHA and fetch lineage history")
	for _, content := range []string{ciRuns, gateRuns, publishRuns} {
		check(!strings.Contains(content, "git tag -a") && !strings.Contains(content, "git push"), "workflow must not create or push release tags")
	}
	fmt.Println("PASS release-distribution/no-automatic-tag-or-persistent-checkout-token")

	type event struct {
		name, kind, ref                   string
		gates, nonVacuous, sameSHA, allow bool
		ciExpected                        bool
	}
	fixtures := []event{
		{name: "pull-request", kind: "pull_request", ref: "refs/pull/14/merge", ciExpected: true},
		{name: "develop-push", kind: "push", ref: "refs/heads/develop", ciExpected: true},
		{name: "ordinary-branch-push", kind: "push", ref: "refs/heads/feature/release-docs", ciExpected: true},
		{name: "malformed-prerelease-tag", kind: "push", ref: "refs/tags/v0.2.0-rc.1", gates: true, nonVacuous: true, sameSHA: true},
		{name: "failed-gate", kind: "push", ref: "refs/tags/v0.1.0", nonVacuous: true, sameSHA: true},
		{name: "zero-match-gate", kind: "push", ref: "refs/tags/v0.1.0", gates: true, sameSHA: true},
		{name: "tag-source-sha-mismatch", kind: "push", ref: "refs/tags/v0.1.0", gates: true, nonVacuous: true},
		{name: "approved-stable-release", kind: "push", ref: "refs/tags/v0.1.0", gates: true, nonVacuous: true, sameSHA: true, allow: true},
	}
	for _, fixture := range fixtures {
		check(ciTriggerMatches(ciOn, fixture.kind, fixture.ref) == fixture.ciExpected, fixture.name+" has incorrect CI trigger behavior")
		got := mayPublish(releaseOn, fixture.kind, fixture.ref, fixture.gates, fixture.nonVacuous, fixture.sameSHA)
		check(got == fixture.allow, fixture.name+" has incorrect publication eligibility")
		fmt.Println("PASS release-distribution/event-fixture/" + fixture.name)
	}
	fmt.Printf("RELEASE_WORKFLOW_POLICY=passed CASES=%d\n", 7+len(fixtures))
}
