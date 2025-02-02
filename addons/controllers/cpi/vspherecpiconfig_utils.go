// Copyright 2022 VMware, Inc. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	capvv1beta1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/v1beta1"
	capvvmwarev1beta1 "sigs.k8s.io/cluster-api-provider-vsphere/apis/vmware/v1beta1"
	clusterapiv1beta1 "sigs.k8s.io/cluster-api/api/v1beta1"

	"github.com/vmware-tanzu/tanzu-framework/addons/pkg/constants"
	pkgtypes "github.com/vmware-tanzu/tanzu-framework/addons/pkg/types"
	"github.com/vmware-tanzu/tanzu-framework/addons/pkg/util"
	cpiv1alpha1 "github.com/vmware-tanzu/tanzu-framework/apis/cpi/v1alpha1"
)

// mapCPIConfigToDataValuesNonParavirtual generates CPI data values for non-paravirtual modes
func (r *VSphereCPIConfigReconciler) mapCPIConfigToDataValuesNonParavirtual( // nolint
	ctx context.Context,
	cpiConfig *cpiv1alpha1.VSphereCPIConfig, cluster *clusterapiv1beta1.Cluster) (*VSphereCPIDataValues, error,
) { // nolint:whitespace
	d := &VSphereCPIDataValues{}
	c := cpiConfig.Spec.VSphereCPI.NonParavirtualConfig
	d.VSphereCPI.Mode = VsphereCPINonParavirtualMode

	// get the vsphere cluster object
	vsphereCluster, err := r.getVSphereCluster(ctx, cluster)
	if err != nil {
		return nil, err
	}

	// derive the thumbprint, server from the vsphere cluster object
	d.VSphereCPI.TLSThumbprint = vsphereCluster.Spec.Thumbprint
	d.VSphereCPI.Server = vsphereCluster.Spec.Server

	// derive vSphere username and password from the <cluster name> secret
	clusterSecret, err := r.getSecret(ctx, cluster.Namespace, cluster.Name)
	if err != nil {
		return nil, err
	}
	d.VSphereCPI.Username, d.VSphereCPI.Password, err = getUsernameAndPasswordFromSecret(clusterSecret)
	if err != nil {
		return nil, err
	}

	// get the control plane machine template
	cpMachineTemplate := &capvv1beta1.VSphereMachineTemplate{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      controlPlaneName(cluster.Name),
	}, cpMachineTemplate); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.Errorf("VSphereMachineTemplate %s/%s not found", cluster.Namespace, controlPlaneName(cluster.Name))
		}
		return nil, errors.Errorf("VSphereMachineTemplate %s/%s could not be fetched, error %v", cluster.Namespace, controlPlaneName(cluster.Name), err)
	}

	// derive data center information from control plane machine template, if not provided
	d.VSphereCPI.Datacenter = cpMachineTemplate.Spec.Template.Spec.Datacenter

	// derive ClusterCidr from cluster.spec.clusterNetwork
	if cluster.Spec.ClusterNetwork != nil && cluster.Spec.ClusterNetwork.Pods != nil && len(cluster.Spec.ClusterNetwork.Pods.CIDRBlocks) > 0 {
		d.VSphereCPI.Nsxt.Routes.ClusterCidr = cluster.Spec.ClusterNetwork.Pods.CIDRBlocks[0]
	}

	// derive IP family or proxy related settings from cluster annotations
	if cluster.Annotations != nil {
		d.VSphereCPI.IPFamily = cluster.Annotations[pkgtypes.IPFamilyConfigAnnotation]
		d.VSphereCPI.HTTPProxy = cluster.Annotations[pkgtypes.HTTPProxyConfigAnnotation]
		d.VSphereCPI.HTTPSProxy = cluster.Annotations[pkgtypes.HTTPSProxyConfigAnnotation]
		d.VSphereCPI.NoProxy = cluster.Annotations[pkgtypes.NoProxyConfigAnnotation]
	}

	// derive nsxt related configs from cluster variable
	d.VSphereCPI.Nsxt.PodRoutingEnabled = r.tryParseClusterVariableBool(cluster, NsxtPodRoutingEnabledVarName)
	d.VSphereCPI.Nsxt.Routes.RouterPath = r.tryParseClusterVariableString(cluster, NsxtRouterPathVarName)
	d.VSphereCPI.Nsxt.Routes.ClusterCidr = r.tryParseClusterVariableString(cluster, ClusterCIDRVarName)
	d.VSphereCPI.Nsxt.Username = r.tryParseClusterVariableString(cluster, NsxtUsernameVarName)
	d.VSphereCPI.Nsxt.Password = r.tryParseClusterVariableString(cluster, NsxtPasswordVarName)
	d.VSphereCPI.Nsxt.Host = r.tryParseClusterVariableString(cluster, NsxtManagerHostVarName)
	d.VSphereCPI.Nsxt.InsecureFlag = r.tryParseClusterVariableBool(cluster, NsxtAllowUnverifiedSSLVarName)
	d.VSphereCPI.Nsxt.RemoteAuth = r.tryParseClusterVariableBool(cluster, NsxtRemoteAuthVarName)
	d.VSphereCPI.Nsxt.VmcAccessToken = r.tryParseClusterVariableString(cluster, NsxtVmcAccessTokenVarName)
	d.VSphereCPI.Nsxt.VmcAuthHost = r.tryParseClusterVariableString(cluster, NsxtVmcAuthHostVarName)
	d.VSphereCPI.Nsxt.ClientCertKeyData = r.tryParseClusterVariableString(cluster, NsxtClientCertKeyDataVarName)
	d.VSphereCPI.Nsxt.ClientCertData = r.tryParseClusterVariableString(cluster, NsxtClientCertDataVarName)
	d.VSphereCPI.Nsxt.RootCAData = r.tryParseClusterVariableString(cluster, NsxtRootCADataB64VarName)
	d.VSphereCPI.Nsxt.SecretName = r.tryParseClusterVariableString(cluster, NsxtSecretNameVarName)
	d.VSphereCPI.Nsxt.SecretNamespace = r.tryParseClusterVariableString(cluster, NsxtSecretNamespaceVarName)

	// allow API user to override the derived values if he/she specified fields in the VSphereCPIConfig
	d.VSphereCPI.TLSThumbprint = tryParseString(d.VSphereCPI.TLSThumbprint, c.TLSThumbprint)
	d.VSphereCPI.Server = tryParseString(d.VSphereCPI.Server, c.VCenterAPIEndpoint)
	d.VSphereCPI.Server = tryParseString(d.VSphereCPI.Server, c.VCenterAPIEndpoint)
	d.VSphereCPI.Datacenter = tryParseString(d.VSphereCPI.Datacenter, c.Datacenter)

	if c.VSphereCredentialLocalObjRef != nil {
		vsphereSecret, err := r.getSecret(ctx, cpiConfig.Namespace, c.VSphereCredentialLocalObjRef.Name)
		if err != nil {
			return nil, err
		}
		d.VSphereCPI.Username, d.VSphereCPI.Password, err = getUsernameAndPasswordFromSecret(vsphereSecret)
		if err != nil {
			return nil, err
		}
	}

	d.VSphereCPI.Region = tryParseString(d.VSphereCPI.Region, c.Region)
	d.VSphereCPI.Zone = tryParseString(d.VSphereCPI.Zone, c.Zone)
	if c.Insecure != nil {
		d.VSphereCPI.InsecureFlag = *c.Insecure
	}

	if c.VMNetwork != nil {
		d.VSphereCPI.VMInternalNetwork = tryParseString(d.VSphereCPI.VMInternalNetwork, c.VMNetwork.Internal)
		d.VSphereCPI.VMExternalNetwork = tryParseString(d.VSphereCPI.VMExternalNetwork, c.VMNetwork.External)
		d.VSphereCPI.VMExcludeInternalNetworkSubnetCidr = tryParseString(d.VSphereCPI.VMExcludeInternalNetworkSubnetCidr, c.VMNetwork.ExcludeInternalSubnetCidr)
		d.VSphereCPI.VMExcludeExternalNetworkSubnetCidr = tryParseString(d.VSphereCPI.VMExcludeExternalNetworkSubnetCidr, c.VMNetwork.ExcludeExternalSubnetCidr)
	}
	d.VSphereCPI.CloudProviderExtraArgs.TLSCipherSuites = tryParseString(d.VSphereCPI.CloudProviderExtraArgs.TLSCipherSuites, c.TLSCipherSuites)

	if c.NSXT != nil {
		if c.NSXT.PodRoutingEnabled != nil {
			d.VSphereCPI.Nsxt.PodRoutingEnabled = *c.NSXT.PodRoutingEnabled
		}

		if c.NSXT.Insecure != nil {
			d.VSphereCPI.Nsxt.InsecureFlag = *c.NSXT.Insecure
		}
		if c.NSXT.Route != nil {
			d.VSphereCPI.Nsxt.Routes.RouterPath = tryParseString(d.VSphereCPI.Nsxt.Routes.RouterPath, c.NSXT.Route.RouterPath)
		}
		if c.NSXT.CredentialLocalObjRef != nil {
			d.VSphereCPI.Nsxt.SecretName = c.NSXT.CredentialLocalObjRef.Name
			d.VSphereCPI.Nsxt.SecretNamespace = cpiConfig.Namespace
			nsxtSecret, err := r.getSecret(ctx, cpiConfig.Namespace, c.NSXT.CredentialLocalObjRef.Name)
			if err != nil {
				return nil, err
			}
			d.VSphereCPI.Nsxt.Username, d.VSphereCPI.Nsxt.Password, err = getUsernameAndPasswordFromSecret(nsxtSecret)
			if err != nil {
				return nil, err
			}
		}
		d.VSphereCPI.Nsxt.Host = tryParseString(d.VSphereCPI.Nsxt.Host, c.NSXT.APIHost)
		if c.NSXT.RemoteAuth != nil {
			d.VSphereCPI.Nsxt.RemoteAuth = *c.NSXT.RemoteAuth
		}
		d.VSphereCPI.Nsxt.VmcAccessToken = tryParseString(d.VSphereCPI.Nsxt.VmcAccessToken, c.NSXT.VMCAccessToken)
		d.VSphereCPI.Nsxt.VmcAuthHost = tryParseString(d.VSphereCPI.Nsxt.VmcAccessToken, c.NSXT.VMCAuthHost)
		d.VSphereCPI.Nsxt.ClientCertKeyData = tryParseString(d.VSphereCPI.Nsxt.ClientCertKeyData, c.NSXT.ClientCertKeyData)
		d.VSphereCPI.Nsxt.ClientCertData = tryParseString(d.VSphereCPI.Nsxt.ClientCertData, c.NSXT.ClientCertData)
		d.VSphereCPI.Nsxt.RootCAData = tryParseString(d.VSphereCPI.Nsxt.RootCAData, c.NSXT.RootCAData)
	}

	d.VSphereCPI.IPFamily = tryParseString(d.VSphereCPI.IPFamily, c.IPFamily)
	if c.Proxy != nil {
		d.VSphereCPI.HTTPProxy = tryParseString(d.VSphereCPI.HTTPProxy, c.Proxy.HTTPProxy)
		d.VSphereCPI.HTTPSProxy = tryParseString(d.VSphereCPI.HTTPSProxy, c.Proxy.HTTPSProxy)
		d.VSphereCPI.NoProxy = tryParseString(d.VSphereCPI.NoProxy, c.Proxy.NoProxy)
	}
	return d, nil
}

