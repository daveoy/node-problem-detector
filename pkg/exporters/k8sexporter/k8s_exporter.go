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

package k8sexporter

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"
	"strconv"
	"time"

	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/clock"

	"k8s.io/node-problem-detector/cmd/options"
	"k8s.io/node-problem-detector/pkg/exporters/k8sexporter/condition"
	"k8s.io/node-problem-detector/pkg/exporters/k8sexporter/problemclient"
	"k8s.io/node-problem-detector/pkg/types"
	"k8s.io/node-problem-detector/pkg/util"
)

type k8sExporter struct {
	client           problemclient.Client
	conditionManager condition.ConditionManager
}

// NewExporterOrDie creates a exporter for Kubernetes apiserver exporting,
// panics if error occurs.
//
// Note that this function may be blocked (until a timeout occurs) before
// kube-apiserver becomes ready.
func NewExporterOrDie(ctx context.Context, npdo *options.NodeProblemDetectorOptions) types.Exporter {
	if !npdo.EnableK8sExporter {
		return nil
	}

	c := problemclient.NewClientOrDie(npdo)

	klog.Infof("Waiting for kube-apiserver to be ready (timeout %v)...", npdo.APIServerWaitTimeout)
	if err := waitForAPIServerReadyWithTimeout(ctx, c, npdo); err != nil {
		klog.Warningf("kube-apiserver did not become ready: timed out on waiting for kube-apiserver to return the node object: %v", err)
	}

	ke := k8sExporter{
		client:           c,
		conditionManager: condition.NewConditionManager(c, clock.RealClock{}, npdo.K8sExporterHeartbeatPeriod),
	}

	ke.startHTTPReporting(ctx, npdo)
	if npdo.DeleteDeprecatedConditions {
		err := ke.client.DeleteDeprecatedConditions(ctx, npdo.DeprecatedConditionTypes)
		if err != nil {
			klog.Errorf("Failed to delete deprecated conditions: %v", err)
		}
	}
	ke.conditionManager.Start(ctx)

	return &ke
}

func (ke *k8sExporter) ExportProblems(status *types.Status) {
	for _, event := range status.Events {
		ke.client.Eventf(util.ConvertToAPIEventType(event.Severity), status.Source, event.Reason, event.Message)
	}
	for _, cdt := range status.Conditions {
		ke.conditionManager.UpdateCondition(cdt)
	}
}

func (ke *k8sExporter) startHTTPReporting(ctx context.Context, npdo *options.NodeProblemDetectorOptions) {
	if npdo.ServerPort <= 0 {
		return
	}
	mux := http.NewServeMux()

	// Add healthz http request handler. Always return ok now, add more health check
	// logic in the future.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Add the handler to serve condition http request.
	mux.HandleFunc("/conditions", func(w http.ResponseWriter, r *http.Request) {
		util.ReturnHTTPJson(w, ke.conditionManager.GetConditions())
	})

	// register pprof
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	addr := net.JoinHostPort(npdo.ServerAddress, strconv.Itoa(npdo.ServerPort))
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Shut it down when ctx is cancelled
	go func() {
		<-ctx.Done()
		klog.Infof("Context cancelled; shutting down HTTP server on %s", addr)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			klog.Errorf("HTTP server shutdown error: %v", err)
		}
	}()
}

func waitForAPIServerReadyWithTimeout(ctx context.Context, c problemclient.Client, npdo *options.NodeProblemDetectorOptions) error {
	return wait.PollUntilContextTimeout(ctx, npdo.APIServerWaitInterval, npdo.APIServerWaitTimeout, true, func(ctx context.Context) (done bool, err error) {
		// If NPD can get the node object from kube-apiserver, the server is
		// ready and the RBAC permission is set correctly.
		if _, err := c.GetNode(ctx); err != nil {
			klog.Errorf("Can't get node object: %v", err)
			return false, err
		}
		return true, nil
	})
}
