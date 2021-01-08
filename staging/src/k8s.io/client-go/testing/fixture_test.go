/*
Copyright 2015 The Kubernetes Authors.

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

package testing

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

func getArbitraryResource(s schema.GroupVersionResource, name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       s.Resource,
			"apiVersion": s.Version,
			"metadata": map[string]interface{}{
				"name":            name,
				"namespace":       namespace,
				"generateName":    "test_generateName",
				"uid":             "test_uid",
				"resourceVersion": "test_resourceVersion",
				"selfLink":        "test_selfLink",
			},
			"data": strconv.Itoa(rand.Int()),
		},
	}
}

func TestWatchCallNonNamespace(t *testing.T) {
	testResource := schema.GroupVersionResource{Group: "", Version: "test_version", Resource: "test_kind"}
	testObj := getArbitraryResource(testResource, "test_name", "test_namespace")
	accessor, err := meta.Accessor(testObj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ns := accessor.GetNamespace()
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	o := NewObjectTracker(scheme, codecs.UniversalDecoder())
	watch, err := o.Watch(testResource, ns)
	if err != nil {
		t.Fatalf("test resource watch failed in %s: %v ", ns, err)
	}
	go func() {
		err := o.Create(testResource, testObj, ns)
		if err != nil {
			t.Errorf("test resource creation failed: %v", err)
		}
	}()
	out := <-watch.ResultChan()
	assert.Equal(t, testObj, out.Object, "watched object mismatch")
}

func TestWatchCallAllNamespace(t *testing.T) {
	testResource := schema.GroupVersionResource{Group: "", Version: "test_version", Resource: "test_kind"}
	testObj := getArbitraryResource(testResource, "test_name", "test_namespace")
	accessor, err := meta.Accessor(testObj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ns := accessor.GetNamespace()
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	o := NewObjectTracker(scheme, codecs.UniversalDecoder())
	w, err := o.Watch(testResource, "test_namespace")
	if err != nil {
		t.Fatalf("test resource watch failed in test_namespace: %v", err)
	}
	wAll, err := o.Watch(testResource, "")
	if err != nil {
		t.Fatalf("test resource watch failed in all namespaces: %v", err)
	}
	go func() {
		err := o.Create(testResource, testObj, ns)
		assert.NoError(t, err, "test resource creation failed")
	}()
	out := <-w.ResultChan()
	outAll := <-wAll.ResultChan()
	assert.Equal(t, watch.Added, out.Type, "watch event mismatch")
	assert.Equal(t, watch.Added, outAll.Type, "watch event mismatch")
	assert.Equal(t, testObj, out.Object, "watched created object mismatch")
	assert.Equal(t, testObj, outAll.Object, "watched created object mismatch")
	go func() {
		err := o.Update(testResource, testObj, ns)
		assert.NoError(t, err, "test resource updating failed")
	}()
	out = <-w.ResultChan()
	outAll = <-wAll.ResultChan()
	assert.Equal(t, watch.Modified, out.Type, "watch event mismatch")
	assert.Equal(t, watch.Modified, outAll.Type, "watch event mismatch")
	assert.Equal(t, testObj, out.Object, "watched updated object mismatch")
	assert.Equal(t, testObj, outAll.Object, "watched updated object mismatch")
	go func() {
		err := o.Delete(testResource, "test_namespace", "test_name")
		assert.NoError(t, err, "test resource deletion failed")
	}()
	out = <-w.ResultChan()
	outAll = <-wAll.ResultChan()
	assert.Equal(t, watch.Deleted, out.Type, "watch event mismatch")
	assert.Equal(t, watch.Deleted, outAll.Type, "watch event mismatch")
	assert.Equal(t, testObj, out.Object, "watched deleted object mismatch")
	assert.Equal(t, testObj, outAll.Object, "watched deleted object mismatch")
}

func TestWatchCallMultipleInvocation(t *testing.T) {
	cases := []struct {
		name string
		op   watch.EventType
		ns   string
	}{
		{
			"foo",
			watch.Added,
			"test_namespace",
		},
		{
			"bar",
			watch.Added,
			"test_namespace",
		},
		{
			"baz",
			watch.Added,
			"",
		},
		{
			"bar",
			watch.Modified,
			"test_namespace",
		},
		{
			"baz",
			watch.Modified,
			"",
		},
		{
			"foo",
			watch.Deleted,
			"test_namespace",
		},
		{
			"bar",
			watch.Deleted,
			"test_namespace",
		},
		{
			"baz",
			watch.Deleted,
			"",
		},
	}

	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	testResource := schema.GroupVersionResource{Group: "", Version: "test_version", Resource: "test_kind"}

	o := NewObjectTracker(scheme, codecs.UniversalDecoder())
	watchNamespaces := []string{
		"",
		"",
		"test_namespace",
		"test_namespace",
	}
	var wg sync.WaitGroup
	wg.Add(len(watchNamespaces))
	for idx, watchNamespace := range watchNamespaces {
		i := idx
		watchNamespace := watchNamespace
		w, err := o.Watch(testResource, watchNamespace)
		if err != nil {
			t.Fatalf("test resource watch failed in %s: %v", watchNamespace, err)
		}
		go func() {
			assert.NoError(t, err, "watch invocation failed")
			for _, c := range cases {
				if watchNamespace == "" || c.ns == watchNamespace {
					fmt.Printf("%#v %#v\n", c, i)
					event := <-w.ResultChan()
					accessor, err := meta.Accessor(event.Object)
					if err != nil {
						t.Errorf("unexpected error: %v", err)
						break
					}
					assert.Equal(t, c.op, event.Type, "watch event mismatched")
					assert.Equal(t, c.name, accessor.GetName(), "watched object mismatch")
					assert.Equal(t, c.ns, accessor.GetNamespace(), "watched object mismatch")
				}
			}
			wg.Done()
		}()
	}
	for _, c := range cases {
		switch c.op {
		case watch.Added:
			obj := getArbitraryResource(testResource, c.name, c.ns)
			o.Create(testResource, obj, c.ns)
		case watch.Modified:
			obj := getArbitraryResource(testResource, c.name, c.ns)
			o.Update(testResource, obj, c.ns)
		case watch.Deleted:
			o.Delete(testResource, c.ns, c.name)
		}
	}
	wg.Wait()
}

func TestWatchAddAfterStop(t *testing.T) {
	testResource := schema.GroupVersionResource{Group: "", Version: "test_version", Resource: "test_kind"}
	testObj := getArbitraryResource(testResource, "test_name", "test_namespace")
	accessor, err := meta.Accessor(testObj)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ns := accessor.GetNamespace()
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	o := NewObjectTracker(scheme, codecs.UniversalDecoder())
	watch, err := o.Watch(testResource, ns)
	if err != nil {
		t.Errorf("watch creation failed: %v", err)
	}

	// When the watch is stopped it should ignore later events without panicking.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Watch panicked when it should have ignored create after stop: %v", r)
		}
	}()

	watch.Stop()
	err = o.Create(testResource, testObj, ns)
	if err != nil {
		t.Errorf("test resource creation failed: %v", err)
	}
}

func TestPatchWithMissingObject(t *testing.T) {
	nodesResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}

	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)
	o := NewObjectTracker(scheme, codecs.UniversalDecoder())
	reaction := ObjectReaction(o)
	action := NewRootPatchSubresourceAction(nodesResource, "node-1", types.StrategicMergePatchType, []byte(`{}`))
	handled, node, err := reaction(action)
	assert.True(t, handled)
	assert.Nil(t, node)
	assert.EqualError(t, err, `nodes "node-1" not found`)
}

func TestGetWithExactMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	codecs := serializer.NewCodecFactory(scheme)

	constructObject := func(s schema.GroupVersionResource, name, namespace string) (*unstructured.Unstructured, schema.GroupVersionResource) {
		obj := getArbitraryResource(s, name, namespace)
		gvks, _, err := scheme.ObjectKinds(obj)
		assert.NoError(t, err)
		gvr, _ := meta.UnsafeGuessKindToResource(gvks[0])
		return obj, gvr
	}

	var err error
	// Object with empty namespace
	o := NewObjectTracker(scheme, codecs.UniversalDecoder())
	nodeResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "node"}
	node, gvr := constructObject(nodeResource, "node", "")

	assert.Nil(t, o.Add(node))

	// Exact match
	_, err = o.Get(gvr, "", "node")
	assert.NoError(t, err)

	// Unexpected namespace provided
	_, err = o.Get(gvr, "ns", "node")
	assert.Error(t, err)
	errNotFound := errors.NewNotFound(gvr.GroupResource(), "node")
	assert.EqualError(t, err, errNotFound.Error())

	// Object with non-empty namespace
	o = NewObjectTracker(scheme, codecs.UniversalDecoder())
	podResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pod"}
	pod, gvr := constructObject(podResource, "pod", "default")
	assert.Nil(t, o.Add(pod))

	// Exact match
	_, err = o.Get(gvr, "default", "pod")
	assert.NoError(t, err)

	// Missing namespace
	_, err = o.Get(gvr, "", "pod")
	assert.Error(t, err)
	errNotFound = errors.NewNotFound(gvr.GroupResource(), "pod")
	assert.EqualError(t, err, errNotFound.Error())
}

func TestListPodAfterCreateSubresource(t *testing.T) {
	podsResource := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	podsKind := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"},
	}

	cases := []struct {
		name string
		subresourceAction CreateActionImpl
		parentResourceAction CreateActionImpl
	}{
		{
			name: "eviction",
			parentResourceAction: NewCreateAction(podsResource, "default", pod),
			subresourceAction: NewCreateSubresourceAction(podsResource, "foo", "eviction", "default", &policyv1beta1.Eviction{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}),
		},
		{
			name: "binding",
			parentResourceAction: NewCreateAction(podsResource, "default", pod),
			subresourceAction: NewCreateSubresourceAction(podsResource, "cool", "binding", "default", &corev1.Binding{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
			}),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			assert.NoError(t, corev1.AddToScheme(scheme))
			assert.NoError(t, policyv1beta1.AddToScheme(scheme))

			codecs := serializer.NewCodecFactory(scheme)
			o := NewObjectTracker(scheme, codecs.UniversalDecoder())
			reaction := ObjectReaction(o)

			handled, obj, err := reaction(c.parentResourceAction)
			assert.True(t, handled)
			assert.NotNil(t, obj)
			assert.NoError(t, err)

			handled, obj, err = reaction(c.subresourceAction)
			assert.True(t, handled)
			assert.NotNil(t, obj)
			assert.NoError(t, err)

			action := NewListAction(podsResource, podsKind, "default", metav1.ListOptions{})
			handled, list, err := reaction(action)
			assert.True(t, handled)

			pods := list.(*corev1.PodList)
			assert.Equal(t, *pod, pods.Items[0])
			assert.NoError(t, err)
		})
	}
}
