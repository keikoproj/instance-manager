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

package ekscloudformation

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/keikoproj/instance-manager/controllers/common"
	"github.com/stretchr/testify/assert"
)

func TestCustomResourceSubmitter(t *testing.T) {
	var (
		ctx                = NewBasicContext(t)
		customResourceSpec = getRollupSpec("my-rolling-upgrade", "instance-manager")
		customResource, _  = common.ParseCustomResourceYaml(customResourceSpec)
	)

	// CR does not exist
	obj, err := ctx.submitCustomResource(RollupSchema, customResource)
	assert.Nil(t, err)
	assert.Equal(t, obj, customResource)

	// CR exist
	obj, err = ctx.submitCustomResource(RollupSchema, customResource)
	assert.Nil(t, err)
	assert.Equal(t, obj, customResource)
}

func TestCustomResourceWaiter(t *testing.T) {
	var (
		ctx                = NewBasicContext(t)
		customResourceSpec = getRollupSpec("my-rolling-upgrade", "instance-manager")
		customResource, _  = common.ParseCustomResourceYaml(customResourceSpec)
	)

	// CR does not exist - timeout
	result := ctx.waitForResourceExist(RollupSchema, customResource.GetNamespace(), customResource.GetName(), 0)
	assert.False(t, result)

	// CR exist
	CreateCR(customResource, RollupSchema, ctx.KubernetesClient.KubeDynamic)
	result = ctx.waitForResourceExist(RollupSchema, customResource.GetNamespace(), customResource.GetName(), 0)
	assert.True(t, result)

}

func TestCustomResourceFieldPoller(t *testing.T) {
	var (
		ctx                = NewBasicContext(t)
		jsonPath           = ".status.currentStatus"
		successString      = "success"
		failureString      = "error"
		customResourceSpec = getRollupSpec("my-rolling-upgrade", "instance-manager")
		customResource, _  = common.ParseCustomResourceYaml(customResourceSpec)
	)

	// CR still running
	unstructured.SetNestedField(customResource.Object, "running", "status", "currentStatus")
	CreateCR(customResource, RollupSchema, ctx.KubernetesClient.KubeDynamic)
	status, err := ctx.checkCustomResourceField(jsonPath, successString, failureString, RollupSchema, customResource)
	assert.Nil(t, err)
	assert.False(t, status)

	// CR failed
	unstructured.SetNestedField(customResource.Object, "error", "status", "currentStatus")
	UpdateCR(customResource, RollupSchema, ctx.KubernetesClient.KubeDynamic)
	status, err = ctx.checkCustomResourceField(jsonPath, successString, failureString, RollupSchema, customResource)
	assert.NotNil(t, err)
	assert.False(t, status)

	// CR status not updated
	unstructured.SetNestedField(customResource.Object, "", "status", "currentStatus")
	UpdateCR(customResource, RollupSchema, ctx.KubernetesClient.KubeDynamic)
	status, err = ctx.checkCustomResourceField(jsonPath, successString, failureString, RollupSchema, customResource)
	assert.Nil(t, err)
	assert.False(t, status)

	// CR successful
	unstructured.SetNestedField(customResource.Object, "success", "status", "currentStatus")
	UpdateCR(customResource, RollupSchema, ctx.KubernetesClient.KubeDynamic)
	status, err = ctx.checkCustomResourceField(jsonPath, successString, failureString, RollupSchema, customResource)
	assert.Nil(t, err)
	assert.True(t, status)

	// CR wrong schema
	badSchema := RollupSchema
	badSchema.Resource = "wrongresource"
	status, err = ctx.checkCustomResourceField(jsonPath, successString, failureString, badSchema, customResource)
	assert.NotNil(t, err)
	assert.False(t, status)
}