// mapCPIConfigToDataValuesParavirtual generates CPI data values for paravirtual modes
func (r *VSphereCPIConfigReconciler) mapCPIConfigToDataValuesParavirtual(_ context.Context, _ *cpiv1alpha1.VSphereCPIConfig, cluster *clusterapiv1beta1.Cluster) (*VSphereCPIDataValues, error) {
	d := &VSphereCPIDataValues{}
	d.VSphereCPI.Mode = VSphereCPIParavirtualMode

	// derive owner cluster information
	d.VSphereCPI.ClusterAPIVersion = cluster.GroupVersionKind().GroupVersion().String()
	d.VSphereCPI.ClusterKind = cluster.GroupVersionKind().Kind
	d.VSphereCPI.ClusterName = cluster.ObjectMeta.Name
	d.VSphereCPI.ClusterUID = string(cluster.ObjectMeta.UID)

	d.VSphereCPI.SupervisorMasterEndpointIP = SupervisorEndpointHostname
	d.VSphereCPI.SupervisorMasterPort = fmt.Sprint(SupervisorEndpointPort)

	return d, nil
}

// mapCPIConfigToDataValues maps VSphereCPIConfig CR to data values
func (r *VSphereCPIConfigReconciler) mapCPIConfigToDataValues(ctx context.Context, cpiConfig *cpiv1alpha1.VSphereCPIConfig, cluster *clusterapiv1beta1.Cluster) (*VSphereCPIDataValues, error) {
	mode := *cpiConfig.Spec.VSphereCPI.Mode
	switch mode {
	case VsphereCPINonParavirtualMode:
		return r.mapCPIConfigToDataValuesNonParavirtual(ctx, cpiConfig, cluster)
	case VSphereCPIParavirtualMode:
		return r.mapCPIConfigToDataValuesParavirtual(ctx, cpiConfig, cluster)
	default:
		break
	}
	return nil, errors.Errorf("Invalid CPI mode %s, must either be %s or %s", mode, VSphereCPIParavirtualMode, VsphereCPINonParavirtualMode)
}

