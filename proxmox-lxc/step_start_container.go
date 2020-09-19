package proxmox_lxc

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"strconv"

	"github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

// stepStartContainer takes the given configuration and starts a VM on the given Proxmox node.
//
// It sets the vmRef state which is used throughout the later steps to reference the VM
// in API calls.
type stepStartContainer struct{}

func (s *stepStartContainer) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	client := state.Get("proxmoxClient").(*proxmox.Client)
	c := state.Get("config").(*Config)

	ui.Say("Creating LXC Container")

	config := proxmox.NewConfigLxc()
	config.Ostemplate = c.TemplateStoragePool + ":vztmpl/" + c.TemplateFile
	config.Force = true
	config.Password = c.Comm.SSHPassword
	config.Start = true
	config.Storage = c.TemplateStoragePool
	config.RootFs = c.FSStorage + ":" + strconv.Itoa(c.FSSize)
	if c.SSHPublicKeyPath != "" {
		keyPath, err := packer.ExpandUser(c.SSHPublicKeyPath)
		if err == nil {
			content, err := ioutil.ReadFile(keyPath)
			if err == nil {
				config.SSHPublicKeys = string(content)
			} else {
				err := fmt.Errorf("Error uploading SSH key: %s", err)
				ui.Message(err.Error())
			}
		} else {
			err := fmt.Errorf("Error uploading SSH key: %s", err)
			ui.Message(err.Error())
		}
	}
	config.Networks = proxmox.QemuDevices{
		0: {
			"bridge":   "vmbr0",
			"name":     "eth0",
			"ip":       "dhcp",
			"firewall": 0,
			"hwaddr": c.ProvisionMac,
		},
	}

	if c.VMID == 0 {
		ui.Say("No VM ID given, getting next free from Proxmox")
		for n := 0; n < 5; n++ {
			id, err := proxmox.MaxVmId(client)
			if err != nil {
				log.Printf("Error getting max used VM ID: %v (attempt %d/5)", err, n+1)
				continue
			}
			c.VMID = id + 1
			break
		}
		if c.VMID == 0 {
			err := fmt.Errorf("Failed to get free VM ID")
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
	}
	vmRef := proxmox.NewVmRef(c.VMID)
	vmRef.SetNode(c.Node)
	if c.Pool != "" {
		vmRef.SetPool(c.Pool)
	}

	err := config.CreateLxc(vmRef, client)
	if err != nil {
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Store the vm id for later
	state.Put("vmRef", vmRef)
	// instance_id is the generic term used so that users can have access to the
	// instance id inside of the provisioners, used in step_provision.
	state.Put("instance_id", vmRef)

	ui.Say("Starting LXC Container")
	_, err = client.StartVm(vmRef)
	if err != nil {
		err := fmt.Errorf("Error starting VM: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

type startedVMCleaner interface {
	StopVm(*proxmox.VmRef) (string, error)
	DeleteVm(*proxmox.VmRef) (string, error)
}

var _ startedVMCleaner = &proxmox.Client{}

func (s *stepStartContainer) Cleanup(state multistep.StateBag) {
	vmRefUntyped, ok := state.GetOk("vmRef")
	// If not ok, we probably errored out before creating the VM
	if !ok {
		return
	}
	vmRef := vmRefUntyped.(*proxmox.VmRef)

	// The vmRef will actually refer to the created template if everything
	// finished successfully, so in that case we shouldn't cleanup
	if _, ok := state.GetOk("success"); ok {
		return
	}

	client := state.Get("proxmoxClient").(startedVMCleaner)
	ui := state.Get("ui").(packer.Ui)

	// Destroy the server we just created
	ui.Say("Stopping LXC Container")
	_, err := client.StopVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error stopping VM. Please stop and delete it manually: %s", err))
		return
	}

	ui.Say("Deleting LXC Container")
	_, err = client.DeleteVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error deleting VM. Please delete it manually: %s", err))
		return
	}
}
