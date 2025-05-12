/*
Copyright 2024 The Kubernetes Authors.

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

package util

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	ICSErrorCode = "code"

	ICSErrorMessage = "message"
)

func IsNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func ExtractICSError(msg string) (map[string]string, error) {
	re := regexp.MustCompile(`^Service response error: map\[(.*)\]$`)
	matches := re.FindStringSubmatch(msg)

	if len(matches) < 2 {
		return nil, fmt.Errorf("iCenter base server error: %s", msg)
	}
	result := make(map[string]string)

	if matches[1] == "" {
		return result, nil
	}
	codex := 0
	if codex = strings.Index(matches[1], " message:"); codex != -1 {
		code := matches[1][:codex]
		if idx := strings.Index(code, ":"); idx != -1 {
			value := code[idx+1:]
			result[ICSErrorCode] = value
		}
	}
	message := ""
	if msgx := strings.Index(matches[1], " params:"); msgx != -1 {
		message = matches[1][codex+1:msgx]
	} else {
		message = matches[1][codex+1:]
	}
	if idx := strings.Index(msg, ":"); idx != -1 {
		value := message[idx+1:]
		result[ICSErrorMessage] = value
	}
	return result, nil
}