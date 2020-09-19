//go:generate mapstructure-to-hcl2 -type Config,nicConfig,diskConfig,vgaConfig,storageConfig

package proxmox_lxc

import (
	"errors"
	"fmt"
	"github.com/hashicorp/packer/common"
	"github.com/hashicorp/packer/common/bootcommand"
	"github.com/hashicorp/packer/helper/communicator"
	"github.com/hashicorp/packer/helper/config"
	"github.com/hashicorp/packer/packer"
	"github.com/hashicorp/packer/template/interpolate"
	"github.com/mitchellh/mapstructure"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	common.HTTPConfig      `mapstructure:",squash"`
	bootcommand.BootConfig `mapstructure:",squash"`
	Comm                   communicator.Config `mapstructure:",squash"`
	BootKeyInterval        time.Duration       `mapstructure:"boot_key_interval"`

	ProxmoxURLRaw      string `mapstructure:"proxmox_url"`
	proxmoxURL         *url.URL
	SkipCertValidation bool   `mapstructure:"insecure_skip_tls_verify"`
	Username           string `mapstructure:"username"`
	Password           string `mapstructure:"password"`
	Node               string `mapstructure:"node"`
	Pool               string `mapstructure:"pool"`

	Memory         int          `mapstructure:"memory"`
	Cores          int          `mapstructure:"cores"`
	TemplateFile        string       `mapstructure:"template_file"`
	TemplateStoragePool string       `mapstructure:"template_storage_pool"`
	Agent          bool         `mapstructure:"qemu_agent"`
	Onboot         bool         `mapstructure:"onboot"`
	FSStorage         string         `mapstructure:"filesystem_storage"`
	FSSize         int         `mapstructure:"filesystem_size"`
	VMID         int         `mapstructure:"vmid"`

	VMInterface        string          `mapstructure:"vm_interface"`
	OutputPath        string          `mapstructure:"output_path"`
	ProvisionIP        string          `mapstructure:"provision_ip"`
	ProvisionMac       string          `mapstructure:"provision_mac"`
	ProvisionPort        int          `mapstructure:"provision_port"`
	SSHPublicKeyPath        string          `mapstructure:"ssh_public_key_file"`

	ctx interpolate.Context
}

func (c *Config) Prepare(raws ...interface{}) ([]string, error) {
	// Agent defaults to true
	c.Agent = true

	var md mapstructure.Metadata
	err := config.Decode(c, &config.DecodeOpts{
		Metadata:           &md,
		Interpolate:        true,
		InterpolateContext: &c.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{
				"boot_command",
			},
		},
	}, raws...)
	if err != nil {
		return nil, err
	}

	var errs *packer.MultiError
	// Defaults
	if c.ProxmoxURLRaw == "" {
		c.ProxmoxURLRaw = os.Getenv("PROXMOX_URL")
	}
	if c.Username == "" {
		c.Username = os.Getenv("PROXMOX_USERNAME")
	}
	if c.Password == "" {
		c.Password = os.Getenv("PROXMOX_PASSWORD")
	}

	if c.Memory < 16 {
		log.Printf("Memory %d is too small, using default: 512", c.Memory)
		c.Memory = 512
	}
	if c.Cores < 1 {
		log.Printf("Number of cores %d is too small, using default: 1", c.Cores)
		c.Cores = 1
	}

	if c.ProvisionPort <= 0 {
		c.ProvisionPort = 22
	}

	if c.ProvisionMac == "" {
		c.ProvisionMac = "1e:eb:08:d1:e7:e2"
	}

	errs = packer.MultiErrorAppend(errs, c.Comm.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.BootConfig.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.HTTPConfig.Prepare(&c.ctx)...)

	// Required configurations that will display errors if not set
	if c.Username == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("username must be specified"))
	}
	if c.Password == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("password must be specified"))
	}
	if c.ProxmoxURLRaw == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("proxmox_url must be specified"))
	}
	if c.proxmoxURL, err = url.Parse(c.ProxmoxURLRaw); err != nil {
		errs = packer.MultiErrorAppend(errs, fmt.Errorf("Could not parse proxmox_url: %s", err))
	}
	if c.Node == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("node must be specified"))
	}
	if strings.ContainsAny(c.TemplateFile, " ") {
		errs = packer.MultiErrorAppend(errs, errors.New("template_name must not contain spaces"))
	}
	if c.FSStorage == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("filesystem_storage must be specified"))
	}
	if c.FSSize <= 0 {
		errs = packer.MultiErrorAppend(errs, errors.New("filesystem_size must be specified"))
	}

	if c.ProvisionIP == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("provision_ip must be specified"))
	}

	if c.TemplateStoragePool == "" {
		c.TemplateStoragePool = "local"
	}
	
	if errs != nil && len(errs.Errors) > 0 {
		return nil, errs
	}

	packer.LogSecretFilter.Set(c.Password)
	return nil, nil
}

func contains(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}
