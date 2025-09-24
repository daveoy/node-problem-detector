/*
Copyright 2019 The Kubernetes Authors All rights reserved.

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
package metrics

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"k8s.io/klog/v2"
	
	otelutil "k8s.io/node-problem-detector/pkg/util/otel"
)

// Int64MetricRepresentation represents a parsed Prometheus metric with int64 value
type Int64MetricRepresentation struct {
	Name   string
	Labels map[string]string
	Value  int64
}

// Int64MetricInterface is the interface for int64 metrics
type Int64MetricInterface interface {
	Record(labelValues map[string]string, value int64) error
}

// OTelInt64Metric wraps OpenTelemetry int64 instruments
type OTelInt64Metric struct {
	name        string
	description string
	unit        string
	aggregation Aggregation
	labels      []string
	counter     metric.Int64Counter
	gauge       metric.Int64UpDownCounter
	meter       metric.Meter
	// For LastValue aggregation, track current values to implement SetValue semantics
	gaugeValues map[string]int64
	mutex       sync.RWMutex
}

// Record implements Int64MetricInterface
func (m *OTelInt64Metric) Record(labelValues map[string]string, value int64) error {
	ctx := context.Background()

	// Convert to OTel attributes
	attrs := make([]attribute.KeyValue, 0, len(labelValues))
	for k, v := range labelValues {
		attrs = append(attrs, attribute.String(k, v))
	}

	switch m.aggregation {
	case Sum:
		aggregationMethod = view.Sum()
	default:
		return nil, fmt.Errorf("unknown aggregation option %q", aggregation)
	}

	measure := stats.Int64(viewName, description, unit)
	newView := &view.View{
		Name:        viewName,
		Measure:     measure,
		Description: description,
		Aggregation: aggregationMethod,
		TagKeys:     tagKeys,
	}
	if err := view.Register(newView); err != nil {
		return nil, fmt.Errorf("failed to register view for metric %q: %v", viewName, err)
	}

	metric := Int64Metric{viewName, measure}
	return &metric, nil
}

// Record records a measurement for the metric, with provided tags as metric labels.
func (metric *Int64Metric) Record(tags map[string]string, measurement int64) error {
	var mutators []tag.Mutator

	tagMapMutex.RLock()
	defer tagMapMutex.RUnlock()

	for tagName, tagValue := range tags {
		tagKey, ok := tagMap[tagName]
		if !ok {
			return fmt.Errorf("referencing none existing tag %q in metric %q", tagName, metric.name)
		}
	case LastValue:
		if m.gauge != nil {
			// For LastValue aggregation, we need to set the absolute value
			// First, get the current value and then add the difference
			// Since OTel UpDownCounter only supports Add, we implement SetValue logic
			// by tracking current values and adding the difference
			m.setGaugeValue(ctx, attrs, value)
		}
	default:
		klog.Warningf("Unsupported aggregation type: %v", m.aggregation)
	}

	return nil
}

// setGaugeValue implements SetValue semantics for UpDownCounter
func (m *OTelInt64Metric) setGaugeValue(ctx context.Context, attrs []attribute.KeyValue, newValue int64) {
	// Create a key from the attributes for tracking
	key := m.attributesToKey(attrs)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	currentValue, exists := m.gaugeValues[key]
	if !exists {
		currentValue = 0
	}

	// Calculate the difference needed to reach the target value
	diff := newValue - currentValue

	// Add the difference to reach the target value
	m.gauge.Add(ctx, diff, metric.WithAttributes(attrs...))

	// Update our tracked value
	m.gaugeValues[key] = newValue
}

// attributesToKey converts attributes to a string key for tracking
func (m *OTelInt64Metric) attributesToKey(attrs []attribute.KeyValue) string {
	if len(attrs) == 0 {
		return ""
	}

	// Sort attributes by key to ensure consistent key generation
	attrMap := make(map[string]string, len(attrs))
	keys := make([]string, 0, len(attrs))

	for _, attr := range attrs {
		keyStr := string(attr.Key)
		attrMap[keyStr] = attr.Value.AsString()
		keys = append(keys, keyStr)
	}

	// Sort keys for consistent ordering
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	// Build the key string
	key := ""
	for _, k := range keys {
		if key != "" {
			key += ","
		}
		key += k + "=" + attrMap[k]
	}
	return key
}

// Type aliases for backward compatibility
type Int64Metric = OTelInt64Metric

// NewInt64Metric creates a new Int64 metric using OpenTelemetry
func NewInt64Metric(metricID MetricID, name, description, unit string, aggregation Aggregation, labels []string) (*Int64Metric, error) {
	meter := otelutil.GetGlobalMeter()

	otelMetric := &OTelInt64Metric{
		name:        name,
		description: description,
		unit:        unit,
		aggregation: aggregation,
		labels:      labels,
		meter:       meter,
		gaugeValues: make(map[string]int64),
	}

	var err error
	switch aggregation {
	case Sum:
		otelMetric.counter, err = meter.Int64Counter(
			name,
			metric.WithDescription(description),
			metric.WithUnit(unit),
		)
	case LastValue:
		otelMetric.gauge, err = meter.Int64UpDownCounter(
			name,
			metric.WithDescription(description),
			metric.WithUnit(unit),
		)
	default:
		klog.Warningf("Unsupported aggregation type for metric %s: %v", name, aggregation)
	}

	if err != nil {
		return nil, err
	}

	// Register metric mapping
	MetricMap.AddMapping(metricID, name)

	return otelMetric, nil
}
