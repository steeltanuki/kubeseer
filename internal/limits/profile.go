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

// Package limits owns the immutable manager-wide performance profile and the
// canonical byte accounting shared by the runtime's bounded stages. It has no
// dependency on a domain package so every adapter can depend on it without a
// package cycle.
package limits

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	DefaultPageSize                int64 = 500
	DefaultMaxMatchedResources     int   = 1000
	DefaultSelectedInputBytes      int64 = 8 * 1024 * 1024
	DefaultProducedValueBytes      int64 = 8 * 1024 * 1024
	DefaultMaxStatusBytes          int64 = 512 * 1024
	DefaultEvaluationTimeout             = 30 * time.Second
	DefaultMaxConcurrentReconciles int   = 4
	DefaultDiscoveryCacheEntries   int   = 1024
	DefaultDiscoveryCacheTTL             = 5 * time.Minute
	DefaultMaxActiveWatches        int   = 1024
	DefaultMaxPendingTriggers      int   = 4096

	DefaultMaxKubeseerSources          int = 32
	DefaultMaxSourceNamespaces         int = 64
	DefaultMaxSourceFields             int = 64
	DefaultMaxFieldOperators           int = 16
	DefaultMaxOperatorValues           int = 128
	DefaultMaxSourceAggregations       int = 32
	DefaultMaxAggregationGroupBy       int = 16
	DefaultMaxSelectorMatchExpressions int = 64
	DefaultMaxSelectorExpressionValues int = 64
	DefaultMaxSelectorMatchLabels      int = 64
	DefaultMaxPolicyNamespaceNames     int = 256
	DefaultMaxPolicyResourceRules      int = 128
	DefaultMaxPolicyAPIGroups          int = 64
	DefaultMaxPolicyKinds              int = 64
	DefaultMaxCanonicalSpecBytes       int = 262144

	DefaultMaxAggregationGroups          int = 1000
	DefaultMaxAggregationContributions   int = 10000
	DefaultMaxAggregationCollectedValues int = 10000
	DefaultMaxAggregationDistinctValues  int = 10000
	DefaultMaxAggregationProvenance      int = 10000
)

// AdmissionOverrides contains optional admission-budget changes. A nil
// member is omitted; a non-nil zero or negative member is invalid.
type AdmissionOverrides struct {
	MaxKubeseerSources          *int
	MaxSourceNamespaces         *int
	MaxSourceFields             *int
	MaxFieldOperators           *int
	MaxOperatorValues           *int
	MaxSourceAggregations       *int
	MaxAggregationGroupBy       *int
	MaxSelectorMatchExpressions *int
	MaxSelectorExpressionValues *int
	MaxSelectorMatchLabels      *int
	MaxPolicyNamespaceNames     *int
	MaxPolicyResourceRules      *int
	MaxPolicyAPIGroups          *int
	MaxPolicyKinds              *int
	MaxKubeseerSpecBytes        *int
	MaxAccessPolicySpecBytes    *int
}

// AggregationOverrides contains optional per-aggregate cardinality changes.
type AggregationOverrides struct {
	MaxGroups            *int
	MaxContributions     *int
	MaxCollectedValues   *int
	MaxDistinctValues    *int
	MaxProvenanceEntries *int
}

// Overrides contains the manager-wide performance settings. It is intended to
// be assembled at the composition root and resolved exactly once.
type Overrides struct {
	PageSize                *int64
	MaxMatchedResources     *int
	MaxSelectedInputBytes   *int64
	MaxProducedValueBytes   *int64
	MaxStatusBytes          *int64
	EvaluationTimeout       *time.Duration
	MaxConcurrentReconciles *int
	DiscoveryCacheEntries   *int
	DiscoveryCacheTTL       *time.Duration
	MaxActiveWatches        *int
	MaxPendingTriggers      *int
	Admission               AdmissionOverrides
	Aggregation             AggregationOverrides
}

