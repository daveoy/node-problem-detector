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

package otel

import (
	"os"
	"sync"
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
)

func TestGetResource(t *testing.T) {
	// Reset the global state for isolated testing
	globalResource = nil
	resourceOnce = sync.Once{}

	// Test with NODE_NAME environment variable
	os.Setenv("NODE_NAME", "test-node")
	defer os.Unsetenv("NODE_NAME")

	resource := GetResource()
	if resource == nil {
		t.Fatal("Expected resource to be created, got nil")
	}

	attrs := resource.Attributes()

	// Check service attributes
	serviceName := ""
	nodeName := ""
	serviceVersion := ""
	serviceInstanceID := ""

	for _, attr := range attrs {
		switch attr.Key {
		case semconv.ServiceNameKey:
			serviceName = attr.Value.AsString()
		case "node.name":
			nodeName = attr.Value.AsString()
		case semconv.ServiceVersionKey:
			serviceVersion = attr.Value.AsString()
		case semconv.ServiceInstanceIDKey:
			serviceInstanceID = attr.Value.AsString()
		}
	}

	if serviceName != "node-problem-detector" {
		t.Errorf("Expected service name 'node-problem-detector', got '%s'", serviceName)
	}

	if nodeName != "test-node" {
		t.Errorf("Expected node name 'test-node', got '%s'", nodeName)
	}

	if serviceVersion == "" {
		t.Error("Expected service version to be set")
	}

	// Verify service instance ID is a valid UUID format
	if len(serviceInstanceID) != 36 {
		t.Errorf("Expected service instance ID to be UUID format (36 chars), got '%s' (%d chars)", serviceInstanceID, len(serviceInstanceID))
	}
}

func TestGetResourceWithoutNodeName(t *testing.T) {
	// Reset the global state for isolated testing
	globalResource = nil
	resourceOnce = sync.Once{}

	// Ensure NODE_NAME is not set
	os.Unsetenv("NODE_NAME")

	resource := GetResource()
	if resource == nil {
		t.Fatal("Expected resource to be created, got nil")
	}

	attrs := resource.Attributes()

	// Check that some node name was set (hostname fallback)
	nodeName := ""
	for _, attr := range attrs {
		if string(attr.Key) == "node.name" {
			nodeName = attr.Value.AsString()
			break
		}
	}

	if nodeName == "" {
		t.Error("Expected node name to be set from hostname fallback")
	}
}

func TestGetResourceGeneratesUniqueInstanceIDs(t *testing.T) {
	// Reset the global state for isolated testing
	globalResource = nil
	resourceOnce = sync.Once{}

	// Generate multiple resources and verify they have unique instance IDs
	resource1 := GetResource()
	
	// Reset again to create a different resource
	globalResource = nil
	resourceOnce = sync.Once{}
	resource2 := GetResource()

	if resource1 == nil || resource2 == nil {
		t.Fatal("Expected resources to be created")
	}

	attrs1 := resource1.Attributes()
	attrs2 := resource2.Attributes()

	var instanceID1, instanceID2 string

	for _, attr := range attrs1 {
		if attr.Key == semconv.ServiceInstanceIDKey {
			instanceID1 = attr.Value.AsString()
			break
		}
	}

	for _, attr := range attrs2 {
		if attr.Key == semconv.ServiceInstanceIDKey {
			instanceID2 = attr.Value.AsString()
			break
		}
	}

	if instanceID1 == "" || instanceID2 == "" {
		t.Fatal("Expected both resources to have instance IDs")
	}

	if instanceID1 == instanceID2 {
		t.Errorf("Expected unique instance IDs, but got the same: %s", instanceID1)
	}
}
