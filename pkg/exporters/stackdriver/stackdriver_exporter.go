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

package stackdriverexporter

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	gcpmetric "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric"
	"github.com/avast/retry-go/v4"
	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"k8s.io/klog/v2"

	"k8s.io/node-problem-detector/pkg/exporters"
	seconfig "k8s.io/node-problem-detector/pkg/exporters/stackdriver/config"
	"k8s.io/node-problem-detector/pkg/types"
)

func init() {
	clo := commandLineOptions{}
	exporters.Register(exporterName, types.ExporterHandler{
		CreateExporterOrDie: NewExporterOrDie,
		Options:             &clo,
	})
}

const exporterName = "stackdriver"

var NPDMetricToSDMetric = map[string]string{
	"cpu_runnable_task_count":     "compute.googleapis.com/guest/cpu/runnable_task_count",
	"cpu_usage_time":              "compute.googleapis.com/guest/cpu/usage_time",
	"cpu_load_1m":                 "compute.googleapis.com/guest/cpu/load_1m",
	"cpu_load_5m":                 "compute.googleapis.com/guest/cpu/load_5m",
	"cpu_load_15m":                "compute.googleapis.com/guest/cpu/load_15m",
	"disk_avg_queue_len":          "compute.googleapis.com/guest/disk/queue_length",
	"disk_bytes_used":             "compute.googleapis.com/guest/disk/bytes_used",
	"disk_percent_used":           "compute.googleapis.com/guest/disk/percent_used",
	"disk_io_time":                "compute.googleapis.com/guest/disk/io_time",
	"disk_merged_operation_count": "compute.googleapis.com/guest/disk/merged_operation_count",
	"disk_operation_bytes_count":  "compute.googleapis.com/guest/disk/operation_bytes_count",
	"disk_operation_count":        "compute.googleapis.com/guest/disk/operation_count",
	"disk_operation_time":         "compute.googleapis.com/guest/disk/operation_time",
	"disk_weighted_io":            "compute.googleapis.com/guest/disk/weighted_io_time",
	"host_uptime":                 "compute.googleapis.com/guest/system/uptime",
	"memory_anonymous_used":       "compute.googleapis.com/guest/memory/anonymous_used",
	"memory_bytes_used":           "compute.googleapis.com/guest/memory/bytes_used",
	"memory_dirty_used":           "compute.googleapis.com/guest/memory/dirty_used",
	"memory_page_cache_used":      "compute.googleapis.com/guest/memory/page_cache_used",
	"memory_unevictable_used":     "compute.googleapis.com/guest/memory/unevictable_used",
	"memory_percent_used":         "compute.googleapis.com/guest/memory/percent_used",
	"problem_counter":             "compute.googleapis.com/guest/system/problem_count",
	"problem_gauge":               "compute.googleapis.com/guest/system/problem_state",
	"system_os_feature":           "compute.googleapis.com/guest/system/os_feature_enabled",
	"system_processes_total":      "kubernetes.io/internal/node/guest/system/processes_total",
	"system_procs_running":        "kubernetes.io/internal/node/guest/system/procs_running",
	"system_procs_blocked":        "kubernetes.io/internal/node/guest/system/procs_blocked",
	"system_interrupts_total":     "kubernetes.io/internal/node/guest/system/interrupts_total",
	"system_cpu_stat":             "kubernetes.io/internal/node/guest/system/cpu_stat",
	"net_rx_bytes":                "kubernetes.io/internal/node/guest/net/rx_bytes",
	"net_rx_packets":              "kubernetes.io/internal/node/guest/net/rx_packets",
	"net_rx_errors":               "kubernetes.io/internal/node/guest/net/rx_errors",
	"net_rx_dropped":              "kubernetes.io/internal/node/guest/net/rx_dropped",
	"net_rx_fifo":                 "kubernetes.io/internal/node/guest/net/rx_fifo",
	"net_rx_frame":                "kubernetes.io/internal/node/guest/net/rx_frame",
	"net_rx_compressed":           "kubernetes.io/internal/node/guest/net/rx_compressed",
	"net_rx_multicast":            "kubernetes.io/internal/node/guest/net/rx_multicast",
	"net_tx_bytes":                "kubernetes.io/internal/node/guest/net/tx_bytes",
	"net_tx_packets":              "kubernetes.io/internal/node/guest/net/tx_packets",
	"net_tx_errors":               "kubernetes.io/internal/node/guest/net/tx_errors",
	"net_tx_dropped":              "kubernetes.io/internal/node/guest/net/tx_dropped",
	"net_tx_fifo":                 "kubernetes.io/internal/node/guest/net/tx_fifo",
	"net_tx_collisions":           "kubernetes.io/internal/node/guest/net/tx_collisions",
	"net_tx_carrier":              "kubernetes.io/internal/node/guest/net/tx_carrier",
	"net_tx_compressed":           "kubernetes.io/internal/node/guest/net/tx_compressed",
}