// Profile is an immutable, positive effective limit profile. Its concrete
// fields are private so callers can only observe copied scalar values.
type Profile struct {
	pageSize                int64
	maxMatchedResources     int
	maxSelectedInputBytes   int64
	maxProducedValueBytes   int64
	maxStatusBytes          int64
	evaluationTimeout       time.Duration
	maxConcurrentReconciles int
	discoveryCacheEntries   int
	discoveryCacheTTL       time.Duration
	maxActiveWatches        int
	maxPendingTriggers      int
	admission               AdmissionBudgets
	aggregation             AggregationBudgets
}

// AdmissionBudgets is the domain-neutral view consumed by the admission
// adapter. It deliberately contains values, never pointers.
type AdmissionBudgets struct {
	MaxKubeseerSources          int
	MaxSourceNamespaces         int
	MaxSourceFields             int
	MaxFieldOperators           int
	MaxOperatorValues           int
	MaxSourceAggregations       int
	MaxAggregationGroupBy       int
	MaxSelectorMatchExpressions int
	MaxSelectorExpressionValues int
	MaxSelectorMatchLabels      int
	MaxPolicyNamespaceNames     int
	MaxPolicyResourceRules      int
	MaxPolicyAPIGroups          int
	MaxPolicyKinds              int
	MaxKubeseerSpecBytes        int
	MaxAccessPolicySpecBytes    int
}

// AggregationBudgets is the domain-neutral view consumed by the aggregation
// adapter. It deliberately contains values, never pointers.
type AggregationBudgets struct {
	MaxGroups            int
	MaxContributions     int
	MaxCollectedValues   int
	MaxDistinctValues    int
	MaxProvenanceEntries int
}

// ReasonInvalidLimitConfiguration is the stable setup-validation reason.
const ReasonInvalidLimitConfiguration = "InvalidLimitConfiguration"

// InvalidConfigurationError identifies one invalid override without retaining
// a mutable configuration object.
type InvalidConfigurationError struct {
	Field string
	Value string
	Cause error
}

func (e *InvalidConfigurationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field == "" {
		return ReasonInvalidLimitConfiguration
	}
	return fmt.Sprintf("%s: %s", ReasonInvalidLimitConfiguration, e.Field)
}

