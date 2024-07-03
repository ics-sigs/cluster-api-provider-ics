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

package infrastructure

import (
	"encoding/base64"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apitypes "k8s.io/apimachinery/pkg/types"

	basehstv1 "github.com/ics-sigs/ics-go-sdk/host"
	basetkv1 "github.com/ics-sigs/ics-go-sdk/task"
	basevmv1 "github.com/ics-sigs/ics-go-sdk/vm"

	infrav1 "github.com/ics-sigs/cluster-api-provider-ics/api/v1beta1"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/context"
	basev1 "github.com/ics-sigs/cluster-api-provider-ics/pkg/services/goclient/icenter"
	"github.com/ics-sigs/cluster-api-provider-ics/pkg/services/goclient/net"
	infrautilv1 "github.com/ics-sigs/cluster-api-provider-ics/pkg/util"
)

// VMService provdes API to interact with the VMs using golang ics sdk
type VMService struct{}

// ReconcileVM makes sure that the VM is in the desired state by:
//   1. Creating the VM if it does not exist, then...
//   2. Updating the VM with the bootstrap data, such as the cloud-init meta and user data, before...
//   3. Powering on the VM, and finally...
//   4. Returning the real-time state of the VM to the caller
func (vms *VMService) ReconcileVM(ctx *context.VMContext) (vm infrav1.VirtualMachine, _ error) {
	// Initialize the result.
	vm = infrav1.VirtualMachine{
		Name:  ctx.ICSVM.Name,
		State: infrav1.VirtualMachineStatePending,
	}

	// If there is an in-flight task associated with this VM then do not
	// reconcile the VM until the task is completed.
	if inFlight, err := reconcileInFlightTask(ctx); err != nil || inFlight {
		return vm, err
	}

	// This deferred function will trigger a reconcile event for the
	// ICSVM resource once its associated task completes. If
	// there is no task for the ICSVM resource then no reconcile
	// event is triggered.
	defer reconcileICSVMOnTaskCompletion(ctx)

	// Before going further, we need the VM's managed object reference.
	vmRef, err := findVM(ctx)
	if err != nil {
		ctx.Logger.Error(err, "fail to get vm object reference")

		// Get the bootstrap data.
		metadata, err := vms.getBootstrapData(ctx)
		if err != nil {
			return vm, err
		}
		metadataBytes, err := base64.StdEncoding.DecodeString(metadata)
		if err != nil {
			ctx.Logger.Error(err, "fail to decode bootstrap data")
			return vm, err
		}

		// Otherwise, this is a new machine and the  the VM should be created.
		// Create the VM.
		return vm, basev1.CreateVM(ctx, string(metadataBytes))
	}

	// At this point we know the VM exists, so it needs to be updated.
	// Create a new virtualMachineContext to reconcile the VM.
	vmCtx := &virtualMachineContext{
		VMContext: *ctx,
		Obj:       basevmv1.NewVirtualMachineService(ctx.Session.Client),
		Ref:       vmRef,
		State:     &vm,
	}

	vms.reconcileUUID(vmCtx)

	if err := vms.reconcileNetworkStatus(vmCtx); err != nil {
		return vm, err
	}

	if ok, err := vms.reconcilePowerState(vmCtx); err != nil || !ok {
		return vm, err
	}

	vm.State = infrav1.VirtualMachineStateReady
	return vm, nil
}

