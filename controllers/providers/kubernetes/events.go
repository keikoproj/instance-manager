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

package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/keikoproj/instance-manager/api/instancemgr/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EventKind defines the kind of an event
type EventKind string

// EventLevel defines the level of an event
type EventLevel string

var (
	// InvolvedObjectKind is the default kind of involved objects
	InvolvedObjectKind = "InstanceGroup"
	// EventName is the default name for service events
	EventControllerName = "instance-manager"
	// EventLevelNormal is the level of a normal event
	EventLevelNormal = "Normal"
	// EventLevelWarning is the level of a warning event
	EventLevelWarning = "Warning"

	InstanceGroupCreatedEvent       EventKind = "InstanceGroupCreated"
	InstanceGroupDeletedEvent       EventKind = "InstanceGroupDeleted"
	NodesReadyEvent                 EventKind = "InstanceGroupNodesReady"
	NodesNotReadyEvent              EventKind = "InstanceGroupNodesNotReady"
	InstanceGroupUpgradeFailedEvent EventKind = "InstanceGroupUpgradeFailed"

	EventLevels = map[EventKind]string{
		InstanceGroupCreatedEvent:       EventLevelNormal,
		InstanceGroupDeletedEvent:       EventLevelNormal,
		NodesNotReadyEvent:              EventLevelWarning,
		NodesReadyEvent:                 EventLevelNormal,
		InstanceGroupUpgradeFailedEvent: EventLevelWarning,
	}

	EventMessages = map[EventKind]string{
		InstanceGroupCreatedEvent:       "instance group has been successfully created",
		InstanceGroupDeletedEvent:       "instance group has been successfully deleted",
		InstanceGroupUpgradeFailedEvent: "instance group has failed upgrading",
		NodesNotReadyEvent:              "instance group nodes are not ready",
		NodesReadyEvent:                 "instance group nodes are ready",
	}
)

type EventPublisher struct {
	Client          kubernetes.Interface
	Name            string
	Namespace       string
	UID             types.UID
	ResourceVersion string
}

func (e *EventPublisher) Publish(kind EventKind, keysAndValues ...interface{}) {

	messageFields := make(map[string]string)
	messageFields["msg"] = getEventMessage(kind)

	for i := 0; i < len(keysAndValues); i += 2 {
		key := keysAndValues[i].(string)
		value := keysAndValues[i+1].(string)
		messageFields[key] = value
	}

	payload, err := json.Marshal(messageFields)
	if err != nil {
		log.Error(err, "failed to marshal event message fields", "fields", messageFields)
	}

	now := time.Now()

	eventName := fmt.Sprintf("%v.%v.%v", EventControllerName, time.Now().Unix(), rand.Int())
	event := &v1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eventName,
			Namespace: e.Namespace,
		},
		InvolvedObject: v1.ObjectReference{
			Kind:            InvolvedObjectKind,
			Namespace:       e.Namespace,
			Name:            e.Name,
			APIVersion:      v1alpha1.GroupVersion.Version,
			UID:             e.UID,
			ResourceVersion: e.ResourceVersion,
		},
		Reason:         string(kind),
		Message:        string(payload),
		Type:           getEventLevel(kind),
		FirstTimestamp: metav1.NewTime(now),
		LastTimestamp:  metav1.NewTime(now),
	}

	_, err = e.Client.CoreV1().Events(e.Namespace).Create(context.Background(), event, metav1.CreateOptions{})
	if err != nil {
		log.Error(err, "failed to publish event", "event", event)
	}
}

func getEventLevel(kind EventKind) string {
	if val, ok := EventLevels[kind]; ok {
		return val
	}
	return EventLevelNormal
}

func getEventMessage(kind EventKind) string {
	if val, ok := EventMessages[kind]; ok {
		return val
	}
	return EventLevelNormal
}
