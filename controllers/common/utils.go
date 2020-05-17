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

package common

import (
	"fmt"
	"io/ioutil"
	"reflect"
	"sort"
	"strings"
	"time"
)

// ContainsString returns true if a given slice 'slice' contains string 's', otherwise return false
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// ContainsEqualFold returns true if a given slice 'slice' contains string 's' under unicode case-folding
func ContainsEqualFold(slice []string, s string) bool {
	for _, item := range slice {
		if strings.EqualFold(item, s) {
			return true
		}
	}
	return false
}

func SliceEmpty(slice []string) bool {
	return len(slice) == 0
}

func StringEmpty(str string) bool {
	return str == ""
}

func StringSliceEquals(x, y []string) bool {
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}

func StringSliceContains(x, y []string) bool {
	for _, s := range x {
		if !ContainsString(y, s) {
			return false
		}
	}
	return true
}

func GetLastElementBy(s, sep string) string {
	sp := strings.Split(s, sep)
	return sp[len(sp)-1]
}

// RemoveString removes a string 's' from slice 'slice'
func RemoveString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

// ConcatenateList joins lists to strings delimited with `delimiter`
func ConcatenateList(list []string, delimiter string) string {
	return strings.Trim(strings.Join(strings.Fields(fmt.Sprint(list)), delimiter), "[]")
}

func ReadFile(path string) ([]byte, error) {
	f, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func GetTimeString() string {
	n := time.Now().UTC()
	return fmt.Sprintf("%v%v%v%v%v%v", n.Year(), int(n.Month()), n.Day(), n.Hour(), n.Minute(), n.Second())
}
