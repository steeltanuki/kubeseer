// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/steeltanuki/kubeseer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// StatusSnapshot is an allowlisted public observation used by assertions.
// Result remains in memory for semantic assertions and is never serialized by
// DiagnosticProjection.
type StatusSnapshot struct {
	Generation      int64
	ResourceVersion string
	Conditions      []metav1.Condition
	Summary         *v1alpha1.KubeseerSummary
	ResultHash      string
	Result          *v1alpha1.KubeseerResult
	MetricFamilies  []MetricSample
	RecordCodes     []string
	EventIdentities []string
}

func snapshotOf(object *v1alpha1.Kubeseer) StatusSnapshot {
	var summary *v1alpha1.KubeseerSummary
	if object.Status.Summary != nil {
		copy := *object.Status.Summary
		summary = &copy
	}
	return StatusSnapshot{
		Generation:      object.Generation,
		ResourceVersion: object.ResourceVersion,
		Conditions:      append([]metav1.Condition(nil), object.Status.Conditions...),
		Summary:         summary,
		ResultHash:      object.Status.ResultHash,
		Result:          object.Status.Result,
		MetricFamilies:  nil,
		RecordCodes:     nil,
		EventIdentities: nil,
	}
}

// RecordIdentity is the bounded, allowlisted portion of one manager log
// record. It deliberately carries no free-form message, selector, target,
// or observed value.
type RecordIdentity struct {
	Event     string
	Outcome   string
	Reason    string
	AttemptID string
}

func (r RecordIdentity) Code() string {
	return strings.Join([]string{r.Event, r.Outcome, r.Reason, r.AttemptID}, "|")
}

var allowedRecordEvents = map[string]struct{}{
	"ReconciliationStarted": {}, "ReconciliationSkipped": {},
	"ReconciliationRetryScheduled": {}, "ReconciliationFailed": {},
	"ReconciliationCompleted": {}, "SourceFailed": {},
	"StatusUpdateWritten": {}, "StatusUpdateSkipped": {},
	"AuthorizationDecision": {}, "AuthorizationReadForbidden": {},
	"SourceWatchStopped": {}, "SourceWatchRestarted": {}, "JSONPathFailed": {},
}

// ReadManagerRecordIdentities reads only a bounded tail of the current
// manager Pod log and extracts closed-vocabulary records for one owner. Raw
// lines never leave this method and are not eligible for diagnostics.
func (s *ClusterSession) ReadManagerRecordIdentities(ctx context.Context, namespace, owner string) ([]RecordIdentity, error) {
	if strings.TrimSpace(namespace) == "" || strings.TrimSpace(owner) == "" {
		return nil, errors.New("manager log identity requires namespace and owner")
	}
	pod, err := managerPodMaybe(s, ctx)
	if err != nil {
		return nil, err
	}
	if pod == nil {
		return nil, errors.New("manager Pod is not present")
	}
	tailLines := int64(500)
	body, err := s.Core.CoreV1().Pods(s.Metadata.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "manager", TailLines: &tailLines}).Do(ctx).Raw()
	if err != nil {
		return nil, err
	}
	identities := make([]RecordIdentity, 0)
	for _, line := range strings.Split(string(body), "\n") {
		if logField(line, "name") != owner {
			continue
		}
		identity := RecordIdentity{
			Event:     logField(line, "event"),
			Outcome:   logField(line, "outcome"),
			Reason:    logField(line, "reason"),
			AttemptID: logField(line, "attempt_id"),
		}
		if _, allowed := allowedRecordEvents[identity.Event]; !allowed || identity.AttemptID == "" {
			continue
		}
		identities = append(identities, identity)
	}
	return identities, nil
}

func logField(line, key string) string {
	markers := []string{`"` + key + `"=`, key + "=", `"` + key + `":`, key + ":"}
	for _, marker := range markers {
		index := strings.Index(line, marker)
		if index < 0 {
			continue
		}
		value := strings.TrimLeft(line[index+len(marker):], " \t:=\"'")
		if end := strings.IndexAny(value, " \t,}]\"'"); end >= 0 {
			value = value[:end]
		}
		return value
	}
	return ""
}

