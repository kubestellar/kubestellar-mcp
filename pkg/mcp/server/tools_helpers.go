package server

import (
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func (s *Server) getDynamicClientForCluster(clusterName string) (dynamic.Interface, error) {
	if s.dynamicClientFactory != nil {
		return s.dynamicClientFactory(clusterName)
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if clusterName != "" {
		configOverrides.CurrentContext = clusterName
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, configOverrides).ClientConfig()
	if err != nil {
		return nil, err
	}

	return dynamic.NewForConfig(config)
}

func (s *Server) getRestConfigForCluster(clusterName string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	}

	configOverrides := &clientcmd.ConfigOverrides{}
	if clusterName != "" {
		configOverrides.CurrentContext = clusterName
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, configOverrides).ClientConfig()
}

