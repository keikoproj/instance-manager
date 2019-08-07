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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var log = logrus.New()

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

func KubectlApply(manifestRelativePath string) error {
	kubectlBinaryPath, err := exec.LookPath("kubectl")
	if err != nil {
		panic(err)
	}

	path, err := filepath.Abs(manifestRelativePath)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed generate absolut file path of %s", manifestRelativePath))
	}

	applyArgs := []string{"apply", "-f", path}
	cmd := exec.Command(kubectlBinaryPath, applyArgs...)
	log.Printf("Executing: %v %v", kubectlBinaryPath, applyArgs)

	err = cmd.Start()
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Could not exec kubectl: "))
	}

	err = cmd.Wait()
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Command resulted in error: "))
	}

	return nil
}

func isNodeReady(n corev1.Node) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == "Ready" {
			if condition.Status == "True" {
				return true
			}
		}
	}
	return false
}

func WaitForNodesDelete(k kubernetes.Interface, role string) bool {
	// poll every 20 seconds
	var pollingInterval = time.Second * 20
	// timeout after 24 occurences = 480 seconds = 8 minutes
	var timeoutCounter = 24
	var pollingCounter = 0
	var labelSelector = fmt.Sprintf("node-role.kubernetes.io/%v=", role)

	for {
		log.Printf("waiting for nodes, attempt %v/%v", pollingCounter, timeoutCounter)
		nodeList, _ := k.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: labelSelector})
		nodeCount := len(nodeList.Items)

		if nodeCount == 0 {
			return true
		}

		time.Sleep(pollingInterval)
		log.Println("nodes did not terminate yet, retrying")
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}

func IsStackExist(w cloudformationiface.CloudFormationAPI, stackName string) bool {
	out, _ := w.DescribeStacks(&cloudformation.DescribeStacksInput{StackName: aws.String(stackName)})
	stackCount := len(out.Stacks)
	if stackCount != 0 {
		return true
	}
	return false
}

func GetStackState(w cloudformationiface.CloudFormationAPI, stackName string) string {
	out, err := w.DescribeStacks(&cloudformation.DescribeStacksInput{StackName: aws.String(stackName)})
	if err != nil {
		log.Println(err)
		return ""
	}
	for _, stack := range out.Stacks {
		scanStackName := aws.StringValue(stack.StackName)
		stackStatus := aws.StringValue(stack.StackStatus)
		log.Printf("Stack %v state is: %v", scanStackName, stackStatus)
		if scanStackName == stackName {
			return stackStatus
		}
	}
	return ""
}

func WaitForNodesCreate(k kubernetes.Interface, role string, expectedReadyCount int) bool {
	// poll every 40 seconds
	var pollingInterval = time.Second * 40
	// timeout after 24 occurences = 960 seconds = 16 minutes
	var timeoutCounter = 24
	var pollingCounter = 0
	var seenReady = 0
	var labelSelector = fmt.Sprintf("node-role.kubernetes.io/%v=", role)

	for {
		log.Printf("waiting for nodes, attempt %v/%v", pollingCounter, timeoutCounter)
		nodeList, _ := k.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: labelSelector})
		nodeCount := len(nodeList.Items)

		if nodeCount == expectedReadyCount {

			for _, node := range nodeList.Items {
				log.Printf("found %v", node.ObjectMeta.Name)

				if !isNodeReady(node) {
					log.Printf("%v is not ready", node.ObjectMeta.Name)
					break
				} else {
					seenReady++
				}
			}
			if seenReady == expectedReadyCount {
				return true
			}
		}

		log.Println("nodes did not join yet, retrying")
		time.Sleep(pollingInterval)
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}

func WaitForNodesRotate(k kubernetes.Interface, role string) bool {
	var initialNodeNames []string
	// poll every 40 seconds
	var pollingInterval = time.Second * 40
	// timeout after 24 occurences = 960 seconds = 16 minutes
	var timeoutCounter = 24
	var pollingCounter = 0
	var labelSelector = fmt.Sprintf("node-role.kubernetes.io/%v=", role)

	initialNodes, _ := k.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: labelSelector})
	for _, node := range initialNodes.Items {
		initialNodeNames = append(initialNodeNames, node.Name)
	}
	log.Printf("Found nodes %v, waiting for rotation", initialNodeNames)

	for {
		var scannedNodeNames []string
		var nodeMatch int
		log.Printf("waiting for rotation, attempt %v/%v", pollingCounter, timeoutCounter)
		nodeList, _ := k.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: labelSelector})
		for _, node := range nodeList.Items {
			scannedNodeNames = append(scannedNodeNames, node.Name)
		}

		log.Printf("found nodes %v, comparing to %v", scannedNodeNames, initialNodeNames)

		if initialNodeNames != nil && scannedNodeNames != nil {
			if len(initialNodeNames) == len(scannedNodeNames) {
				for _, value := range initialNodeNames {
					if ContainsString(scannedNodeNames, value) {
						nodeMatch++
					}
				}
				if nodeMatch == 0 {
					return true
				}
			}
		}

		time.Sleep(pollingInterval)
		log.Println("nodes did not rotate yet, retrying")
		pollingCounter++
		if pollingCounter == timeoutCounter {
			break
		}
	}
	return false
}

func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