// DestroyVM powers off and destroys a virtual machine.
func (vms *VMService) DestroyVM(ctx *context.VMContext) (infrav1.VirtualMachine, error) {

	vm := infrav1.VirtualMachine{
		Name:  ctx.ICSVM.Name,
		State: infrav1.VirtualMachineStatePending,
	}

	// If there is an in-flight task associated with this VM then do not
	// reconcile the VM until the task is completed.
	if inFlight, err := reconcileInFlightTask(ctx); err != nil || inFlight {
		return vm, err
	}

	// This deferred function will trigger a reconcile event for the
	// ICSVM resource once its associated task completes. If
	// there is no task for the ICSVM resource then no reconcile
	// event is triggered.
	defer reconcileICSVMOnTaskCompletion(ctx)

	// Before going further, we need the VM's managed object reference.
	vmRef, err := findVM(ctx)
	if err != nil {
		ctx.Logger.Error(err, "fail to get vm object reference")
		// If the VM's MoRef could not be found then the VM no longer exists. This
		// is the desired state.
		if isNotFound(err) {
			vm.State = infrav1.VirtualMachineStateNotFound
			return vm, nil
		}
		return vm, err
	}

	//
	// At this point we know the VM exists, so it needs to be destroyed.
	//

	// Create a new virtualMachineContext to reconcile the VM.
	vmCtx := &virtualMachineContext{
		VMContext: *ctx,
		Obj:       basevmv1.NewVirtualMachineService(ctx.Session.Client),
		Ref:       vmRef,
		State:     &vm,
	}

	// Power off the VM.
	powerState, err := vms.getPowerState(vmCtx)
	if err != nil {
		ctx.Logger.Error(err, "fail to get power state of the vm")
		return vm, err
	}
	if powerState == infrav1.VirtualMachinePowerStatePoweredOn {
		task, err := vmCtx.Obj.PowerOffVM(ctx, vmRef.Value)
		if err != nil {
			ctx.Logger.Error(err, "power off the vm error")
			return vm, err
		}
		ctx.ICSVM.Status.TaskRef = task.TaskId
		ctx.Logger.Info("wait for VM to be powered off")
		return vm, nil
	}

	// At this point the VM is not powered on and can be destroyed. Store the
	// destroy task's reference and return a requeue error.
	task, err := vmCtx.Obj.DeleteVMWithCheckParams(ctx, vmRef.Value, true, true, ctx.Session.Password)
	if err != nil {
		ctx.Logger.Error(err, "fail to destroying vm")
		return vm, err
	}
	ctx.ICSVM.Status.TaskRef = task.TaskId
	ctx.Logger.Info("wait for VM to be destroyed")
	return vm, nil
}

func (vms *VMService) reconcileNetworkStatus(ctx *virtualMachineContext) error {
	netStatus, err := vms.getNetworkStatus(ctx)
	if err != nil {
		return err
	}
	ctx.State.Network = netStatus
	if len(netStatus) >= 1 {
		if ctx.ICSVM.Status.Addresses == nil && netStatus[0].IPAddrs != nil {
			infrautilv1.UpdateNetworkInfo(&ctx.VMContext, netStatus)
			if infrautilv1.IsControlPlaneMachine(ctx.ICSVM) {
				err = ctx.Patch()
				if err != nil {
					ctx.Logger.Error(err, "ICSVM Path IPAddress Error")
				}
			}
		}
	}
	return nil
}

func (vms *VMService) reconcilePowerState(ctx *virtualMachineContext) (bool, error) {
	powerState, err := vms.getPowerState(ctx)
	if err != nil {
		return false, err
	}
	switch powerState {
	case infrav1.VirtualMachinePowerStatePoweredOff:
		ctx.Logger.Info("powering on")
		vm, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, nil
		}
		hostService := basehstv1.NewHostService(ctx.Session.Client)
		host, err := hostService.GetHost(ctx, vm.HostID)
		if err != nil {
			return false, nil
		}
		if vm.MemoryInByte >= host.FreeMemoryInByte || vm.MemoryInByte >= host.LogicFreeMemoryInByte {
			return false, errors.New(infrav1.PoweringOnFailedReason)
		}
		task, err := ctx.Obj.PowerOnVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power on op for vm %s", ctx)
		}

		// Update the ICSVM.Status.TaskRef to track the power-on task.
		ctx.ICSVM.Status.TaskRef = task.TaskId

		// Once the VM is successfully powered on, a reconcile request should be
		// triggered once the VM reports IP addresses are available.
		reconcileICSVMWhenNetworkIsReady(ctx, task)

		taskService := basetkv1.NewTaskService(ctx.Session.Client)
		taskInfo, _ := taskService.WaitForResult(ctx, task)
		if taskInfo != nil && taskInfo.State == "ERROR" {
			ctx.Logger.Error(errors.New(taskInfo.Error), "failed to trigger power on the vm")
			return false, errors.New(infrav1.PoweringOnFailedReason)
		} else {
			ctx.Logger.Info("wait for VM to be powered on")
		}
		return false, nil
	case infrav1.VirtualMachinePowerStatePoweredOn:
		ctx.Logger.Info("powered on")
		return true, nil
	default:
		return false, errors.Errorf("unexpected power state %q for vm %s", powerState, ctx)
	}
}

