/*
Copyright 2025 The Kubernetes Authors All rights reserved.

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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// MetricDataPoint represents a generic metric data point
type MetricDataPoint struct {
	Name        string
	Description string
	Unit        string
	Attributes  attribute.Set
	Timestamp   interface{} // time.Time
	Value       interface{} // int64 or float64
	MetricType  MetricType
	IsMonotonic bool // only relevant for Sum metrics
	
	// For histograms
	Count        uint64
	Sum          interface{} // int64 or float64
	Bounds       []float64
	BucketCounts []uint64
}

// MetricType represents the type of metric
type MetricType int

const (
	MetricTypeGaugeInt64 MetricType = iota
	MetricTypeGaugeFloat64
	MetricTypeSumInt64
	MetricTypeSumFloat64
	MetricTypeHistogramInt64
	MetricTypeHistogramFloat64
)

// MetricExporter defines the interface for metric exporters
type MetricExporter interface {
	// ProcessDataPoint handles a single metric data point
	ProcessDataPoint(dp MetricDataPoint) error
	// Shutdown cleans up resources
	Shutdown(ctx context.Context) error
}

// BaseOTelExporter provides common OpenTelemetry functionality
type BaseOTelExporter struct {
	reader    *metric.ManualReader
	processor *MetricProcessor
	shutdown  func(context.Context) error
	exporter  MetricExporter
}

// NewBaseOTelExporter creates a new base OpenTelemetry exporter
func NewBaseOTelExporter(exporter MetricExporter) *BaseOTelExporter {
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(mp)
	
	return &BaseOTelExporter{
		reader:    reader,
		processor: NewMetricProcessor(),
		shutdown:  mp.Shutdown,
		exporter:  exporter,
	}
}

// CollectAndExport collects metrics and exports them via the configured exporter
func (b *BaseOTelExporter) CollectAndExport(ctx context.Context) error {
	var rm metricdata.ResourceMetrics
	if err := b.reader.Collect(ctx, &rm); err != nil {
		return err
	}
	
	return b.processor.ProcessMetrics(&rm, b.exporter.ProcessDataPoint)
}

// Shutdown shuts down the base exporter
func (b *BaseOTelExporter) Shutdown(ctx context.Context) error {
	if err := b.exporter.Shutdown(ctx); err != nil {
		return err
	}
	return b.shutdown(ctx)
}

// MetricProcessor provides a generic way to process OpenTelemetry metrics
type MetricProcessor struct{}

// NewMetricProcessor creates a new metric processor
func NewMetricProcessor() *MetricProcessor {
	return &MetricProcessor{}
}

// ProcessMetrics processes all metrics in ResourceMetrics and calls the handler for each data point
func (mp *MetricProcessor) ProcessMetrics(rm *metricdata.ResourceMetrics, handler func(MetricDataPoint) error) error {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if err := mp.processMetric(m, handler); err != nil {
				return err
			}
		}
	}
	return nil
}

// processMetric processes a single metric and calls the handler for each data point
func (mp *MetricProcessor) processMetric(m metricdata.Metrics, handler func(MetricDataPoint) error) error {
	switch data := m.Data.(type) {
	case metricdata.Gauge[int64]:
		for _, dp := range data.DataPoints {
			if err := handler(MetricDataPoint{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Attributes:  dp.Attributes,
				Timestamp:   dp.Time,
				Value:       dp.Value,
				MetricType:  MetricTypeGaugeInt64,
			}); err != nil {
				return err
			}
		}
	case metricdata.Gauge[float64]:
		for _, dp := range data.DataPoints {
			if err := handler(MetricDataPoint{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Attributes:  dp.Attributes,
				Timestamp:   dp.Time,
				Value:       dp.Value,
				MetricType:  MetricTypeGaugeFloat64,
			}); err != nil {
				return err
			}
		}
	case metricdata.Sum[int64]:
		for _, dp := range data.DataPoints {
			if err := handler(MetricDataPoint{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Attributes:  dp.Attributes,
				Timestamp:   dp.Time,
				Value:       dp.Value,
				MetricType:  MetricTypeSumInt64,
				IsMonotonic: data.IsMonotonic,
			}); err != nil {
				return err
			}
		}
	case metricdata.Sum[float64]:
		for _, dp := range data.DataPoints {
			if err := handler(MetricDataPoint{
				Name:        m.Name,
				Description: m.Description,
				Unit:        m.Unit,
				Attributes:  dp.Attributes,
				Timestamp:   dp.Time,
				Value:       dp.Value,
				MetricType:  MetricTypeSumFloat64,
				IsMonotonic: data.IsMonotonic,
			}); err != nil {
				return err
			}
		}
	case metricdata.Histogram[int64]:
		for _, dp := range data.DataPoints {
			if err := handler(MetricDataPoint{
				Name:         m.Name,
				Description:  m.Description,
				Unit:         m.Unit,
				Attributes:   dp.Attributes,
				Timestamp:    dp.Time,
				MetricType:   MetricTypeHistogramInt64,
				Count:        dp.Count,
				Sum:          dp.Sum,
				Bounds:       dp.Bounds,
				BucketCounts: dp.BucketCounts,
			}); err != nil {
				return err
			}
		}
	case metricdata.Histogram[float64]:
		for _, dp := range data.DataPoints {
			if err := handler(MetricDataPoint{
				Name:         m.Name,
				Description:  m.Description,
				Unit:         m.Unit,
				Attributes:   dp.Attributes,
				Timestamp:    dp.Time,
				MetricType:   MetricTypeHistogramFloat64,
				Count:        dp.Count,
				Sum:          dp.Sum,
				Bounds:       dp.Bounds,
				BucketCounts: dp.BucketCounts,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}