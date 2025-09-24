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
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"k8s.io/klog/v2"
)

const (
	CPURunnableTaskCountID  MetricID = "cpu/runnable_task_count"
	CPUUsageTimeID          MetricID = "cpu/usage_time"
	CPULoad1m               MetricID = "cpu/load_1m"
	CPULoad5m               MetricID = "cpu/load_5m"
	CPULoad15m              MetricID = "cpu/load_15m"
	ProblemCounterID        MetricID = "problem_counter"
	ProblemGaugeID          MetricID = "problem_gauge"
	DiskIOTimeID            MetricID = "disk/io_time"
	DiskWeightedIOID        MetricID = "disk/weighted_io"
	DiskAvgQueueLenID       MetricID = "disk/avg_queue_len"
	DiskOpsCountID          MetricID = "disk/operation_count"
	DiskMergedOpsCountID    MetricID = "disk/merged_operation_count"
	DiskOpsBytesID          MetricID = "disk/operation_bytes_count"
	DiskOpsTimeID           MetricID = "disk/operation_time"
	DiskBytesUsedID         MetricID = "disk/bytes_used"
	DiskPercentUsedID       MetricID = "disk/percent_used"
	HostUptimeID            MetricID = "host/uptime"
	MemoryBytesUsedID       MetricID = "memory/bytes_used"
	MemoryAnonymousUsedID   MetricID = "memory/anonymous_used"
	MemoryPageCacheUsedID   MetricID = "memory/page_cache_used"
	MemoryUnevictableUsedID MetricID = "memory/unevictable_used"
	MemoryDirtyUsedID       MetricID = "memory/dirty_used"
	MemoryPercentUsedID     MetricID = "memory/percent_used"
	OSFeatureID             MetricID = "system/os_feature"
	SystemProcessesTotal    MetricID = "system/processes_total"
	SystemProcsRunning      MetricID = "system/procs_running"
	SystemProcsBlocked      MetricID = "system/procs_blocked"
	SystemInterruptsTotal   MetricID = "system/interrupts_total"
	SystemCPUStat           MetricID = "system/cpu_stat"
	NetDevRxBytes           MetricID = "net/rx_bytes"
	NetDevRxPackets         MetricID = "net/rx_packets"
	NetDevRxErrors          MetricID = "net/rx_errors"
	NetDevRxDropped         MetricID = "net/rx_dropped"
	NetDevRxFifo            MetricID = "net/rx_fifo"
	NetDevRxFrame           MetricID = "net/rx_frame"
	NetDevRxCompressed      MetricID = "net/rx_compressed"
	NetDevRxMulticast       MetricID = "net/rx_multicast"
	NetDevTxBytes           MetricID = "net/tx_bytes"
	NetDevTxPackets         MetricID = "net/tx_packets"
	NetDevTxErrors          MetricID = "net/tx_errors"
	NetDevTxDropped         MetricID = "net/tx_dropped"
	NetDevTxFifo            MetricID = "net/tx_fifo"
	NetDevTxCollisions      MetricID = "net/tx_collisions"
	NetDevTxCarrier         MetricID = "net/tx_carrier"
	NetDevTxCompressed      MetricID = "net/tx_compressed"
)

var MetricMap MetricMapping

func init() {
	MetricMap.mapMutex.Lock()
	defer MetricMap.mapMutex.Unlock()

	MetricMap.viewNameToMetricIDMap = make(map[string]MetricID)
}

type MetricID string

type MetricMapping struct {
	viewNameToMetricIDMap map[string]MetricID
	mapMutex              sync.RWMutex
}

func (mm *MetricMapping) AddMapping(metricID MetricID, viewName string) {
	mm.mapMutex.Lock()
	defer mm.mapMutex.Unlock()

	mm.viewNameToMetricIDMap[viewName] = metricID
}

func (mm *MetricMapping) ViewNameToMetricID(viewName string) (MetricID, bool) {
	mm.mapMutex.RLock()
	defer mm.mapMutex.RUnlock()

	id, ok := mm.viewNameToMetricIDMap[viewName]
	return id, ok
}

// Aggregation types for compatibility
type Aggregation int

const (
	LastValue Aggregation = iota
	Sum
)

// MetricInterface provides a common interface for all metric types
type MetricInterface interface {
	Record(labelValues map[string]string, value interface{}) error
}

// Int64MetricInterface is the interface for int64 metrics
type Int64MetricInterface interface {
	Record(labelValues map[string]string, value int64) error
}

// Float64MetricInterface is the interface for float64 metrics  
type Float64MetricInterface interface {
	Record(labelValues map[string]string, value float64) error
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
		if m.counter != nil {
			m.counter.Add(ctx, value, metric.WithAttributes(attrs...))
		}
	case LastValue:
		if m.gauge != nil {
			m.gauge.Add(ctx, value, metric.WithAttributes(attrs...))
		}
	default:
		klog.Warningf("Unsupported aggregation type: %v", m.aggregation)
	}
	
	return nil
}

// OTelFloat64Metric wraps OpenTelemetry float64 instruments
type OTelFloat64Metric struct {
	name        string
	description string
	unit        string
	aggregation Aggregation
	labels      []string
	counter     metric.Float64Counter
	gauge       metric.Float64UpDownCounter
	meter       metric.Meter
}

