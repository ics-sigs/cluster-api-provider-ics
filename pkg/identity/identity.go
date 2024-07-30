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

package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	infrav1 "github.com/ics-sigs/cluster-api-provider-ics/api/v1beta1"
	basev1 "github.com/ics-sigs/cluster-api-provider-ics/pkg/services/goclient/icenter"
)

const (
	CloudsSecretKey = "clouds.yaml"
	CaSecretKey     = "cacert"
)

type Credentials struct {
	Username string
	Password string
}

func ValidateInputs(c client.Client, cluster *infrav1.ICSCluster) error {
	if c == nil {
		return errors.New("kubernetes client is required")
	}
	if cluster == nil {
		return errors.New("ics cluster is required")
	}
	ref := cluster.Spec.IdentityRef
	if ref == nil {
		return errors.New("IdentityRef is required")
	}
	return nil
}

func IsSecretIdentity(cluster *infrav1.ICSCluster) bool {
	if cluster == nil || cluster.Spec.IdentityRef == nil {
		return false
	}

	return cluster.Spec.IdentityRef.Kind == infrav1.SecretKind
}

func IsOwnedByIdentityOrCluster(ownerReferences []metav1.OwnerReference) bool {
	if len(ownerReferences) > 0 {
		for _, ownerReference := range ownerReferences {
			if !strings.Contains(ownerReference.APIVersion, infrav1.GroupName+"/") {
				continue
			}
			if ownerReference.Kind == "ICSCluster" {
				return true
			}
		}
	}
	return false
}

func ValidateMachineInputs(c client.Client, vm *infrav1.ICSVM) error {
	if c == nil {
		return errors.New("kubernetes client is required")
	}
	if vm == nil {
		return errors.New("ics vm is required")
	}
	ref := vm.Spec.IdentityRef
	if ref == nil {
		return errors.New("IdentityRef is required")
	}
	return nil
}

func IsMachineSecretIdentity(identityRef *infrav1.ICSIdentityReference) bool {
	if identityRef == nil {
		return false
	}

	return identityRef.Kind == infrav1.SecretKind
}

func NewClientFromMachine(ctx context.Context, ctrlClient client.Client, nameSpace string, cloudName string, identityRef *infrav1.ICSIdentityReference) (*basev1.ICenter, error) {
	var iCenter basev1.ICenter
	var caCert []byte

	if identityRef != nil {
		var err error
		iCenter, caCert, err = getCloudFromSecret(ctx, ctrlClient, nameSpace, identityRef.Name, cloudName)
		if err != nil {
			return nil, err
		}
		if caCert != nil && len(caCert) > 256 {
			iCenter.CACertFile = string(caCert)
			isSecure := true
			iCenter.Verify = &isSecure
		}
	}
	return &iCenter, nil
}

func NewClientFromCluster(ctx context.Context, ctrlClient client.Client, icsCluster *infrav1.ICSCluster) (*basev1.ICenter, error) {
	var iCenter basev1.ICenter
	var caCert []byte

	if icsCluster.Spec.IdentityRef != nil {
		var err error
		iCenter, caCert, err = getCloudFromSecret(ctx, ctrlClient, icsCluster.Namespace, icsCluster.Spec.IdentityRef.Name, icsCluster.Spec.CloudName)
		if err != nil {
			return nil, err
		}
		if caCert != nil && len(caCert) > 256 {
			iCenter.CACertFile = string(caCert)
			isSecure := true
			iCenter.Verify = &isSecure
		}
	}
	return &iCenter, nil
}

//func  NewClient(cloud basev1.ICenter, caCert []byte) (*basev1.ICenter, error) {
//	config := &tls.Config{
//		RootCAs:    x509.NewCertPool(),
//		MinVersion: tls.VersionTLS12,
//	}
//	if cloud.Verify != nil {
//		config.InsecureSkipVerify = !*cloud.Verify
//	}
//	if caCert != nil {
//		config.RootCAs.AppendCertsFromPEM(caCert)
//	}
//
//	cloud.CACertFile = string(caCert)
//
//	return &cloud, nil
//}

// getCloudFromSecret extract a Cloud from the given namespace:secretName.
func getCloudFromSecret(ctx context.Context, ctrlClient client.Client, secretNamespace string, secretName string, cloudName string) (basev1.ICenter, []byte, error) {
	emptyCloud := basev1.ICenter{}

	if secretName == "" {
		return emptyCloud, nil, nil
	}

	if cloudName == "" {
		return emptyCloud, nil, fmt.Errorf("secret name set to %v but no cloud was specified. Please set cloud_name in your machine spec", secretName)
	}

	secret := &corev1.Secret{}
	err := ctrlClient.Get(ctx, types.NamespacedName{
		Namespace: secretNamespace,
		Name:      secretName,
	}, secret)
	if err != nil {
		return emptyCloud, nil, err
	}

	content, ok := secret.Data[CloudsSecretKey]
	if !ok {
		return emptyCloud, nil, fmt.Errorf("ICS credentials secret %v did not contain key %v",
			secretName, CloudsSecretKey)
	}
	var clouds basev1.Clouds
	if err = yaml.Unmarshal(content, &clouds); err != nil {
		return emptyCloud, nil, fmt.Errorf("failed to unmarshal clouds credentials stored in secret %v: %v", secretName, err)
	}

	// get caCert
	caCert, ok := secret.Data[CaSecretKey]
	if !ok {
		return clouds.Clouds[cloudName], nil, nil
	}

	return clouds.Clouds[cloudName], caCert, nil
}