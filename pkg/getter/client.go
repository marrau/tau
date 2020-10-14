package getter

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hashicorp/go-getter"
	"github.com/hashicorp/terraform-svchost/auth"
	"github.com/hashicorp/terraform-svchost/disco"
	"github.com/hashicorp/terraform/command/cliconfig"
	"github.com/hashicorp/terraform/command/format"
	"github.com/hashicorp/terraform/httpclient"
	pluginDiscovery "github.com/hashicorp/terraform/plugin/discovery"
	"github.com/hashicorp/terraform/tfdiags"
	"github.com/hashicorp/terraform/version"
	"github.com/mitchellh/colorstring"

	"github.com/avinor/tau/pkg/helpers/paths"
	"github.com/avinor/tau/pkg/helpers/ui"
)

var (
	USER_AGENT_STRING = fmt.Sprintf("Avinor Tau/%s (+https://github.com/avinor/tau)", version.String())
)

// Options for initialization a new getter client
type Options struct {
	Timeout          time.Duration
	WorkingDirectory string
}

// Client used to download or copy source files with. Support all features
// that go-getter supports, local files, http, git etc.
type Client struct {
	options   *Options
	detectors []getter.Detector
	getters   map[string]getter.Getter
	cliconfig *cliconfig.Config
}

var (
	// defaultTimeout for context to retrieve the sources
	defaultTimeout = 10 * time.Second
)

// New creates a new getter client. It configures all the detectors and getters itself to make
// sure they are configured correctly.
func New(options *Options) *Client {
	if options == nil {
		options = &Options{}
	}

	if options.WorkingDirectory == "" {
		options.WorkingDirectory = paths.WorkingDir
	}

	if options.Timeout == 0 {
		options.Timeout = defaultTimeout
	}

	cliconfig, diagnostics := cliconfig.LoadConfig()
	if diagnostics.HasErrors() {
		diagnostics.ErrWithWarnings()
	}

	httpClient := httpclient.New()
	httpClient.Timeout = options.Timeout

	registryDetector := &RegistryDetector{
		httpClient: httpClient,
		disco:      newDisco(cliconfig),
	}

	detectors := []getter.Detector{
		registryDetector,
		new(getter.GitHubDetector),
		new(getter.GitDetector),
		new(getter.BitBucketDetector),
		new(getter.S3Detector),
		new(getter.GCSDetector),
		new(getter.FileDetector),
	}

	httpGetter := &getter.HttpGetter{
		Netrc: true,
	}

	getters := map[string]getter.Getter{
		"file":  &LocalGetter{FileGetter: getter.FileGetter{Copy: true}},
		"git":   new(getter.GitGetter),
		"gcs":   new(getter.GCSGetter),
		"hg":    new(getter.HgGetter),
		"s3":    new(getter.S3Getter),
		"http":  httpGetter,
		"https": httpGetter,
	}

	return &Client{
		options:   options,
		detectors: detectors,
		getters:   getters,
		cliconfig: cliconfig,
	}
}

// Clone creates a clone of the client with an alternative new working directory.
// If workingDir is set to "" it will just reuse same directory in client
func (c *Client) Clone(workingDir string) *Client {
	if workingDir == "" {
		workingDir = c.options.WorkingDirectory
	}

	return &Client{
		options: &Options{
			WorkingDirectory: workingDir,
			Timeout:          c.options.Timeout,
		},
		detectors: c.detectors,
		getters:   c.getters,
	}
}

// Get retrieves sources from src and load them into dst folder. If version is set it will try to
// download from terraform registry. Set to nil to disable this feature.
func (c *Client) Get(src, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.Timeout)
	defer cancel()

	client := &getter.Client{
		Ctx:       ctx,
		Src:       src,
		Dst:       dst,
		Pwd:       c.options.WorkingDirectory,
		Mode:      getter.ClientModeAny,
		Detectors: c.detectors,
		Getters:   c.getters,
	}

	return client.Get()
}

// GetFile retrieves a single file from src destination. It implements almost same
// functionallity that Get function, but does only allow a single file to be downloaded.
// Since terraform registry does not supply single files it cannot download any
// single files from terraform registry.
func (c *Client) GetFile(src, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.Timeout)
	defer cancel()

	client := &getter.Client{
		Ctx:       ctx,
		Src:       src,
		Dst:       dst,
		Pwd:       c.options.WorkingDirectory,
		Mode:      getter.ClientModeFile,
		Detectors: c.detectors,
		Getters:   c.getters,
	}

	return client.Get()
}

// Detect is a wrapper on go-getter detect and will return a new source string
// that is the parsed url using correct getter
func (c *Client) Detect(src string) (string, error) {
	return getter.Detect(src, c.options.WorkingDirectory, c.detectors)
}

func handleDiags(diags tfdiags.Diagnostics) {
	if len(diags) > 0 {
		// Since we haven't instantiated a command.Meta yet, we need to do
		// some things manually here and use some "safe" defaults for things
		// that command.Meta could otherwise figure out in smarter ways.
		ui.Error("There are some problems with the CLI configuration:")
		for _, diag := range diags {
			earlyColor := &colorstring.Colorize{
				Colors:  colorstring.DefaultColors,
				Disable: true, // Disable color to be conservative until we know better
				Reset:   true,
			}
			// We don't currently have access to the source code cache for
			// the parser used to load the CLI config, so we can't show
			// source code snippets in early diagnostics.
			ui.Error(format.Diagnostic(diag, nil, earlyColor, 78))
		}
		if diags.HasErrors() {
			ui.Error("As a result of the above problems, Terraform may not behave as intended.\n\n")
			// We continue to run anyway, since Terraform has reasonable defaults.
		}
	}
}

func newDisco(config *cliconfig.Config) *disco.Disco {
	var services *disco.Disco
	credsSrc, err := credentialsSource(config)
	if err == nil {
		services = disco.NewWithCredentialsSource(credsSrc)
	} else {
		// Most commands don't actually need credentials, and most situations
		// that would get us here would already have been reported by the config
		// loading above, so we'll just log this one as an aid to debugging
		// in the unlikely event that it _does_ arise.
		log.Printf("[WARN] Cannot initialize remote host credentials manager: %s", err)
		// passing (untyped) nil as the creds source is okay because the disco
		// object checks that and just acts as though no credentials are present.
		services = disco.NewWithCredentialsSource(nil)
	}
	services.SetUserAgent(USER_AGENT_STRING)
	return services
}

func credentialsSource(config *cliconfig.Config) (auth.CredentialsSource, error) {
	helperPlugins := pluginDiscovery.FindPlugins("credentials", globalPluginDirs())
	return config.CredentialsSource(helperPlugins)
}

// globalPluginDirs returns directories that should be searched for
// globally-installed plugins (not specific to the current configuration).
//
// Earlier entries in this slice get priority over later when multiple copies
// of the same plugin version are found, but newer versions always override
// older versions where both satisfy the provider version constraints.
func globalPluginDirs() []string {
	var ret []string
	// Look in ~/.terraform.d/plugins/ , or its equivalent on non-UNIX
	dir, err := cliconfig.ConfigDir()
	if err != nil {
		log.Printf("[ERROR] Error finding global config directory: %s", err)
	} else {
		machineDir := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
		ret = append(ret, filepath.Join(dir, "plugins"))
		ret = append(ret, filepath.Join(dir, "plugins", machineDir))
	}

	return ret
}
