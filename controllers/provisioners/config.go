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
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtime "k8s.io/apimachinery/pkg/runtime"

	"github.com/keikoproj/instance-manager/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type DefaultConfiguration struct {
	Boundaries BoundaryConfiguration
	Defaults   map[string]interface{}
}

type BoundaryConfiguration struct {
	Restricted []string `yaml:"restricted,omitempty"`
	Shared     []string `yaml:"shared,omitempty"`
}

func SetConfigurationDefaults(instanceGroup *v1alpha1.InstanceGroup, config *DefaultConfiguration) (*v1alpha1.InstanceGroup, error) {
	var (
		modifiedInstanceGroup = &v1alpha1.InstanceGroup{}
	)

	unstructuredInstanceGroup, err := runtime.DefaultUnstructuredConverter.ToUnstructured(instanceGroup)
	if err != nil {
		return instanceGroup, errors.Wrap(err, "failed to convert instance group to unstructured")
	}

	log.Info("applying restricted defaults", "paths", config.Boundaries.Restricted)

	// apply restricted paths to instance group
	for _, pathStr := range config.Boundaries.Restricted {
		path := strings.Split(pathStr, ".")
		// if a default value exists for the path, set it on the instance group
		if field, ok, _ := unstructured.NestedFieldCopy(config.Defaults, path...); ok {
			// default value exists for restricted path
			log.Info("setting field", "field", field, "path", path)
			err := unstructured.SetNestedField(unstructuredInstanceGroup, field, path...)
			if err != nil {
				return instanceGroup, errors.Wrap(err, "failed to set field")
			}
		}
	}

	log.Info("applying shared defaults", "paths", config.Boundaries.Shared)

	// merge shared paths with instance group
	for _, pathStr := range config.Boundaries.Shared {
		path := strings.Split(pathStr, ".")
		// if a default value exists for the path, merge it with the instance group
		var (
			defaultField interface{}
			crField      interface{}
		)
		if field, ok, _ := unstructured.NestedFieldCopy(config.Defaults, path...); ok {
			defaultField = field
		}
		if field, ok, _ := unstructured.NestedFieldCopy(unstructuredInstanceGroup, path...); ok {
			crField = field
		}

		if defaultField == nil {
			continue
		}

		if crField == nil {
			log.Info("setting shared field", "field", defaultField, "path", path)
			if err := unstructured.SetNestedField(unstructuredInstanceGroup, defaultField, path...); err != nil {
				return instanceGroup, errors.Wrap(err, "failed to set field")
			}
			continue
		}

		log.Info("merging shared fields", "field", defaultField, "resource", crField, "path", path)
		switch v := defaultField.(type) {
		case []interface{}:
			ifaceSlc := crField.([]interface{})
			ifaceSlc = append(ifaceSlc, v...)
			if err := unstructured.SetNestedField(unstructuredInstanceGroup, ifaceSlc, path...); err != nil {
				return instanceGroup, errors.Wrap(err, "failed to set field")
			}
		case map[string]interface{}:
			crMapObj := crField.(map[string]interface{})
			for key, val := range v {
				crMapObj[key] = val
			}
			if err := unstructured.SetNestedField(unstructuredInstanceGroup, crMapObj, path...); err != nil {
				return instanceGroup, errors.Wrap(err, "failed to set field")
			}
		default:
			// any other type will prefer the CR value
			if err := unstructured.SetNestedField(unstructuredInstanceGroup, crField, path...); err != nil {
				return instanceGroup, errors.Wrap(err, "failed to set field")
			}
		}
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredInstanceGroup, modifiedInstanceGroup)
	if err != nil {
		return instanceGroup, errors.Wrap(err, "failed to convert instance group from unstructured")
	}

	return modifiedInstanceGroup, nil
}

func UnmarshalConfiguration(cm *corev1.ConfigMap) (*DefaultConfiguration, error) {
	var (
		config                  = &DefaultConfiguration{}
		configmapBoundariesPath = []string{"data", "boundaries"}
		configmapDefaultsPath   = []string{"data", "defaults"}
	)

	if cm == nil {
		return config, nil
	}

	unstructuredConfig, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	if err != nil {
		return config, errors.Wrap(err, "failed to convert configmap to unstructured")
	}

	if boundaries, ok, _ := unstructured.NestedString(unstructuredConfig, configmapBoundariesPath...); ok {
		boundaryConfig := &BoundaryConfiguration{}
		err := yaml.Unmarshal([]byte(boundaries), boundaryConfig)
		if err != nil {
			return config, errors.Wrap(err, "failed to unmarshal boundaries")
		}
		config.Boundaries = *boundaryConfig
	}

	if defaults, ok, _ := unstructured.NestedString(unstructuredConfig, configmapDefaultsPath...); ok {
		defaultConfig := &map[string]interface{}{}
		err := yaml.Unmarshal([]byte(defaults), defaultConfig)
		if err != nil {
			return config, errors.Wrap(err, "failed to unmarshal defaults")
		}

		config.Defaults, err = runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfig)
		if err != nil {
			return config, errors.Wrap(err, "failed to convert defaults to unstructured")
		}
	}

	return config, nil
}
