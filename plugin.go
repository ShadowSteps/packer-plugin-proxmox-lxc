package main

import (
	"github.com/hashicorp/packer/packer/plugin"
	"proxmox-lxc/proxmox-lxc"
)


func main() {
	server, err := plugin.Server()
	if err != nil {
		panic(err)
	}
	server.RegisterBuilder(new(proxmox_lxc.Builder))
	server.Serve()
}