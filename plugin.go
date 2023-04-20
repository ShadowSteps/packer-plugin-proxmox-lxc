package main

import (
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/plugin"
	"os"
	"proxmox-lxc/proxmox-lxc"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder("proxmox-lxc", new(proxmox_lxc.Builder))
	err := pps.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
