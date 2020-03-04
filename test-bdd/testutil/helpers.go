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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type TemplateArguments struct {
	ClusterName        string
	KeyPairName        string
	VpcID              string
	AmiID              string
	NodeSecurityGroups []string
	Subnets            []string
}

func NewTemplateArguments() *TemplateArguments {
	return &TemplateArguments{
		ClusterName:        os.Getenv("EKS_CLUSTER"),
		KeyPairName:        os.Getenv("KEYPAIR_NAME"),
		VpcID:              os.Getenv("VPC_ID"),
		AmiID:              os.Getenv("AMI_ID"),
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

func ParseInstanceGroupYaml(relativePath string, args *TemplateArguments) (*unstructured.Unstructured, error) {
	var renderBuffer bytes.Buffer
	var err error

	var ig *unstructured.Unstructured

	if _, err = PathToOSFile(relativePath); err != nil {
		return nil, err
	}

	fileData, err := common.ReadFile(relativePath)
	if err != nil {
		return nil, err
	}

	rawTemplate := string(fileData)

	template, err := template.New("InstanceGroup").Parse(rawTemplate)
	if err != nil {
		return nil, err
	}

	err = template.Execute(&renderBuffer, &args)
	if err != nil {
		return nil, err
	}

	decoder := yaml.NewYAMLOrJSONDecoder(&renderBuffer, 100)
	for {
		var out unstructured.Unstructured
		err = decoder.Decode(&out)
		if err != nil {
			break
		}

		if out.GetKind() == "InstanceGroup" {
			var marshaled []byte
			marshaled, err = out.MarshalJSON()
			json.Unmarshal(marshaled, &ig)
			break
		}
	}

	if err != io.EOF && err != nil {
		return nil, err
	}
	return ig, nil
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
