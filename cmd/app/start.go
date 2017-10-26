package app

import (
	"fmt"
	"io"
	"time"

	localprovider "github.com/cosineyan/metrics-apiserver/pkg/provider"
	"github.com/spf13/cobra"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/cmd/server"
	"github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/dynamicmapper"
)

func NewCommandStartSampleAdapterServer(out, errOut io.Writer, stopCh <-chan struct{}) *cobra.Command {
	baseOpts := server.NewCustomMetricsAdapterServerOptions(out, errOut)
	o := SampleAdapterServerOptions{
		CustomMetricsAdapterServerOptions: baseOpts,
		DiscoveryInterval:                 10 * time.Minute,
	}

	cmd := &cobra.Command{
		Short: "Launch the custom metrics API adapter server",
		Long:  "Launch the custom metrics API adapter server",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				return err
			}
			if err := o.RunCustomMetricsAdapterServer(stopCh); err != nil {
				return err
			}
			return nil
		},
	}

	flags := cmd.Flags()
	o.SecureServing.AddFlags(flags)
	o.Authentication.AddFlags(flags)
	o.Authorization.AddFlags(flags)
	o.Features.AddFlags(flags)

	flags.StringVar(&o.RemoteKubeConfigFile, "lister-kubeconfig", o.RemoteKubeConfigFile, ""+
		"kubeconfig file pointing at the 'core' kubernetes server with enough rights to list "+
		"any described objets")
	flags.DurationVar(&o.DiscoveryInterval, "discovery-interval", o.DiscoveryInterval, ""+
		"interval at which to refresh API discovery information")

	return cmd
}

func (o SampleAdapterServerOptions) RunCustomMetricsAdapterServer(stopCh <-chan struct{}) error {
	config, err := o.Config()
	if err != nil {
		return err
	}

	var clientConfig *rest.Config
	if len(o.RemoteKubeConfigFile) > 0 {
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: o.RemoteKubeConfigFile}
		loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

		clientConfig, err = loader.ClientConfig()
	} else {
		clientConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return fmt.Errorf("unable to construct lister client config to initialize provider: %v", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(clientConfig)
	if err != nil {
		return fmt.Errorf("unable to construct discovery client for dynamic client: %v", err)
	}

	// NB: since we never actually look at the contents of
	// the objects we fetch (beyond ObjectMeta), unstructured should be fine
	dynamicMapper, err := dynamicmapper.NewRESTMapper(discoveryClient, apimeta.InterfacesForUnstructured, o.DiscoveryInterval)
	if err != nil {
		return fmt.Errorf("unable to construct dynamic discovery mapper: %v", err)
	}

	clientPool := dynamic.NewClientPool(clientConfig, dynamicMapper, dynamic.LegacyAPIPathResolverFunc)
	if err != nil {
		return fmt.Errorf("unable to construct lister client to initialize provider: %v", err)
	}

	cmProvider := localprovider.NewMemoryProvider(clientPool, dynamicMapper)

	server, err := config.Complete().New(cmProvider)
	if err != nil {
		return err
	}
	return server.GenericAPIServer.PrepareRun().Run(stopCh)
}

type SampleAdapterServerOptions struct {
	*server.CustomMetricsAdapterServerOptions

	// RemoteKubeConfigFile is the config used to list pods from the master API server
	RemoteKubeConfigFile string
	// DiscoveryInterval is the interval at which discovery information is refreshed
	DiscoveryInterval time.Duration
}
