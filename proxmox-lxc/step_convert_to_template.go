package proxmox_lxc

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/hashicorp/packer/helper/multistep"
	"github.com/hashicorp/packer/packer"
)

// stepConvertToTemplate takes the running VM configured in earlier steps, stops it, and
// converts it into a Proxmox template.
//
// It sets the template_id state which is used for Artifact lookup.
type stepConvertToTemplate struct{}

type templateConverter interface {
	DeleteVm(*proxmox.VmRef) (string, error)
	ShutdownVm(*proxmox.VmRef) (string, error)
	CreateTemplate(*proxmox.VmRef) error
	WaitForCompletion(taskResponse map[string]interface{}) (waitExitStatus string, err error)
}

var _ templateConverter = &proxmox.Client{}

func (s *stepConvertToTemplate) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui := state.Get("ui").(packer.Ui)
	c := state.Get("config").(*Config)
	client := state.Get("proxmoxClient").(templateConverter)
	vmRef := state.Get("vmRef").(*proxmox.VmRef)

	ui.Say("Stopping LXC Container")
	_, err := client.ShutdownVm(vmRef)
	if err != nil {
		err := fmt.Errorf("Error converting VM to template, could not stop: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	ui.Say("Converting LXC Container to template")

	tlsConf := &tls.Config{InsecureSkipVerify: true}
	session, _ := proxmox.NewSession(c.proxmoxURL.String(), nil, tlsConf)
	err = session.Login(c.Username, c.Password, "")
	if err != nil {
		err := fmt.Errorf("Error converting VM to template, failed to create session: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	var body = url.Values{}
	body.Add("mode", "stop")
	body.Add("compress", "gzip")
	body.Add("remove", "1")
	body.Add("storage", c.TemplateStoragePool)
	body.Add("vmid", strconv.Itoa(c.VMID))
	var bodyEncode = bytes.NewBufferString(body.Encode()).Bytes()
	resp, err := session.Post("/nodes/"+c.Node+"/vzdump", nil, nil, &bodyEncode)
	if err != nil {
		err := fmt.Errorf("Error converting VM to template, failed to create backup: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	taskResponse, err := proxmox.ResponseJSON(resp)
	if err != nil {
		err := fmt.Errorf("Error converting VM to template, faield to parse backup response: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	_, err = client.WaitForCompletion(taskResponse)
	if err != nil {
		err := fmt.Errorf("Error converting VM to template, failed to wait process completion: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	err = downloadBackup(ui, strings.Replace(c.Username, "@pam", "", 1), c.Password, c.proxmoxURL.Hostname(), 22, c.VMID, c.OutputPath)
	if err != nil {
		err := fmt.Errorf("Error converting VM to template, failed to donwload backup: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}
	ui.Say("Deleting LXC Container")
	_, err = client.DeleteVm(vmRef)
	if err != nil {
		ui.Error(fmt.Sprintf("Error deleting VM. Please delete it manually: %s", err))
	}

	return multistep.ActionContinue
}

func (s *stepConvertToTemplate) Cleanup(state multistep.StateBag) {}

func downloadBackup(ui packer.Ui, apiUser string, apiPassword string, apiAddr string, apiPort int, vmId int, dstPath string) error {
	config := &ssh.ClientConfig{
		User: apiUser,
		Auth: []ssh.AuthMethod{
			ssh.Password(apiPassword),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	var sshAddr string = apiAddr+":" + strconv.Itoa(apiPort)
	ui.Say("Establishing SSH connection with ["+apiUser+"] at ["+sshAddr+"] for template file...")
	client, _ := ssh.Dial("tcp",sshAddr , config)
	defer client.Close()

	ui.Say("Establishing SFTP connection for template file...")
	// open an SFTP session over an existing ssh connection.
	ftpClient, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer ftpClient.Close()

	ui.Say("Listing vzdump backup directory for template backup...")
	dir :=  "/var/lib/vz/dump/"
	files, err := ftpClient.ReadDir(dir)

	var srcFilePath = ""
	for _, file := range files {
		match, err := regexp.MatchString(`vzdump-lxc-`+strconv.Itoa(vmId)+`-.*?\.tar\.gz`, file.Name())
		if err == nil && match {
			srcFilePath = path.Join(dir, file.Name())
		}
	}

	if srcFilePath == "" {
		return fmt.Errorf("could not find backup file for LXC container %d", vmId)
	}

	ui.Say("Opening vzdump template backup "+srcFilePath+"...")
	srcFile, err := ftpClient.Open(srcFilePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	ui.Say("Creating local template file "+dstPath+"...")
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	ui.Say("Transferring vzdump template backup file to local path...")
	// write to file
	if  _, err := dstFile.ReadFrom(srcFile); err!= nil {
		return err
	}

	return nil
}
