/*
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package featureflag

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NVIDIA/nvcf/src/compute-plane-services/nvca/pkg/apis/nvca/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// minBYOOMemoryBytes is the floor for a per-workload BYOO collector memory override.
// Overrides below 1Gi are rejected and the safe cluster default is used instead.
const minBYOOMemoryBytes = 1 << 30 // 1Gi

const (
	// WorkloadConfigConfigMapName is the fixed name of the ConfigMap a chart author may include
	// to supply workload-specific configuration. The ConfigMap is never created on-cluster; it is
	// read from the ReVal-rendered objects by the MiniService controller and then dropped from the
	// set of objects that are applied.
	WorkloadConfigConfigMapName = "nvcf-workload-config"

	// WorkloadConfigDataKey is the ConfigMap data key holding the workload config YAML document.
	WorkloadConfigDataKey = "config.yaml"
)

// Workload feature flag keys recognized under the workload config featureFlags map.
const (
	// StatusByWorkerReadiness directs the MiniService controller to only consider worker
	// container readiness when determining MiniService readiness, rather than aggressively
	// accounting for the health of all workload objects.
	StatusByWorkerReadiness = "StatusByWorkerReadiness"
)

// workloadFeatureFlagKeys is the set of recognized workload feature flag keys. Unrecognized
// keys are ignored (with a warning) by DecodeWorkloadConfig.
var workloadFeatureFlagKeys = map[string]struct{}{
	StatusByWorkerReadiness: {},
}

// workloadConfigRaw mirrors v1alpha1.WorkloadConfig for decoding, but captures the BYOO
// resource override as raw JSON. This defers quantity parsing so a malformed CPU/memory
// value can be dropped (and logged) instead of failing the whole config decode, which the
// caller would otherwise turn into a terminal reconcile error.
type workloadConfigRaw struct {
	FeatureFlags  map[string]bool `json:"featureFlags,omitempty"`
	BYOOResources json.RawMessage `json:"byooResources,omitempty"`
}

// DecodeWorkloadConfig decodes the workload config ConfigMap into a WorkloadConfig. The
// config is read from the WorkloadConfigDataKey data key as a YAML document. Unrecognized
// feature flags are dropped and logged as a warning. A nil ConfigMap yields a zero config.
func DecodeWorkloadConfig(ctx context.Context, log logr.Logger, cm *corev1.ConfigMap) (*v1alpha1.WorkloadConfig, error) {
	if cm == nil {
		return nil, nil
	}

	raw, ok := cm.Data[WorkloadConfigDataKey]
	if !ok || raw == "" {
		log.Info("Ignoring empty workload config in ConfigMap %q", WorkloadConfigConfigMapName)
		return nil, nil
	}
	var rawCfg workloadConfigRaw
	if err := yaml.Unmarshal([]byte(raw), &rawCfg); err != nil {
		return nil, err
	}

	cfg := v1alpha1.WorkloadConfig{FeatureFlags: rawCfg.FeatureFlags}
	for key := range cfg.FeatureFlags {
		if _, known := workloadFeatureFlagKeys[key]; !known {
			log.Info("Ignoring unknown workload feature flag %q in ConfigMap %q", key, WorkloadConfigConfigMapName)
			delete(cfg.FeatureFlags, key)
		}
	}

	// Decode and validate the BYOO override separately: a malformed or unsafe value is
	// dropped (and logged) rather than failing the deploy, so translate then falls back
	// to the safe cluster-level default.
	if len(rawCfg.BYOOResources) > 0 {
		var rr corev1.ResourceRequirements
		if err := json.Unmarshal(rawCfg.BYOOResources, &rr); err != nil {
			log.Error(err, "Ignoring malformed byooResources", "configMap", WorkloadConfigConfigMapName)
		} else if err := validateBYOOResources(&rr); err != nil {
			log.Error(err, "Ignoring invalid byooResources", "configMap", WorkloadConfigConfigMapName)
		} else {
			cfg.BYOOResources = &rr
		}
	}
	return &cfg, nil
}

// validateBYOOResources rejects a per-workload BYOO collector resource override that
// would be unsafe: every specified quantity must be positive, and any memory quantity
// must be at least 1Gi.
func validateBYOOResources(rr *corev1.ResourceRequirements) error {
	for kind, list := range map[string]corev1.ResourceList{"requests": rr.Requests, "limits": rr.Limits} {
		for name, q := range list {
			if q.Sign() <= 0 {
				return fmt.Errorf("%s.%s must be positive (got %q)", kind, name, q.String())
			}
			if name == corev1.ResourceMemory && q.CmpInt64(minBYOOMemoryBytes) < 0 {
				return fmt.Errorf("%s.memory must be at least 1Gi (got %q)", kind, q.String())
			}
		}
	}
	return nil
}