// WaitForStatus watches from a captured resource version and relists after a
// closed/expired watch. It is bounded by the caller's context deadline.
func (s *ClusterSession) WaitForStatus(ctx context.Context, scenarioID, namespace, name, awaited string, predicate func(StatusSnapshot) bool) (StatusSnapshot, error) {
	if predicate == nil {
		return StatusSnapshot{}, errors.New("status predicate is required")
	}
	var last StatusSnapshot
	for {
		object, err := s.GetKubeseer(ctx, namespace, name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return last, fmt.Errorf("SCENARIO=%s awaited=%s: Kubeseer %s/%s not found: %w", scenarioID, awaited, namespace, name, err)
			}
			return last, fmt.Errorf("SCENARIO=%s awaited=%s: read status: %w", scenarioID, awaited, err)
		}
		last = snapshotOf(object)
		if predicate(last) {
			return last, nil
		}
		resourceVersion := object.ResourceVersion
		stream, err := s.Dynamic.Resource(KubeseerResource).Namespace(namespace).Watch(ctx, metav1.ListOptions{ResourceVersion: resourceVersion})
		if err != nil {
			if apierrors.IsResourceExpired(err) {
				continue
			}
			return last, fmt.Errorf("SCENARIO=%s awaited=%s: start status watch: %w", scenarioID, awaited, err)
		}
		watchEnded := false
		for !watchEnded {
			select {
			case <-ctx.Done():
				stream.Stop()
				return last, fmt.Errorf("SCENARIO=%s awaited=%s: %w", scenarioID, awaited, ctx.Err())
			case event, open := <-stream.ResultChan():
				if !open {
					watchEnded = true
					continue
				}
				if event.Type == watch.Error {
					stream.Stop()
					watchEnded = true
					continue
				}
				object, ok := event.Object.(*unstructured.Unstructured)
				if !ok {
					continue
				}
				encoded, err := object.MarshalJSON()
				if err != nil {
					stream.Stop()
					return last, err
				}
				decoded := &v1alpha1.Kubeseer{}
				if err := json.Unmarshal(encoded, decoded); err != nil {
					stream.Stop()
					return last, err
				}
				last = snapshotOf(decoded)
				if predicate(last) {
					stream.Stop()
					return last, nil
				}
			}
		}
		stream.Stop()
	}
}

// WaitForResourceVersion waits for one public object update using bounded
// polling, useful when a status subresource watch is not available.
func (s *ClusterSession) WaitForResourceVersion(ctx context.Context, scenarioID, namespace, name, initial string) (string, error) {
	var current string
	err := s.WaitFor(ctx, scenarioID, "resourceVersion changed", func(waitCtx context.Context) (bool, error) {
		object, err := s.GetKubeseer(waitCtx, namespace, name)
		if err != nil {
			return false, err
		}
		current = object.ResourceVersion
		return current != "" && current != initial, nil
	})
	return current, err
}