func (vms *VMService) reconcileCloudInit(ctx *virtualMachineContext) (bool, error) {
	vmObj, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return false, errors.Errorf("get vm %s info err", ctx.Ref.Value)
	}

	if vmObj != nil && len(vmObj.CloudInit.UserData) > 0 {
		return true, nil
	}

	ctx.Logger.Info("restarting vm on")

	if vmObj.Status == "STOPPED" {
		ctx.Logger.Info("first powering on")
		powerOnTask, err := ctx.Obj.PowerOnVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power on op for vm %s", ctx)
		}

		// Wait for the VM to be powered off.
		taskService := basetkv1.NewTaskService(ctx.Session.Client)
		powerOnTaskInfo, err := taskService.WaitForResult(ctx, powerOnTask)
		if err != nil && powerOnTaskInfo == nil {
			ctx.Logger.Error(err, "ics task tracing error.", "id", powerOnTask.TaskId)
		}
		time.Sleep(time.Duration(24) * time.Second)

		powerOffTask, err := ctx.Obj.PowerOffVM(ctx, ctx.Ref.Value)
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power off op for vm %s", ctx)
		}

		// Wait for the VM to be powered off.
		taskService = basetkv1.NewTaskService(ctx.Session.Client)
		powerOffTaskInfo, err := taskService.WaitForResult(ctx, powerOffTask)
		if err != nil && powerOffTaskInfo == nil {
			ctx.Logger.Error(err, "ics task tracing error.", "id", powerOffTask.TaskId)
		}
		time.Sleep(time.Duration(12) * time.Second)
	}

	ctx.Logger.Info("Reconcile CloudInit Will Starting ...")
	return true, nil
}

func (vms *VMService) reconcileUUID(ctx *virtualMachineContext) {
	vm, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		return
	}
	ctx.State.UID = vm.ID
	ctx.State.BiosUUID = vm.UUID
}

func (vms *VMService) getPowerState(ctx *virtualMachineContext) (infrav1.VirtualMachinePowerState, error) {
	vmObj, err := ctx.Obj.GetVM(ctx, ctx.Ref.Value)
	if err != nil {
		ctx.Logger.Error(err, "fail to get vm info from ics")
		return "", err
	}

	switch vmObj.Status {
	case "STARTED":
		return infrav1.VirtualMachinePowerStatePoweredOn, nil
	case "STOPPED":
		return infrav1.VirtualMachinePowerStatePoweredOff, nil
	case "PAUSED":
		return infrav1.VirtualMachinePowerStateSuspended, nil
	case "RESTARTING":
		return infrav1.VirtualMachinePowerStateSuspended, nil
	case "PENDING":
		return infrav1.VirtualMachinePowerStateSuspended, nil
	default:
		return "", errors.Errorf("unexpected power state %q for vm %s", vmObj.Status, ctx)
	}
}

func (vms *VMService) getNetworkStatus(ctx *virtualMachineContext) ([]infrav1.NetworkStatus, error) {
	allNetStatus, err := net.GetNetworkStatus(&ctx.VMContext, ctx.Session.Client, ctx.Ref)
	if err != nil {
		ctx.Logger.Info("got allNetStatus", "err", err)
		return nil, err
	}
	ctx.Logger.Info("got allNetStatus", "status", allNetStatus)
	apiNetStatus := []infrav1.NetworkStatus{}
	for _, s := range allNetStatus {
		apiNetStatus = append(apiNetStatus, infrav1.NetworkStatus{
			Connected:   s.Connected,
			IPAddrs:     sanitizeIPAddrs(&ctx.VMContext, s.IPAddrs),
			MACAddr:     s.MACAddr,
			NetworkName: s.NetworkName,
		})
	}
	return apiNetStatus, nil
}

func (vms *VMService) getBootstrapData(ctx *context.VMContext) (string, error) {
	if ctx.ICSVM.Spec.BootstrapRef == nil {
		ctx.Logger.Info("VM has no bootstrap data")
		return "", errors.New("error retrieving bootstrap data: linked icsvm's bootstrapRef is nil")
	}

	secret := &corev1.Secret{}
	secretKey := apitypes.NamespacedName{
		Namespace: ctx.ICSVM.Spec.BootstrapRef.Namespace,
		Name:      ctx.ICSVM.Spec.BootstrapRef.Name,
	}
	if err := ctx.Client.Get(ctx, secretKey, secret); err != nil {
		return "", errors.Wrapf(err, "failed to retrieve bootstrap data secret for %s", ctx)
	}

	value, ok := secret.Data["value"]
	if !ok {
		return "", errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return base64.StdEncoding.EncodeToString(value), nil
}
