//go:generate mapstructure-to-hcl2 -type Config,nicConfig,diskConfig,vgaConfig,storageConfig

package proxmox_lxc

import (
	"errors"
	"fmt"
	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
	"github.com/mitchellh/mapstructure"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	commonsteps.HTTPConfig `mapstructure:",squash"`
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

	Memory              int    `mapstructure:"memory"`
	Cores               int    `mapstructure:"cores"`
	Unprivileged        bool   `mapstructure:"unprivileged"`
	TemplateFile        string `mapstructure:"template_file"`
	TemplateStoragePool string `mapstructure:"template_storage_pool"`
	FSStorage           string `mapstructure:"filesystem_storage"`
	FSSize              int    `mapstructure:"filesystem_size"`
	VMID                int    `mapstructure:"vmid"`

	OutputPath              string `mapstructure:"output_path"`
	ProvisionIP             string `mapstructure:"provision_ip"`
	ProvisionMac            string `mapstructure:"provision_mac"`
	ProvisionPort           int    `mapstructure:"provision_port"`
	ProvisionPublicKeyPath  string `mapstructure:"provision_public_key_file"`
	ProvisionPrivateKeyPath string `mapstructure:"provision_private_key_file"`
	ProvisionPassword       string `mapstructure:"provision_password"`

	ctx interpolate.Context
}

func (c *Config) Prepare(raws ...interface{}) ([]string, error) {
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

	if c.ProvisionPassword == "" {
		c.ProvisionPassword = "provision"
	}

	if c.TemplateStoragePool == "" {
		c.TemplateStoragePool = "local"
	}

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

	if c.ProvisionPublicKeyPath == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("provision_public_key_file must be specified"))
	}

	if c.ProvisionPrivateKeyPath == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("provision_private_key_file must be specified"))
	}

	if c.OutputPath == "" {
		errs = packer.MultiErrorAppend(errs, errors.New("output_path must be specified"))
	}

	// Set internal values
	//c.Comm.SSHAgentAuth = true
	c.Comm.SSHPrivateKeyFile = c.ProvisionPrivateKeyPath
	c.Comm.SSHHost = c.ProvisionIP
	c.Comm.SSHPort = c.ProvisionPort
	c.Comm.SSHUsername = "root"
	
	//c.Comm.SSHPassword = c.ProvisionPassword

	errs = packer.MultiErrorAppend(errs, c.Comm.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.BootConfig.Prepare(&c.ctx)...)
	errs = packer.MultiErrorAppend(errs, c.HTTPConfig.Prepare(&c.ctx)...)

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
