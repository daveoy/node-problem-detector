/*
Copyright 2016 The Kubernetes Authors All rights reserved.

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

package problemclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	testclock "k8s.io/utils/clock/testing"
)

const (
	testSource  = "test"
	testNode    = "test-node"
	testNodeUID = "11111111-1111-1111-1111-111111111111"
)

func newFakeProblemClient() *nodeProblemClient {
	return &nodeProblemClient{
		nodeName:  testNode,
		clock:     testclock.NewFakeClock(time.Now()),
		recorders: make(map[string]record.EventRecorder),
		nodeRef:   getNodeRef("", testNode),
	}
}

func TestGeneratePatch(t *testing.T) {
	now := time.Now()
	update := []v1.NodeCondition{
		{
			Type:               "TestType1",
			Status:             v1.ConditionTrue,
			LastTransitionTime: metav1.NewTime(now),
			Reason:             "TestReason1",
			Message:            "TestMessage1",
		},
		{
			Type:               "TestType2",
			Status:             v1.ConditionFalse,
			LastTransitionTime: metav1.NewTime(now),
			Reason:             "TestReason2",
			Message:            "TestMessage2",
		},
	}
	raw, err := json.Marshal(&update)
	assert.NoError(t, err)
	expectedPatch := []byte(fmt.Sprintf(`{"status":{"conditions":%s}}`, raw))

	patch, err := generatePatch(update)
	assert.NoError(t, err)
	if string(patch) != string(expectedPatch) {
		t.Errorf("expected patch %q, got %q", expectedPatch, patch)
	}
}

func TestEvent(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(1)
	client := newFakeProblemClient()
	client.recorders[testSource] = fakeRecorder
	client.Eventf(v1.EventTypeWarning, testSource, "test reason", "test message")
	expected := fmt.Sprintf("%s %s %s", v1.EventTypeWarning, "test reason", "test message")
	got := <-fakeRecorder.Events
	if expected != got {
		t.Errorf("expected event %q, got %q", expected, got)
	}
}

func TestNodeRefHasAPIVersionV1(t *testing.T) {
	client := newFakeProblemClient()

	if client.nodeRef.APIVersion != "v1" {
		t.Errorf("expected nodeRef.APIVersion to be 'v1', got %q", client.nodeRef.APIVersion)
	}
}

func TestNodeRefWithUID(t *testing.T) {
	client := newFakeProblemClient()

	if got := client.nodeRefWithUID().UID; got != "" {
		t.Errorf("expected no UID before the node is read, got %q", got)
	}

	client.cacheNodeRef(testNodeUID)

	if got := client.nodeRefWithUID().UID; got != testNodeUID {
		t.Errorf("expected UID %q, got %q", testNodeUID, got)
	}
	if got := client.nodeRef.UID; got != "" {
		t.Errorf("expected the shared nodeRef to keep no UID, got %q", got)
	}
}

func TestCacheNodeRefIgnoresEmptyUID(t *testing.T) {
	client := newFakeProblemClient()

	client.cacheNodeRef("")

	if got := client.nodeRefWithUID().UID; got != "" {
		t.Errorf("expected no UID, got %q", got)
	}
}

func TestCacheNodeRefKeepsFirstUID(t *testing.T) {
	client := newFakeProblemClient()

	client.cacheNodeRef(testNodeUID)
	client.cacheNodeRef("22222222-2222-2222-2222-222222222222")

	if got := client.nodeRefWithUID().UID; got != testNodeUID {
		t.Errorf("expected the first UID %q, got %q", testNodeUID, got)
	}
}

// capturingRecorder records the object that Eventf reports the event against.
type capturingRecorder struct {
	object runtime.Object
}

func (r *capturingRecorder) Event(object runtime.Object, eventType, reason, message string) {
	r.object = object
}

func (r *capturingRecorder) Eventf(object runtime.Object, eventType, reason, messageFmt string, args ...interface{}) {
	r.object = object
}

func (r *capturingRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventType, reason, messageFmt string, args ...interface{}) {
	r.object = object
}

func TestEventfReportsNodeUID(t *testing.T) {
	recorder := &capturingRecorder{}
	client := newFakeProblemClient()
	client.recorders[testSource] = recorder
	client.cacheNodeRef(testNodeUID)

	client.Eventf(v1.EventTypeWarning, testSource, "test reason", "test message")

	ref, ok := recorder.object.(*v1.ObjectReference)
	if !ok {
		t.Fatalf("expected an *v1.ObjectReference, got %T", recorder.object)
	}
	if ref.UID != testNodeUID {
		t.Errorf("expected the reported event to carry UID %q, got %q", testNodeUID, ref.UID)
	}
	if ref.Name != testNode {
		t.Errorf("expected the reported event to carry name %q, got %q", testNode, ref.Name)
	}
}

// newProblemClientAgainstAPI returns a client that talks to a server which
// always answers with the test node.
func newProblemClientAgainstAPI(t *testing.T) *nodeProblemClient {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: testNode, UID: testNodeUID}}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(node); err != nil {
			t.Errorf("failed to encode the node: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	coreClient, err := typedcorev1.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("failed to create the core client: %v", err)
	}

	client := newFakeProblemClient()
	client.client = coreClient
	return client
}

func TestGetNodeCachesNodeUID(t *testing.T) {
	client := newProblemClientAgainstAPI(t)

	if _, err := client.GetNode(context.Background()); err != nil {
		t.Fatalf("GetNode returned an error: %v", err)
	}

	if got := client.nodeRefWithUID().UID; got != testNodeUID {
		t.Errorf("expected GetNode to cache UID %q, got %q", testNodeUID, got)
	}
}

func TestSetConditionsCachesNodeUID(t *testing.T) {
	client := newProblemClientAgainstAPI(t)

	conditions := []v1.NodeCondition{{Type: "TestType", Status: v1.ConditionTrue}}
	if err := client.SetConditions(context.Background(), conditions); err != nil {
		t.Fatalf("SetConditions returned an error: %v", err)
	}

	if got := client.nodeRefWithUID().UID; got != testNodeUID {
		t.Errorf("expected SetConditions to cache UID %q, got %q", testNodeUID, got)
	}
}

// newProblemClientWithNodeConditions returns a client that talks to a server
// which serves the test node with the given conditions. It records every
// status update, and fails the first failUpdates of them.
func newProblemClientWithNodeConditions(t *testing.T, conditions []v1.NodeCondition, failUpdates int) (*nodeProblemClient, *[]*v1.Node) {
	t.Helper()

	var updates []*v1.Node
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: testNode, UID: testNodeUID},
			Status:     v1.NodeStatus{Conditions: conditions},
		}
		if r.Method == http.MethodPut {
			updated := &v1.Node{}
			if err := json.NewDecoder(r.Body).Decode(updated); err != nil {
				t.Errorf("failed to decode the node update: %v", err)
			}
			updates = append(updates, updated)
			if len(updates) <= failUpdates {
				http.Error(w, "injected failure", http.StatusInternalServerError)
				return
			}
			node = updated
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(node); err != nil {
			t.Errorf("failed to encode the node: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	// Send JSON so the server can decode the update.
	coreClient, err := typedcorev1.NewForConfig(&rest.Config{
		Host:          server.URL,
		ContentConfig: rest.ContentConfig{ContentType: "application/json"},
	})
	if err != nil {
		t.Fatalf("failed to create the core client: %v", err)
	}

	client := newFakeProblemClient()
	client.client = coreClient
	return client, &updates
}

func conditionTypes(conditions []v1.NodeCondition) []v1.NodeConditionType {
	types := []v1.NodeConditionType{}
	for _, condition := range conditions {
		types = append(types, condition.Type)
	}
	return types
}

func TestDeleteDeprecatedConditions(t *testing.T) {
	existing := []v1.NodeCondition{
		{Type: "Ready", Status: v1.ConditionTrue},
		{Type: "OldCondition", Status: v1.ConditionFalse},
		{Type: "KernelDeadlock", Status: v1.ConditionFalse},
		{Type: "OtherOldCondition", Status: v1.ConditionTrue},
	}

	testCases := []struct {
		name            string
		deprecated      []string
		failUpdates     int
		expectedUpdates int
		expectedTypes   []v1.NodeConditionType
	}{
		{
			name:            "deletes only the deprecated conditions",
			deprecated:      []string{"OldCondition", "OtherOldCondition"},
			expectedUpdates: 1,
			expectedTypes:   []v1.NodeConditionType{"Ready", "KernelDeadlock"},
		},
		{
			name:            "skips the update when no deprecated conditions are present",
			deprecated:      []string{"NotPresent"},
			expectedUpdates: 0,
		},
		{
			name:            "skips the update when no deprecated types are given",
			deprecated:      []string{},
			expectedUpdates: 0,
		},
		{
			name:            "retries a failed update",
			deprecated:      []string{"OldCondition"},
			failUpdates:     1,
			expectedUpdates: 2,
			expectedTypes:   []v1.NodeConditionType{"Ready", "KernelDeadlock", "OtherOldCondition"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, updates := newProblemClientWithNodeConditions(t, existing, tc.failUpdates)

			err := client.DeleteDeprecatedConditions(context.Background(), tc.deprecated)
			assert.NoError(t, err)
			assert.Len(t, *updates, tc.expectedUpdates)
			if tc.expectedUpdates > 0 {
				last := (*updates)[len(*updates)-1]
				assert.Equal(t, tc.expectedTypes, conditionTypes(last.Status.Conditions))
			}
		})
	}
}

func TestFakeDeleteDeprecatedConditions(t *testing.T) {
	fake := NewFakeProblemClient()
	conditions := []v1.NodeCondition{
		{Type: "Ready", Status: v1.ConditionTrue},
		{Type: "OldCondition", Status: v1.ConditionFalse},
	}
	assert.NoError(t, fake.SetConditions(context.Background(), conditions))

	assert.NoError(t, fake.DeleteDeprecatedConditions(context.Background(), []string{"OldCondition"}))
	assert.NoError(t, fake.AssertConditions([]v1.NodeCondition{conditions[0]}))
}
