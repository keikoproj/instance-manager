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

package testutil

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

type TemplateArguments struct {
	ClusterName        string
	KeyPairName        string
	AmiID              string
	NodeRole           string
	NodeSecurityGroups []string
	Subnets            []string
}

func NewTemplateArguments() *TemplateArguments {
	return &TemplateArguments{
		ClusterName:        os.Getenv("EKS_CLUSTER"),
		KeyPairName:        os.Getenv("KEYPAIR_NAME"),
		AmiID:              os.Getenv("AMI_ID"),
		NodeRole:           os.Getenv("NODE_ROLE"),
		NodeSecurityGroups: strings.Split(os.Getenv("SECURITY_GROUPS"), ","),
		Subnets:            strings.Split(os.Getenv("NODE_SUBNETS"), ","),
	}
}

func IsNodeReady(n corev1.Node) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == "Ready" {
			if condition.Status == "True" {
				return true
			}
		}
	}
	return false
}

func PathToOSFile(relativePath string) (*os.File, error) {
	path, err := filepath.Abs(relativePath)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed generate absolute file path of %s", relativePath))
	}

	manifest, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to open file %s", path))
	}

	return manifest, nil
}

func DeleteEmpty(s []string) []string {
	var r []string
	for _, str := range s {
		if str != "" {
			r = append(r, str)
		}
	}
	return r
}

// find the corresponding GVR (available in *meta.RESTMapping) for gvk
func FindGVR(gvk *schema.GroupVersionKind, cfg *rest.Config) (*meta.RESTMapping, error) {

	// DiscoveryClient queries API server about the resources
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	return mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
}

func GetResourceFromYaml(path string, config *rest.Config, args *TemplateArguments) (*meta.RESTMapping, *unstructured.Unstructured, error) {
	resource := &unstructured.Unstructured{}

	d, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, resource, err
	}

	template, err := template.New("InstanceGroup").Parse(string(d))
	if err != nil {
		return nil, resource, err
	}

	var renderBuffer bytes.Buffer
	err = template.Execute(&renderBuffer, &args)
	if err != nil {
		return nil, resource, err
	}
	dec := serializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	_, gvk, err := dec.Decode(renderBuffer.Bytes(), nil, resource)
	if err != nil {
		return nil, resource, err
	}

	gvr, err := FindGVR(gvk, config)
	if err != nil {
		return nil, resource, err
	}

	return gvr, resource, nil
}
