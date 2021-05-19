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

package provisioners

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/keikoproj/instance-manager/api/v1alpha1"
	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

var (
	EKSConfigurationPath  = "spec.eks.configuration"
	EKSTagsPath           = fmt.Sprintf("%v.tags", EKSConfigurationPath)
	EKSVolumesPath        = fmt.Sprintf("%v.volumes", EKSConfigurationPath)
	EKSLifecycleHooksPath = fmt.Sprintf("%v.lifecycleHooks", EKSConfigurationPath)
	EKSUserDataPath       = fmt.Sprintf("%v.userData", EKSConfigurationPath)

	// MergeSchema defines the key to merge by
	MergeSchema = map[string]string{
		EKSTagsPath:           "key",
		EKSVolumesPath:        "name",
		EKSLifecycleHooksPath: "name",
		EKSUserDataPath:       "name",
	}
)

type ProvisionerConfiguration struct {
	Boundaries    ResourceFieldBoundary
	Defaults      map[string]interface{}
	Conditionals  []Conditional
	InstanceGroup *v1alpha1.InstanceGroup
}

type SelectableAnnotations struct {
	Annotations map[string]string
}

func (a *SelectableAnnotations) Has(label string) (exists bool) {
	if _, ok := a.Annotations[label]; ok {
		return true
	}
	return false
}

// Get returns the value for the provided label.
func (a *SelectableAnnotations) Get(label string) (value string) {
	return a.Annotations[label]
}

func NewProvisionerConfiguration(config *corev1.ConfigMap, instanceGroup *v1alpha1.InstanceGroup) (*ProvisionerConfiguration, error) {
	var c = &ProvisionerConfiguration{}
	c.InstanceGroup = &v1alpha1.InstanceGroup{}
	if err := c.Unmarshal(config); err != nil {
		return c, errors.Wrap(err, "failed to unmarshal configuration")
	}
	instanceGroup.DeepCopyInto(c.InstanceGroup)
	return c, nil
}

type SharedBoundaries struct {
	MergeOverride []string `yaml:"mergeOverride,omitempty"`
	Merge         []string `yaml:"merge,omitempty"`
	Replace       []string `yaml:"replace,omitempty"`
}

type ResourceFieldBoundary struct {
	Restricted []string         `yaml:"restricted,omitempty"`
	Shared     SharedBoundaries `yaml:"shared,omitempty"`
}

type Conditional struct {
	AnnotationSelector string                 `yaml:"annotationSelector,omitempty"`
	Defaults           map[string]interface{} `yaml:"defaults,omitempty"`
}

func (c *ProvisionerConfiguration) Unmarshal(cm *corev1.ConfigMap) error {
	var (
		boundariesPath   = common.FieldPath("data.boundaries")
		defaultsPath     = common.FieldPath("data.defaults")
		conditionalsPath = common.FieldPath("data.conditionals")
	)

	config, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	if err != nil {
		return errors.Wrap(err, "failed to convert configmap to unstructured")
	}

	if boundaries, ok, _ := unstructured.NestedString(config, boundariesPath...); ok {
		boundaryConfig := &ResourceFieldBoundary{}
		err := yaml.Unmarshal([]byte(boundaries), boundaryConfig)
		if err != nil {
			return errors.Wrap(err, "failed to unmarshal boundaries")
		}
		c.Boundaries = *boundaryConfig
	}

	if defaults, ok, _ := unstructured.NestedString(config, defaultsPath...); ok {
		defaultConfig := &map[string]interface{}{}
		err := yaml.Unmarshal([]byte(defaults), defaultConfig)
		if err != nil {
			return errors.Wrap(err, "failed to unmarshal defaults")
		}

		c.Defaults, err = runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfig)
		if err != nil {
			return errors.Wrap(err, "failed to convert defaults to unstructured")
		}
	}

	if conditionals, ok, _ := unstructured.NestedString(config, conditionalsPath...); ok {
		var conditionalConfig = make([]Conditional, 0)
		err := yaml.Unmarshal([]byte(conditionals), &conditionalConfig)
		if err != nil {
			return errors.Wrap(err, "failed to convert conditionals to unstructured")
		}
		c.Conditionals = conditionalConfig
	}
	return nil
}

func (c *ProvisionerConfiguration) SetDefaults() error {
	unstructuredInstanceGroup, err := runtime.DefaultUnstructuredConverter.ToUnstructured(c.InstanceGroup)
	if err != nil {
		return errors.Wrap(err, "failed to convert instance group to unstructured")
	}

	if err := c.setSharedFields(unstructuredInstanceGroup); err != nil {
		return errors.Wrap(err, "failed to set shared fields")
	}

	if err := c.setRestrictedFields(unstructuredInstanceGroup); err != nil {
		return errors.Wrap(err, "failed to set restricted fields")
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredInstanceGroup, c.InstanceGroup)
	if err != nil {
		return errors.Wrap(err, "failed to convert instance group from unstructured")
	}

	return nil
}

func (c *ProvisionerConfiguration) setRestrictedFields(unstructuredInstanceGroup map[string]interface{}) error {
	// apply restricted paths to instance group
	var applicableConditionals, err = getMatchingConditionals(c.InstanceGroup, c.Conditionals)
	if err != nil {
		return err
	}
	for _, pathStr := range c.Boundaries.Restricted {
		path := common.FieldPath(pathStr)
		// if a default value exists for the path, set it on the instance group
		var setFieldInAnyConditional = false
		for _, conditional := range applicableConditionals {
			if field, setField, _ := unstructured.NestedFieldCopy(conditional.Defaults, path...); setField {
				setFieldInAnyConditional = true
				err := unstructured.SetNestedField(unstructuredInstanceGroup, field, path...)
				if err != nil {
					errors.Wrap(err, "failed to set nested field")
				}
			}
		}
		if setFieldInAnyConditional {
			continue
		}
		if field, ok, _ := unstructured.NestedFieldCopy(c.Defaults, path...); ok {
			// default value exists for restricted path
			err := unstructured.SetNestedField(unstructuredInstanceGroup, field, path...)
			if err != nil {
				errors.Wrap(err, "failed to set nested field")
			}
		}
	}
	return nil
}

