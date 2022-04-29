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
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	Base64  string = "^(?:[A-Za-z0-9+\\/]{4})*(?:[A-Za-z0-9+\\/]{2}==|[A-Za-z0-9+\\/]{3}=|[A-Za-z0-9+\\/]{4})$"
	Percent string = "^[0-9]+%$"
)

var (
	rxBase64 = regexp.MustCompile(Base64)
)

func IsBase64(str string) bool {
	return rxBase64.MatchString(str)
}

func GetDecodedString(str string) (string, error) {
	if IsBase64(str) {
		d, err := base64.StdEncoding.DecodeString(str)
		if err != nil {
			return "", err
		}
		return string(d), nil
	}
	return str, nil
}

// ContainsString returns true if a given slice 'slice' contains string 's', otherwise return false
func ContainsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func RemoveDuplicateValues(strSlice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range strSlice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func ContainsEqualFoldSubstring(str, substr string) bool {
	x := strings.ToLower(str)
	y := strings.ToLower(substr)
	if strings.Contains(x, y) {
		return true
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

func StringMD5(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func StringMapSliceContains(m []map[string]string, contains map[string]string) bool {
	for _, obj := range m {
		if reflect.DeepEqual(obj, contains) {
			return true
		}
	}
	return false
}

func FieldValue(path string, obj map[string]interface{}) interface{} {
	var resourceField interface{}

	p := FieldPath(path)

	if field, ok, _ := unstructured.NestedFieldCopy(obj, p...); ok {
		resourceField = field
	}

	return resourceField
}

func SetFieldValue(path string, obj map[string]interface{}, value interface{}) error {
	p := FieldPath(path)
	if err := unstructured.SetNestedField(obj, value, p...); err != nil {
		return err
	}

	return nil
}

func FieldPathString(path ...string) string {
	return strings.Join(path, ".")
}

func FieldPath(path string) []string {
	return strings.Split(path, ".")
}

func AppendUnique(slice []interface{}, i interface{}) []interface{} {
	for _, ele := range slice {
		if reflect.DeepEqual(ele, i) {
			return slice
		}
	}
	return append(slice, i)
}

func AppendUniqueIndex(slice []interface{}, i interface{}, idx string, override bool) []interface{} {
	var fieldStr, fieldStr2 string
	appendIdxVal := reflect.ValueOf(i)

	switch appendIdxVal.Kind() {
	case reflect.Map:
		for _, e := range appendIdxVal.MapKeys() {
			if strings.EqualFold(e.String(), idx) {
				fieldStr = appendIdxVal.MapIndex(e).Elem().String()
			}
		}

		for ix, ele := range slice {
			compareIdxVal := reflect.ValueOf(ele)
			for _, e := range compareIdxVal.MapKeys() {
				if strings.EqualFold(e.String(), idx) {
					fieldStr2 = compareIdxVal.MapIndex(e).Elem().String()
				}
			}
			if strings.EqualFold(fieldStr, fieldStr2) {
				if override {
					slice[ix] = i
				}
				return slice
			}
		}
	default:
		return slice
	}

	return append(slice, i)
}

func MergeSliceByUnique(sl1, sl2 []interface{}) []interface{} {
	for _, ele := range sl2 {
		sl1 = AppendUnique(sl1, ele)
	}
	return sl1
}

func MergeSliceByIndex(sl1, sl2 []interface{}, idx string, override bool) []interface{} {
	for _, ele := range sl2 {
		sl1 = AppendUniqueIndex(sl1, ele, idx, override)
	}
	return sl1
}

func StringSliceEqualFold(x []string, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	for _, element := range x {
		if !ContainsEqualFold(y, element) {
			return false
		}
	}
	return true
}

func SliceEmpty(slice []string) bool {
	return len(slice) == 0
}

func MapEmpty(m map[string]string) bool {
	return len(m) == 0
}

func StringEmpty(str string) bool {
	return str == ""
}

func StringValue(str *string) string {
	if str != nil {
		return *str
	}
	return ""
}

func StringPtr(str string) *string {
	return &str
}

func Int64Value(i *int64) int64 {
	if i != nil {
		return *i
	}
	return 0
}

func Int64InRange(i, min, max int64) bool {
	if (i >= min) && (i <= max) {
		return true
	}
	return false
}

func StringSliceEquals(x, y []string) bool {
	if x == nil {
		x = []string{}
	}
	if y == nil {
		y = []string{}
	}
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

func Min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func GetLastElementBy(s, sep string) string {
	sp := strings.Split(s, sep)
	return sp[len(sp)-1]
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
	return n.Format("20060102150405")
}

// Set Difference: A - B
func Difference(a, b []string) []string {
	var diff []string
	m := make(map[string]bool)

	for _, item := range b {
		m[item] = true
	}

	for _, item := range a {
		if _, ok := m[item]; !ok {
			diff = append(diff, item)
		}
	}
	return diff
}

func IsValidPercent(percent string) error {
	if !regexp.MustCompile(Percent).MatchString(percent) {
		return errors.Errorf("invalid percent value %v", percent)
	}
	return nil
}

func IntOrStrValue(x *intstr.IntOrString) int {
	var value int
	if x.Type == intstr.String {
		if err := IsValidPercent(x.StrVal); err == nil {
			value, _ = strconv.Atoi(x.StrVal[:len(x.StrVal)-1])
		}
	} else {
		value = x.IntValue()
	}

	return value
}

func Int64ToStr(x int64) string {
	return strconv.FormatInt(x, 10)
}

// GetStringIndexInSlice returns index of string 's' if a given slice 'slice' contains string 's', otherwise return -1
func GetStringIndexInSlice(slice []string, s string) int {
	for index, item := range slice {
		if item == s {
			return index
		}
	}
	return -1
}
