/*

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
// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// InstanceGroupLister helps list InstanceGroups.
// All objects returned here must be treated as read-only.
type InstanceGroupLister interface {
	// List lists all InstanceGroups in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.InstanceGroup, err error)
	// InstanceGroups returns an object that can list and get InstanceGroups.
	InstanceGroups(namespace string) InstanceGroupNamespaceLister
	InstanceGroupListerExpansion
}

// instanceGroupLister implements the InstanceGroupLister interface.
type instanceGroupLister struct {
	indexer cache.Indexer
}

// NewInstanceGroupLister returns a new InstanceGroupLister.
func NewInstanceGroupLister(indexer cache.Indexer) InstanceGroupLister {
	return &instanceGroupLister{indexer: indexer}
}

// List lists all InstanceGroups in the indexer.
func (s *instanceGroupLister) List(selector labels.Selector) (ret []*v1alpha1.InstanceGroup, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.InstanceGroup))
	})
	return ret, err
}

// InstanceGroups returns an object that can list and get InstanceGroups.
func (s *instanceGroupLister) InstanceGroups(namespace string) InstanceGroupNamespaceLister {
	return instanceGroupNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// InstanceGroupNamespaceLister helps list and get InstanceGroups.
// All objects returned here must be treated as read-only.
type InstanceGroupNamespaceLister interface {
	// List lists all InstanceGroups in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.InstanceGroup, err error)
	// Get retrieves the InstanceGroup from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.InstanceGroup, error)
	InstanceGroupNamespaceListerExpansion
}

// instanceGroupNamespaceLister implements the InstanceGroupNamespaceLister
// interface.
type instanceGroupNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all InstanceGroups in the indexer for a given namespace.
func (s instanceGroupNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.InstanceGroup, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.InstanceGroup))
	})
	return ret, err
}

// Get retrieves the InstanceGroup from the indexer for a given namespace and name.
func (s instanceGroupNamespaceLister) Get(name string) (*v1alpha1.InstanceGroup, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("instancegroup"), name)
	}
	return obj.(*v1alpha1.InstanceGroup), nil
}