func isConflict(defaultVal, resourceVal interface{}) bool {
	if resourceVal != nil && defaultVal != nil {
		return true
	}
	return false
}

func getMatchingConditionals(ig *v1alpha1.InstanceGroup, conditionals []Conditional) ([]Conditional, error) {
	var applicableConditionals = make([]Conditional, 0)
	var annotationLabels = &SelectableAnnotations{Annotations: ig.Annotations}
	for _, conditional := range conditionals {
		selector, err := labels.Parse(conditional.AnnotationSelector)
		if err != nil {
			return nil, err
		}
		if selector.Matches(annotationLabels) {
			applicableConditionals = append(applicableConditionals, conditional)
		}
	}
	return applicableConditionals, nil
}

func (c *ProvisionerConfiguration) setSharedFields(obj map[string]interface{}) error {
	var applicableConditionals, err = getMatchingConditionals(c.InstanceGroup, c.Conditionals)
	if err != nil {
		return err
	}
	for _, pathStr := range c.Boundaries.Shared.Replace {
		var (
			defaultVal  = common.FieldValue(pathStr, c.Defaults)
			resourceVal = common.FieldValue(pathStr, obj)
		)

		for _, conditional := range applicableConditionals {
			conditionalValue := common.FieldValue(pathStr, conditional.Defaults)
			if conditionalValue != nil {
				defaultVal = conditionalValue
			}
		}
		if defaultVal == nil {
			continue
		}

		if isConflict(defaultVal, resourceVal) {
			if err := common.SetFieldValue(pathStr, obj, resourceVal); err != nil {
				return errors.Wrap(err, "failed to replace field")
			}
			continue
		}
		if err := common.SetFieldValue(pathStr, obj, defaultVal); err != nil {
			return errors.Wrap(err, "failed to replace field")
		}
	}

	for _, pathStr := range c.Boundaries.Shared.Merge {
		var (
			defaultVal  = common.FieldValue(pathStr, c.Defaults)
			resourceVal = common.FieldValue(pathStr, obj)
		)

		for _, conditional := range applicableConditionals {
			conditionalValue := common.FieldValue(pathStr, conditional.Defaults)
			if conditionalValue != nil {
				if isConflict(conditionalValue, defaultVal) {
					merge := Merge(conditionalValue, defaultVal, pathStr, false)
					if err := common.SetFieldValue(pathStr, obj, merge); err != nil {
						return errors.Wrap(err, "failed to merge field")
					}
					defaultVal = merge
					continue
				} else {
					defaultVal = conditionalValue
				}
			}
		}

		if defaultVal == nil {
			continue
		}

		if isConflict(defaultVal, resourceVal) {
			merge := Merge(defaultVal, resourceVal, pathStr, false)
			if err := common.SetFieldValue(pathStr, obj, merge); err != nil {
				return errors.Wrap(err, "failed to merge field")
			}
			continue
		}
		if err := common.SetFieldValue(pathStr, obj, defaultVal); err != nil {
			return errors.Wrap(err, "failed to merge field")
		}
	}

	for _, pathStr := range c.Boundaries.Shared.MergeOverride {
		var (
			defaultVal  = common.FieldValue(pathStr, c.Defaults)
			resourceVal = common.FieldValue(pathStr, obj)
		)

		for _, conditional := range applicableConditionals {
			conditionalValue := common.FieldValue(pathStr, conditional.Defaults)
			if conditionalValue != nil {
				if isConflict(conditionalValue, defaultVal) {
					//Merge conditional into default, with conditional overriding any conflicting values.
					merge := Merge(conditionalValue, defaultVal, pathStr, false)
					if err := common.SetFieldValue(pathStr, obj, merge); err != nil {
						return errors.Wrap(err, "failed to merge field")
					}
					defaultVal = merge
					continue
				} else {
					defaultVal = conditionalValue
				}
			}
		}

		if defaultVal == nil {
			continue
		}

		if isConflict(defaultVal, resourceVal) {
			merge := Merge(defaultVal, resourceVal, pathStr, true)
			if err := common.SetFieldValue(pathStr, obj, merge); err != nil {
				return errors.Wrap(err, "failed to merge field")
			}
			continue
		}
		if err := common.SetFieldValue(pathStr, obj, defaultVal); err != nil {
			return errors.Wrap(err, "failed to merge field")
		}
	}

	return nil
}

func Merge(x, y interface{}, path string, override bool) interface{} {
	switch xValue := x.(type) {
	case []interface{}:
		var (
			idx       string
			pathMatch bool
		)

		yValue := y.([]interface{})
		for k, v := range MergeSchema {
			if strings.HasSuffix(path, k) {
				idx = v
				pathMatch = true
			}
		}

		if !pathMatch {
			return common.MergeSliceByUnique(xValue, yValue)
		}

		return common.MergeSliceByIndex(xValue, yValue, idx, override)
	case map[string]interface{}:
		yValue := y.(map[string]interface{})
		for key, val := range xValue {
			if v, ok := yValue[key]; ok {
				if override {
					yValue[key] = v
					continue
				}
			}
			yValue[key] = val
		}
		return yValue
	default:
		return y
	}
}