// Record implements Float64MetricInterface
func (m *OTelFloat64Metric) Record(labelValues map[string]string, value float64) error {
	ctx := context.Background()
	
	// Convert to OTel attributes
	attrs := make([]attribute.KeyValue, 0, len(labelValues))
	for k, v := range labelValues {
		attrs = append(attrs, attribute.String(k, v))
	}
	
	switch m.aggregation {
	case Sum:
		if m.counter != nil {
			m.counter.Add(ctx, value, metric.WithAttributes(attrs...))
		}
	case LastValue:
		if m.gauge != nil {
			m.gauge.Add(ctx, value, metric.WithAttributes(attrs...))
		}
	default:
		klog.Warningf("Unsupported aggregation type: %v", m.aggregation)
	}
	
	return nil
}

// Type aliases for backward compatibility
type Int64Metric = OTelInt64Metric
type Float64Metric = OTelFloat64Metric

// NewInt64Metric creates a new Int64 metric using OpenTelemetry
func NewInt64Metric(metricID MetricID, name, description, unit string, aggregation Aggregation, labels []string) (*Int64Metric, error) {
	meter := otel.Meter("node-problem-detector")
	
	otelMetric := &OTelInt64Metric{
		name:        name,
		description: description,
		unit:        unit,
		aggregation: aggregation,
		labels:      labels,
		meter:       meter,
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

// NewFloat64Metric creates a new Float64 metric using OpenTelemetry
func NewFloat64Metric(metricID MetricID, name, description, unit string, aggregation Aggregation, labels []string) (*Float64Metric, error) {
	meter := otel.Meter("node-problem-detector")
	
	otelMetric := &OTelFloat64Metric{
		name:        name,
		description: description,
		unit:        unit,
		aggregation: aggregation,
		labels:      labels,
		meter:       meter,
	}
	
	var err error
	switch aggregation {
	case Sum:
		otelMetric.counter, err = meter.Float64Counter(
			name,
			metric.WithDescription(description),
			metric.WithUnit(unit),
		)
	case LastValue:
		otelMetric.gauge, err = meter.Float64UpDownCounter(
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

// Float64MetricRepresentation represents a parsed Prometheus metric
type Float64MetricRepresentation struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// ParsePrometheusMetrics parses Prometheus text format metrics
func ParsePrometheusMetrics(metricsText string) ([]Float64MetricRepresentation, error) {
	var metrics []Float64MetricRepresentation
	lines := strings.Split(metricsText, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Parse metric line format: metric_name{label1="value1",label2="value2"} value
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		
		metricPart := parts[0]
		valuePart := parts[1]
		
		// Parse value
		value, err := strconv.ParseFloat(valuePart, 64)
		if err != nil {
			continue // Skip invalid values
		}
		
		// Parse metric name and labels
		name, labels := parseMetricNameAndLabels(metricPart)
		
		metrics = append(metrics, Float64MetricRepresentation{
			Name:   name,
			Labels: labels,
			Value:  value,
		})
	}
	
	return metrics, nil
}

// parseMetricNameAndLabels parses metric name and labels from metric string
func parseMetricNameAndLabels(metricStr string) (string, map[string]string) {
	labels := make(map[string]string)
	
	// Check if metric has labels
	braceIndex := strings.Index(metricStr, "{")
	if braceIndex == -1 {
		// No labels
		return metricStr, labels
	}
	
	name := metricStr[:braceIndex]
	labelsPart := metricStr[braceIndex+1:]
	
	// Remove closing brace
	if strings.HasSuffix(labelsPart, "}") {
		labelsPart = labelsPart[:len(labelsPart)-1]
	}
	
	// Parse labels
	if labelsPart != "" {
		labelPairs := strings.Split(labelsPart, ",")
		for _, pair := range labelPairs {
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				
				// Remove quotes from value
				if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
					value = value[1 : len(value)-1]
				}
				
				labels[key] = value
			}
		}
	}
	
	return name, labels
}

// GetFloat64Metric retrieves a specific metric from the list by name and labels
func GetFloat64Metric(metrics []Float64MetricRepresentation, name string, labels map[string]string, exactMatch bool) (Float64MetricRepresentation, error) {
	for _, metric := range metrics {
		if metric.Name == name {
			if exactMatch {
				// Check that all provided labels match exactly
				if labelsMatch(metric.Labels, labels) {
					return metric, nil
				}
			} else {
				// Check that all provided labels are present (subset match)
				if labelsSubset(metric.Labels, labels) {
					return metric, nil
				}
			}
		}
	}
	
	return Float64MetricRepresentation{}, fmt.Errorf("metric %s with labels %v not found", name, labels)
}

// labelsMatch checks if two label maps are exactly equal
func labelsMatch(metricLabels, searchLabels map[string]string) bool {
	if len(metricLabels) != len(searchLabels) {
		return false
	}
	
	for key, value := range searchLabels {
		if metricLabels[key] != value {
			return false
		}
	}
	
	return true
}

// labelsSubset checks if all searchLabels are present in metricLabels
func labelsSubset(metricLabels, searchLabels map[string]string) bool {
	for key, value := range searchLabels {
		if metricLabels[key] != value {
			return false
		}
	}
	
	return true
}