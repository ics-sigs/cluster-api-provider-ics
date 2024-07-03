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

package cmd

import (
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/ics-sigs/cluster-api-provider-ics/packaging/flavorgen/flavors"
	"github.com/ics-sigs/cluster-api-provider-ics/packaging/flavorgen/flavors/util"
)

const flavorFlag = "flavor"

func RootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "flavorgen",
		Short: "flavorgen generates clusterctl templates for Cluster API Provider ICS",
		RunE: func(command *cobra.Command, args []string) error {
			return RunRoot(command)
		},
	}
	rootCmd.Flags().StringP(flavorFlag, "f", "", "Name of flavor to compile")
	return rootCmd
}

func Execute() {
	if err := RootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func RunRoot(command *cobra.Command) error {
	flavor, err := command.Flags().GetString(flavorFlag)
	if err != nil {
		return errors.Wrapf(err, "error accessing flag %s for command %s", flavorFlag, command.Name())
	}
	switch flavor {
	case "loadbalancer":
		util.PrintObjects(flavors.MultiNodeTemplateWithLoadBalancer())
	default:
		//return errors.Errorf("invalid flavor")
		util.PrintObjects(flavors.MultiNodeTemplateWithOutLoadBalancer())
	}
	return nil
}
