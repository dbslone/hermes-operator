package main

import (
	"context"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"hermes-operator/internal/agent"
	"hermes-operator/internal/server"
)

func main() {
	var httpAddr, metricsAddr, probeAddr, watchNS, agentNS, defaultsFile string
	flag.StringVar(&httpAddr, "http-addr", ":8090", "address for the UI, REST API and MCP endpoint")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "metrics address")
	flag.StringVar(&probeAddr, "probe-addr", ":8081", "health probe address")
	flag.StringVar(&watchNS, "watch-namespace", "", "namespace to watch; empty watches all namespaces")
	flag.StringVar(&agentNS, "agent-namespace", "", "namespace where the UI/MCP create agents (required)")
	flag.StringVar(&defaultsFile, "defaults-file", "", "YAML file with agent defaults (image, resources, storage)")
	flag.Parse()

	ctrl.SetLogger(zap.New())
	log := ctrl.Log.WithName("setup")
	if agentNS == "" {
		log.Error(nil, "--agent-namespace is required")
		os.Exit(1)
	}

	defaults, err := agent.LoadDefaults(defaultsFile)
	if err != nil {
		log.Error(err, "reading defaults file")
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)

	opts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
	}
	if watchNS != "" {
		opts.Cache = cache.Options{DefaultNamespaces: map[string]cache.Config{watchNS: {}}}
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), opts)
	if err != nil {
		log.Error(err, "creating manager")
		os.Exit(1)
	}
	if err := (&agent.Reconciler{Client: mgr.GetClient(), Defaults: defaults}).SetupWithManager(mgr); err != nil {
		log.Error(err, "setting up controller")
		os.Exit(1)
	}
	_ = mgr.AddHealthzCheck("healthz", healthz.Ping)
	_ = mgr.AddReadyzCheck("readyz", healthz.Ping)

	svc := &agent.Service{Client: mgr.GetClient(), Namespace: agentNS, ListNamespace: watchNS}
	_ = mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		return server.Run(ctx, httpAddr, svc)
	}))

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager exited")
		os.Exit(1)
	}
}