// QuietStatusWindow watches one public Kubeseer object from the last observed
// resourceVersion and succeeds only when the bounded window closes without a
// subsequent object event. It avoids repeated API GETs while the cluster is
// processing a large fixture cleanup.
func (s *ClusterSession) QuietStatusWindow(ctx context.Context, scenarioID, namespace, name, expectedResourceVersion string, interval time.Duration) error {
	if ctx == nil || strings.TrimSpace(scenarioID) == "" || strings.TrimSpace(namespace) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(expectedResourceVersion) == "" || interval <= 0 {
		return errors.New("quiet status window arguments are invalid")
	}
	windowCtx, cancel := context.WithTimeout(ctx, interval)
	defer cancel()
	stream, err := s.Dynamic.Resource(KubeseerResource).Namespace(namespace).Watch(windowCtx, metav1.ListOptions{
		FieldSelector:       "metadata.name=" + name,
		ResourceVersion:     expectedResourceVersion,
		AllowWatchBookmarks: true,
	})
	if err != nil {
		return fmt.Errorf("SCENARIO=%s quiet status watch: %w", scenarioID, err)
	}
	defer stream.Stop()
	for {
		select {
		case <-windowCtx.Done():
			if errors.Is(windowCtx.Err(), context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("SCENARIO=%s quiet status watch: %w", scenarioID, windowCtx.Err())
		case event, open := <-stream.ResultChan():
			if !open {
				return fmt.Errorf("SCENARIO=%s quiet status watch closed before the window elapsed", scenarioID)
			}
			if event.Type == watch.Bookmark {
				continue
			}
			if event.Type == watch.Error {
				return fmt.Errorf("SCENARIO=%s quiet status watch returned an error", scenarioID)
			}
			return fmt.Errorf("SCENARIO=%s quiet status observed an object event", scenarioID)
		}
	}
}

// MetricSample is a bounded cardinality Prometheus sample. Labels are not
// carried because scenario diagnostics must not expose high-cardinality data.
type MetricSample struct {
	Family string
	Value  float64
}

func ParseMetricFamilies(body string, allowed map[string]struct{}) ([]MetricSample, error) {
	if allowed == nil {
		return nil, errors.New("metric allowlist is required")
	}
	var samples []MetricSample
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		family := strings.SplitN(parts[0], "{", 2)[0]
		if _, ok := allowed[family]; !ok {
			continue
		}
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return nil, fmt.Errorf("metric %s is not numeric", family)
		}
		samples = append(samples, MetricSample{Family: family, Value: value})
	}
	return samples, nil
}

// ListEvents returns stable Event identities only; messages and involved
// object bodies are intentionally discarded.
func (s *ClusterSession) ListEvents(ctx context.Context, namespace string, fieldSelector string) ([]string, error) {
	events, err := s.Core.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{FieldSelector: fieldSelector, Limit: 100})
	if err != nil {
		return nil, err
	}
	identities := make([]string, 0, len(events.Items))
	for _, event := range events.Items {
		identities = append(identities, string(event.UID)+"/"+event.Reason+"/"+string(event.Type))
	}
	return identities, nil
}

// WaitForEvent waits for a matching public Event identity without printing its
// free-form message.
func (s *ClusterSession) WaitForEvent(ctx context.Context, namespace, fieldSelector, reason string) error {
	return s.WaitFor(ctx, "event", "Kubernetes Event "+reason, func(waitCtx context.Context) (bool, error) {
		events, err := s.ListEvents(waitCtx, namespace, fieldSelector)
		if err != nil {
			return false, err
		}
		for _, identity := range events {
			if strings.Contains(identity, "/"+reason+"/") {
				return true, nil
			}
		}
		return false, nil
	})
}

// QuietWindow observes absence after a positive causation signal. It is not a
// semantic assertion helper and never uses an arbitrary delay.
func QuietWindow(ctx context.Context, interval time.Duration, observe func(context.Context) (string, error), expected string) error {
	if interval <= 0 || observe == nil {
		return errors.New("quiet window requires a positive interval and observer")
	}
	windowCtx, cancel := context.WithTimeout(ctx, interval)
	defer cancel()
	deadline, _ := windowCtx.Deadline()
	var initial string
	observed := false
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		observationTimeout := remaining
		if observationTimeout > 2*time.Second {
			observationTimeout = 2 * time.Second
		}
		observationCtx, observationCancel := context.WithTimeout(windowCtx, observationTimeout)
		value, err := observe(observationCtx)
		observationCancel()
		if err != nil {
			lastErr = err
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				return err
			}
		} else {
			observed = true
			if initial == "" {
				initial = value
			}
			if value != expected {
				return fmt.Errorf("quiet window observed unexpected change")
			}
		}
		// Bound the observation cadence so an absence proof cannot flood the API
		// server while still checking the public value throughout the window.
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-windowCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if observed {
				return nil
			}
			if lastErr != nil {
				return lastErr
			}
			return nil
		case <-timer.C:
		}
	}
}

var _ = corev1.Event{}