func getMetricTypeConversionFunction(customMetricPrefix string) func(string) string {
	return func(metricName string) string {
		fallbackMetricType := ""
		if customMetricPrefix != "" {
			// Example fallbackMetricType: custom.googleapis.com/npd/host/uptime
			fallbackMetricType = fmt.Sprintf("%s/%s", customMetricPrefix, metricName)
		}

		if stackdriverMetricType, ok := NPDMetricToSDMetric[metricName]; ok {
			return stackdriverMetricType
		}
		return fallbackMetricType
	}
}

type stackdriverExporter struct {
	config        seconfig.StackdriverExporterConfig
	meterProvider *metric.MeterProvider
}

// ExportProblems does nothing.
// Stackdriver exporter only exports metrics.
func (se *stackdriverExporter) ExportProblems(status *types.Status) {
	return
}

func (se *stackdriverExporter) setupOTelExporterOrDie() {
	// Create Google Cloud Monitoring exporter
	gcpExporter, err := gcpmetric.New(
		gcpmetric.WithProjectID(se.config.GCEMetadata.ProjectID),
		gcpmetric.WithMetricDescriptorTypeFormatter(se.getMetricTypeFormatter()),
	)
	if err != nil {
		klog.Fatalf("Failed to create Google Cloud Monitoring exporter: %v", err)
	}

	// Create periodic reader that exports metrics at regular intervals
	exportPeriod, err := time.ParseDuration(se.config.ExportPeriod)
	if err != nil {
		klog.Fatalf("Failed to parse ExportPeriod %q: %v", se.config.ExportPeriod, err)
	}

	reader := metric.NewPeriodicReader(
		gcpExporter,
		metric.WithInterval(exportPeriod),
	)

	// Create meter provider with GCP exporter
	se.meterProvider = metric.NewMeterProvider(
		metric.WithReader(reader),
	)

	// Set as global meter provider
	otel.SetMeterProvider(se.meterProvider)

	klog.Infof("Google Cloud Monitoring exporter configured for project %s", se.config.GCEMetadata.ProjectID)
}

// getMetricTypeFormatter returns a function to convert metrics to GCP metric types
func (se *stackdriverExporter) getMetricTypeFormatter() func(metricdata.Metrics) string {
	converter := getMetricTypeConversionFunction(se.config.CustomMetricPrefix)
	return func(m metricdata.Metrics) string {
		return converter(m.Name)
	}
}

func (se *stackdriverExporter) populateMetadataOrDie() {
	if !se.config.GCEMetadata.HasMissingField() {
		klog.Infof("Using GCE metadata specified in the config file: %+v", se.config.GCEMetadata)
		return
	}

	metadataFetchTimeout, err := time.ParseDuration(se.config.MetadataFetchTimeout)
	if err != nil {
		klog.Fatalf("Failed to parse MetadataFetchTimeout %q: %v", se.config.MetadataFetchTimeout, err)
	}

	metadataFetchInterval, err := time.ParseDuration(se.config.MetadataFetchInterval)
	if err != nil {
		klog.Fatalf("Failed to parse MetadataFetchInterval %q: %v", se.config.MetadataFetchInterval, err)
	}

	klog.Infof("Populating GCE metadata by querying GCE metadata server.")
	err = retry.Do(se.config.GCEMetadata.PopulateFromGCE,
		retry.Delay(metadataFetchInterval),
		retry.Attempts(uint(metadataFetchTimeout/metadataFetchInterval)),
		retry.DelayType(retry.FixedDelay))
	if err == nil {
		klog.Infof("Using GCE metadata: %+v", se.config.GCEMetadata)
		return
	}
	if se.config.PanicOnMetadataFetchFailure {
		klog.Fatalf("Failed to populate GCE metadata: %v", err)
	} else {
		klog.Errorf("Failed to populate GCE metadata: %v", err)
	}
}

// ExportProblems does nothing.
// Stackdriver exporter only exports metrics.
func (se *stackdriverExporter) ExportProblems(status *types.Status) {
}

type commandLineOptions struct {
	configPath string
}

func (clo *commandLineOptions) SetFlags(fs *pflag.FlagSet) {
	fs.StringVar(&clo.configPath, "exporter.stackdriver", "",
		"Configuration for Stackdriver exporter. Set to config file path.")
}

// NewExporterOrDie creates an exporter to export metrics to Stackdriver, panics if error occurs.
func NewExporterOrDie(clo types.CommandLineOptions) types.Exporter {
	options, ok := clo.(*commandLineOptions)
	if !ok {
		klog.Fatalf("Wrong type for the command line options of Stackdriver Exporter: %s.", reflect.TypeOf(clo))
	}
	if options.configPath == "" {
		return nil
	}

	se := stackdriverExporter{}

	// Apply configurations.
	f, err := os.ReadFile(options.configPath)
	if err != nil {
		klog.Fatalf("Failed to read configuration file %q: %v", options.configPath, err)
	}
	err = json.Unmarshal(f, &se.config)
	if err != nil {
		klog.Fatalf("Failed to unmarshal configuration file %q: %v", options.configPath, err)
	}
	se.config.ApplyConfiguration()

	klog.Infof("Starting Stackdriver exporter %s", options.configPath)

	se.populateMetadataOrDie()
	se.setupOTelExporterOrDie()

	return &se
}