// mapCPIConfigToProviderServiceAccountSpec maps CPIConfig and cluster to the corresponding service account spec
func (r *VSphereCPIConfigReconciler) mapCPIConfigToProviderServiceAccountSpec(cluster *clusterapiv1beta1.Cluster) capvvmwarev1beta1.ProviderServiceAccountSpec {
	return capvvmwarev1beta1.ProviderServiceAccountSpec{
		Ref: &v1.ObjectReference{Name: cluster.Name, Namespace: cluster.Namespace},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"get", "create", "update", "patch", "delete"},
				APIGroups: []string{"vmoperator.vmware.com"},
				Resources: []string{"virtualmachineservices", "virtualmachineservices/status"},
			},
			{
				Verbs:     []string{"get", "list"},
				APIGroups: []string{"vmoperator.vmware.com"},
				Resources: []string{"virtualmachines", "virtualmachines/status"},
			},
			{
				Verbs:     []string{"get", "create", "update", "list", "patch", "delete", "watch"},
				APIGroups: []string{"nsx.vmware.com"},
				Resources: []string{"ippools", "ippools/status"},
			},
			{
				Verbs:     []string{"get", "create", "update", "list", "patch", "delete"},
				APIGroups: []string{"nsx.vmware.com"},
				Resources: []string{"routesets", "routesets/status"},
			},
		},
		TargetNamespace:  ProviderServiceAccountSecretNamespace,
		TargetSecretName: ProviderServiceAccountSecretName,
	}
}