func (e *InvalidConfigurationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// DefaultProfile returns a fresh value containing every approved default.
func DefaultProfile() Profile {
	return Profile{
		pageSize:                DefaultPageSize,
		maxMatchedResources:     DefaultMaxMatchedResources,
		maxSelectedInputBytes:   DefaultSelectedInputBytes,
		maxProducedValueBytes:   DefaultProducedValueBytes,
		maxStatusBytes:          DefaultMaxStatusBytes,
		evaluationTimeout:       DefaultEvaluationTimeout,
		maxConcurrentReconciles: DefaultMaxConcurrentReconciles,
		discoveryCacheEntries:   DefaultDiscoveryCacheEntries,
		discoveryCacheTTL:       DefaultDiscoveryCacheTTL,
		maxActiveWatches:        DefaultMaxActiveWatches,
		maxPendingTriggers:      DefaultMaxPendingTriggers,
		admission: AdmissionBudgets{
			MaxKubeseerSources:          DefaultMaxKubeseerSources,
			MaxSourceNamespaces:         DefaultMaxSourceNamespaces,
			MaxSourceFields:             DefaultMaxSourceFields,
			MaxFieldOperators:           DefaultMaxFieldOperators,
			MaxOperatorValues:           DefaultMaxOperatorValues,
			MaxSourceAggregations:       DefaultMaxSourceAggregations,
			MaxAggregationGroupBy:       DefaultMaxAggregationGroupBy,
			MaxSelectorMatchExpressions: DefaultMaxSelectorMatchExpressions,
			MaxSelectorExpressionValues: DefaultMaxSelectorExpressionValues,
			MaxSelectorMatchLabels:      DefaultMaxSelectorMatchLabels,
			MaxPolicyNamespaceNames:     DefaultMaxPolicyNamespaceNames,
			MaxPolicyResourceRules:      DefaultMaxPolicyResourceRules,
			MaxPolicyAPIGroups:          DefaultMaxPolicyAPIGroups,
			MaxPolicyKinds:              DefaultMaxPolicyKinds,
			MaxKubeseerSpecBytes:        DefaultMaxCanonicalSpecBytes,
			MaxAccessPolicySpecBytes:    DefaultMaxCanonicalSpecBytes,
		},
		aggregation: AggregationBudgets{
			MaxGroups:            DefaultMaxAggregationGroups,
			MaxContributions:     DefaultMaxAggregationContributions,
			MaxCollectedValues:   DefaultMaxAggregationCollectedValues,
			MaxDistinctValues:    DefaultMaxAggregationDistinctValues,
			MaxProvenanceEntries: DefaultMaxAggregationProvenance,
		},
	}
}

// Resolve validates and applies the supplied pointer overrides. The returned
// profile contains no pointers into overrides and can safely be shared.
func Resolve(overrides Overrides) (Profile, error) {
	profile := DefaultProfile()
	var err error
	if profile.pageSize, err = positiveInt64("pageSize", overrides.PageSize, profile.pageSize); err != nil {
		return Profile{}, err
	}
	if profile.maxMatchedResources, err = positiveInt("maxMatchedResources", overrides.MaxMatchedResources, profile.maxMatchedResources); err != nil {
		return Profile{}, err
	}
	if profile.maxSelectedInputBytes, err = positiveInt64("maxSelectedInputBytes", overrides.MaxSelectedInputBytes, profile.maxSelectedInputBytes); err != nil {
		return Profile{}, err
	}
	if profile.maxProducedValueBytes, err = positiveInt64("maxProducedValueBytes", overrides.MaxProducedValueBytes, profile.maxProducedValueBytes); err != nil {
		return Profile{}, err
	}
	if profile.maxStatusBytes, err = positiveInt64("maxStatusBytes", overrides.MaxStatusBytes, profile.maxStatusBytes); err != nil {
		return Profile{}, err
	}
	if profile.evaluationTimeout, err = positiveDuration("evaluationTimeout", overrides.EvaluationTimeout, profile.evaluationTimeout); err != nil {
		return Profile{}, err
	}
	if profile.maxConcurrentReconciles, err = positiveInt("maxConcurrentReconciles", overrides.MaxConcurrentReconciles, profile.maxConcurrentReconciles); err != nil {
		return Profile{}, err
	}
	if profile.discoveryCacheEntries, err = positiveInt("discoveryCacheEntries", overrides.DiscoveryCacheEntries, profile.discoveryCacheEntries); err != nil {
		return Profile{}, err
	}
	if profile.discoveryCacheTTL, err = positiveDuration("discoveryCacheTTL", overrides.DiscoveryCacheTTL, profile.discoveryCacheTTL); err != nil {
		return Profile{}, err
	}
	if profile.maxActiveWatches, err = positiveInt("maxActiveWatches", overrides.MaxActiveWatches, profile.maxActiveWatches); err != nil {
		return Profile{}, err
	}
	if profile.maxPendingTriggers, err = positiveInt("maxPendingTriggers", overrides.MaxPendingTriggers, profile.maxPendingTriggers); err != nil {
		return Profile{}, err
	}
	if profile.admission, err = resolveAdmission(overrides.Admission, profile.admission); err != nil {
		return Profile{}, err
	}
	if profile.aggregation, err = resolveAggregation(overrides.Aggregation, profile.aggregation); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func positiveInt(field string, override *int, fallback int) (int, error) {
	if override == nil {
		return fallback, nil
	}
	if *override <= 0 {
		return 0, invalid(field, fmt.Sprint(*override))
	}
	return *override, nil
}

func positiveInt64(field string, override *int64, fallback int64) (int64, error) {
	if override == nil {
		return fallback, nil
	}
	if *override <= 0 {
		return 0, invalid(field, fmt.Sprint(*override))
	}
	return *override, nil
}

func positiveDuration(field string, override *time.Duration, fallback time.Duration) (time.Duration, error) {
	if override == nil {
		return fallback, nil
	}
	if *override <= 0 {
		return 0, invalid(field, override.String())
	}
	return *override, nil
}

func invalid(field, value string) error {
	return &InvalidConfigurationError{Field: field, Value: value, Cause: errors.New("override must be positive")}
}

func resolveAdmission(overrides AdmissionOverrides, defaults AdmissionBudgets) (AdmissionBudgets, error) {
	var err error
	if defaults.MaxKubeseerSources, err = boundedAdmission("admission.maxKubeseerSources", overrides.MaxKubeseerSources, defaults.MaxKubeseerSources, DefaultMaxKubeseerSources); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxSourceNamespaces, err = boundedAdmission("admission.maxSourceNamespaces", overrides.MaxSourceNamespaces, defaults.MaxSourceNamespaces, DefaultMaxSourceNamespaces); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxSourceFields, err = boundedAdmission("admission.maxSourceFields", overrides.MaxSourceFields, defaults.MaxSourceFields, DefaultMaxSourceFields); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxFieldOperators, err = boundedAdmission("admission.maxFieldOperators", overrides.MaxFieldOperators, defaults.MaxFieldOperators, DefaultMaxFieldOperators); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxOperatorValues, err = boundedAdmission("admission.maxOperatorValues", overrides.MaxOperatorValues, defaults.MaxOperatorValues, DefaultMaxOperatorValues); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxSourceAggregations, err = boundedAdmission("admission.maxSourceAggregations", overrides.MaxSourceAggregations, defaults.MaxSourceAggregations, DefaultMaxSourceAggregations); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxAggregationGroupBy, err = boundedAdmission("admission.maxAggregationGroupBy", overrides.MaxAggregationGroupBy, defaults.MaxAggregationGroupBy, DefaultMaxAggregationGroupBy); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxSelectorMatchExpressions, err = boundedAdmission("admission.maxSelectorMatchExpressions", overrides.MaxSelectorMatchExpressions, defaults.MaxSelectorMatchExpressions, DefaultMaxSelectorMatchExpressions); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxSelectorExpressionValues, err = boundedAdmission("admission.maxSelectorExpressionValues", overrides.MaxSelectorExpressionValues, defaults.MaxSelectorExpressionValues, DefaultMaxSelectorExpressionValues); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxSelectorMatchLabels, err = boundedAdmission("admission.maxSelectorMatchLabels", overrides.MaxSelectorMatchLabels, defaults.MaxSelectorMatchLabels, DefaultMaxSelectorMatchLabels); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxPolicyNamespaceNames, err = boundedAdmission("admission.maxPolicyNamespaceNames", overrides.MaxPolicyNamespaceNames, defaults.MaxPolicyNamespaceNames, DefaultMaxPolicyNamespaceNames); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxPolicyResourceRules, err = boundedAdmission("admission.maxPolicyResourceRules", overrides.MaxPolicyResourceRules, defaults.MaxPolicyResourceRules, DefaultMaxPolicyResourceRules); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxPolicyAPIGroups, err = boundedAdmission("admission.maxPolicyAPIGroups", overrides.MaxPolicyAPIGroups, defaults.MaxPolicyAPIGroups, DefaultMaxPolicyAPIGroups); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxPolicyKinds, err = boundedAdmission("admission.maxPolicyKinds", overrides.MaxPolicyKinds, defaults.MaxPolicyKinds, DefaultMaxPolicyKinds); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxKubeseerSpecBytes, err = boundedAdmission("admission.maxKubeseerSpecBytes", overrides.MaxKubeseerSpecBytes, defaults.MaxKubeseerSpecBytes, DefaultMaxCanonicalSpecBytes); err != nil {
		return AdmissionBudgets{}, err
	}
	if defaults.MaxAccessPolicySpecBytes, err = boundedAdmission("admission.maxAccessPolicySpecBytes", overrides.MaxAccessPolicySpecBytes, defaults.MaxAccessPolicySpecBytes, DefaultMaxCanonicalSpecBytes); err != nil {
		return AdmissionBudgets{}, err
	}
	return defaults, nil
}

func boundedAdmission(field string, override *int, fallback, ceiling int) (int, error) {
	value, err := positiveInt(field, override, fallback)
	if err != nil {
		return 0, err
	}
	if value > ceiling {
		return 0, &InvalidConfigurationError{Field: field, Value: fmt.Sprint(value), Cause: errors.New("override exceeds generated structural ceiling")}
	}
	return value, nil
}

func resolveAggregation(overrides AggregationOverrides, defaults AggregationBudgets) (AggregationBudgets, error) {
	var err error
	if defaults.MaxGroups, err = positiveInt("aggregation.maxGroups", overrides.MaxGroups, defaults.MaxGroups); err != nil {
		return AggregationBudgets{}, err
	}
	if defaults.MaxContributions, err = positiveInt("aggregation.maxContributions", overrides.MaxContributions, defaults.MaxContributions); err != nil {
		return AggregationBudgets{}, err
	}
	if defaults.MaxCollectedValues, err = positiveInt("aggregation.maxCollectedValues", overrides.MaxCollectedValues, defaults.MaxCollectedValues); err != nil {
		return AggregationBudgets{}, err
	}
	if defaults.MaxDistinctValues, err = positiveInt("aggregation.maxDistinctValues", overrides.MaxDistinctValues, defaults.MaxDistinctValues); err != nil {
		return AggregationBudgets{}, err
	}
	if defaults.MaxProvenanceEntries, err = positiveInt("aggregation.maxProvenanceEntries", overrides.MaxProvenanceEntries, defaults.MaxProvenanceEntries); err != nil {
		return AggregationBudgets{}, err
	}
	return defaults, nil
}

// PageSize returns the effective LIST page size.
func (p Profile) PageSize() int64 { return p.pageSize }

// MaxMatchedResources returns the unique selected-resource ceiling.
func (p Profile) MaxMatchedResources() int { return p.maxMatchedResources }

// MaxSelectedInputBytes returns the canonical selected-input ceiling.
func (p Profile) MaxSelectedInputBytes() int64 { return p.maxSelectedInputBytes }

// MaxProducedValueBytes returns the canonical per-stage produced-value ceiling.
func (p Profile) MaxProducedValueBytes() int64 { return p.maxProducedValueBytes }

// MaxStatusBytes returns the canonical status candidate ceiling.
func (p Profile) MaxStatusBytes() int64 { return p.maxStatusBytes }

// EvaluationTimeout returns the evaluation deadline.
func (p Profile) EvaluationTimeout() time.Duration { return p.evaluationTimeout }

// MaxConcurrentReconciles returns the worker ceiling.
func (p Profile) MaxConcurrentReconciles() int { return p.maxConcurrentReconciles }

// DiscoveryCacheEntries returns the metadata-cache entry ceiling.
func (p Profile) DiscoveryCacheEntries() int { return p.discoveryCacheEntries }

// DiscoveryCacheTTL returns the metadata-cache freshness interval.
func (p Profile) DiscoveryCacheTTL() time.Duration { return p.discoveryCacheTTL }

// MaxActiveWatches returns the shared exact-watch ceiling.
func (p Profile) MaxActiveWatches() int { return p.maxActiveWatches }

// MaxPendingTriggers returns the pending identity ceiling.
func (p Profile) MaxPendingTriggers() int { return p.maxPendingTriggers }

// Admission returns a copied admission budget view.
func (p Profile) Admission() AdmissionBudgets { return p.admission }

// Aggregation returns a copied aggregation budget view.
func (p Profile) Aggregation() AggregationBudgets { return p.aggregation }

// Valid reports whether the profile contains a complete positive effective
// configuration. Profiles returned by Resolve and DefaultProfile are valid;
// the guard also protects composition roots from an accidental zero value.
func (p Profile) Valid() bool {
	return p.pageSize > 0 && p.maxMatchedResources > 0 && p.maxSelectedInputBytes > 0 &&
		p.maxProducedValueBytes > 0 && p.maxStatusBytes > 0 && p.evaluationTimeout > 0 &&
		p.maxConcurrentReconciles > 0 && p.discoveryCacheEntries > 0 && p.discoveryCacheTTL > 0 &&
		p.maxActiveWatches > 0 && p.maxPendingTriggers > 0 && p.admission.MaxKubeseerSources > 0 &&
		p.aggregation.MaxGroups > 0
}

// CanonicalSize returns the byte length of the exact deterministic JSON
// representation that would be retained. encoding/json sorts map keys and
// emits the same structural shape used by the API adapters.
func CanonicalSize(value any) (int, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return len(encoded), nil
}

// ErrLimitExceeded identifies an attempted accountant commit beyond its
// configured ceiling.
var ErrLimitExceeded = errors.New("limit exceeded")

// ErrAccountingOverflow identifies an integer overflow while adding bytes.
var ErrAccountingOverflow = errors.New("accounting overflow")

// Accountant is a source/stage-local canonical byte counter. It stores no
// encoded payload and is safe to discard at a source boundary.
type Accountant struct {
	ceiling   int64
	used      int64
	dimension string
}

// NewAccountant creates a counter for one positive ceiling.
func NewAccountant(ceiling int64, dimension string) (*Accountant, error) {
	if ceiling <= 0 {
		return nil, invalid("accountant."+dimension, fmt.Sprint(ceiling))
	}
	return &Accountant{ceiling: ceiling, dimension: dimension}, nil
}

// Dimension returns the fixed diagnostic dimension.
func (a *Accountant) Dimension() string {
	if a == nil {
		return ""
	}
	return a.dimension
}

// Ceiling returns the configured byte ceiling.
func (a *Accountant) Ceiling() int64 {
	if a == nil {
		return 0
	}
	return a.ceiling
}

// Used returns bytes committed so far.
func (a *Accountant) Used() int64 {
	if a == nil {
		return 0
	}
	return a.used
}

// WouldExceed measures a value without changing the counter.
func (a *Accountant) WouldExceed(value any) (bool, error) {
	size, err := CanonicalSize(value)
	if err != nil {
		return false, err
	}
	return a.WouldExceedBytes(int64(size))
}

// WouldExceedBytes checks a byte increment without changing the counter.
func (a *Accountant) WouldExceedBytes(size int64) (bool, error) {
	if a == nil {
		return false, errors.New("accountant is nil")
	}
	if size < 0 || a.used > math.MaxInt64-size {
		return true, ErrAccountingOverflow
	}
	return a.used+size > a.ceiling, nil
}

// Add measures and commits one value if it fits.
func (a *Accountant) Add(value any) error {
	size, err := CanonicalSize(value)
	if err != nil {
		return err
	}
	return a.AddBytes(int64(size))
}

// AddBytes commits one canonical byte count if it fits.
func (a *Accountant) AddBytes(size int64) error {
	if a == nil {
		return errors.New("accountant is nil")
	}
	exceeds, err := a.WouldExceedBytes(size)
	if err != nil {
		return err
	}
	if exceeds {
		return ErrLimitExceeded
	}
	a.used += size
	return nil
}

// ReleaseBytes rolls back a previously committed source-local increment. It
// is used only when a paginated target is restarted after an expired resource
// version and never permits the counter to become negative.
func (a *Accountant) ReleaseBytes(size int64) error {
	if a == nil {
		return errors.New("accountant is nil")
	}
	if size < 0 || size > a.used {
		return ErrAccountingOverflow
	}
	a.used -= size
	return nil
}
