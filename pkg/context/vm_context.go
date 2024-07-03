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

package context

import (
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/cluster-api/util/patch"

	infrav1 "github.com/ics-sigs/cluster-api-provider-ics/api/v1beta1"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/session"
)

// VMContext is a Go context used with a ICSVM.
type VMContext struct {
	*ControllerContext
	ClusterModuleInfo *string
	ICSVM             *infrav1.ICSVM
	PatchHelper       *patch.Helper
	Logger            logr.Logger
	Session           *session.Session
}

// String returns ICSVMGroupVersionKind ICSVMNamespace/ICSVMName.
func (c *VMContext) String() string {
	return fmt.Sprintf("%s %s/%s", c.ICSVM.GroupVersionKind(), c.ICSVM.Namespace, c.ICSVM.Name)
}

// Patch updates the object and its status on the API server.
func (c *VMContext) Patch() error {
	return c.PatchHelper.Patch(c, c.ICSVM)
}

// GetLogger returns this context's logger.
func (c *VMContext) GetLogger() logr.Logger {
	return c.Logger
}

// GetSession returns this context's session.
func (c *VMContext) GetSession() *session.Session {
	return c.Session
}