// getOwnerCluster verifies that the VSphereCPIConfig has a cluster as its owner reference,
// and returns the cluster. It tries to read the cluster name from the VSphereCPIConfig's owner reference objects.
// If not there, we assume the owner cluster and VSphereCPIConfig always has the same name.
func (r *VSphereCPIConfigReconciler) getOwnerCluster(ctx context.Context, cpiConfig *cpiv1alpha1.VSphereCPIConfig) (*clusterapiv1beta1.Cluster, error) {
	cluster := &clusterapiv1beta1.Cluster{}
	clusterName := cpiConfig.Name

	// retrieve the owner cluster for the VSphereCPIConfig object
	for _, ownerRef := range cpiConfig.GetOwnerReferences() {
		if strings.EqualFold(ownerRef.Kind, constants.ClusterKind) {
			clusterName = ownerRef.Name
			break
		}
	}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: cpiConfig.Namespace, Name: clusterName}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info(fmt.Sprintf("Cluster resource '%s/%s' not found", cpiConfig.Namespace, clusterName))
			return nil, nil
		}
		r.Log.Error(err, fmt.Sprintf("Unable to fetch cluster '%s/%s'", cpiConfig.Namespace, clusterName))
		return nil, err
	}
	r.Log.Info(fmt.Sprintf("Cluster resource '%s/%s' is successfully found", cpiConfig.Namespace, clusterName))
	return cluster, nil
}

// TODO: make these functions accessible to other controllers (for example csi) https://github.com/vmware-tanzu/tanzu-framework/issues/2086
// getVSphereCluster gets the VSphereCluster CR for the cluster object
func (r *VSphereCPIConfigReconciler) getVSphereCluster(ctx context.Context, cluster *clusterapiv1beta1.Cluster) (*capvv1beta1.VSphereCluster, error) {
	vsphereCluster := &capvv1beta1.VSphereCluster{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}, vsphereCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.Errorf("VSphereCluster %s/%s not found", cluster.Namespace, cluster.Name)
		}
		return nil, errors.Errorf("VSphereCluster %s/%s could not be fetched, error %v", cluster.Namespace, cluster.Name, err)
	}
	return vsphereCluster, nil
}

// getSecret gets the secret object given its name and namespace
func (r *VSphereCPIConfigReconciler) getSecret(ctx context.Context, namespace, name string) (*v1.Secret, error) {
	secret := &v1.Secret{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, errors.Errorf("Secret %s/%s not found", namespace, name)
		}
		return nil, errors.Errorf("Secret %s/%s could not be fetched, error %v", namespace, name, err)
	}
	return secret, nil
}

// getUsernameAndPasswordFromSecret extracts the username and password from a secret object
func getUsernameAndPasswordFromSecret(s *v1.Secret) (string, string, error) {
	username, exists := s.Data["username"]
	if !exists {
		return "", "", errors.Errorf("Secret %s/%s doesn't have string data with username", s.Namespace, s.Name)
	}
	password, exists := s.Data["password"]
	if !exists {
		return "", "", errors.Errorf("Secret %s/%s doesn't have string data with password", s.Namespace, s.Name)
	}
	return string(username), string(password), nil
}

// controlPlaneName returns the control plane name for a cluster name
func controlPlaneName(clusterName string) string {
	return fmt.Sprintf("%s-control-plane", clusterName)
}

// getCCMName returns the name of cloud control manager for a cluster
func getCCMName(cluster *clusterapiv1beta1.Cluster) string {
	return fmt.Sprintf("%s-%s", cluster.Name, "ccm")
}

// tryParseString tries to convert a string pointer and return its value, if not nil
func tryParseString(src string, sub *string) string {
	if sub != nil {
		return *sub
	}
	return src
}

// tryParseClusterVariableBool tries to parse a boolean cluster variable,
// info any error that occurs
func (r *VSphereCPIConfigReconciler) tryParseClusterVariableBool(cluster *clusterapiv1beta1.Cluster, variableName string) bool {
	res, err := util.ParseClusterVariableBool(cluster, variableName)
	if err != nil {
		r.Log.Info(fmt.Sprintf("Cannot parse cluster variable with key %s", variableName))
	}
	return res
}

// tryParseClusterVariableString tries to parse a string cluster variable,
// info any error that occurs
func (r *VSphereCPIConfigReconciler) tryParseClusterVariableString(cluster *clusterapiv1beta1.Cluster, variableName string) string {
	res, err := util.ParseClusterVariableString(cluster, variableName)
	if err != nil {
		r.Log.Info(fmt.Sprintf("cannot parse cluster variable with key %s", variableName))
	}
	return res
}
