/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/test-infra/pkg/flagutil"
	"k8s.io/test-infra/prow/config/secret"
	prowflagutil "k8s.io/test-infra/prow/flagutil"
	"k8s.io/test-infra/prow/plugins"

	"sigs.k8s.io/verify-conformance/internal/plugin"
)

type options struct {
	port int

	pluginConfig string
	dryRun       bool
	github       prowflagutil.GitHubOptions

	updatePeriod time.Duration

	webhookSecretFile string
}

func (o *options) Validate() error {
	for _, group := range []flagutil.OptionGroup{&o.github} {
		if err := group.Validate(o.dryRun); err != nil {
			return err
		}
	}

	return nil
}

func gatherOptions() options {
	o := options{}
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.IntVar(&o.port, "port", 8888, "Port to listen on.")
	fs.StringVar(&o.pluginConfig, "plugin-config", "/etc/plugins/plugins.yaml", "Path to plugin config file.")
	fs.BoolVar(&o.dryRun, "dry-run", true, "Dry run for testing. Uses API tokens but does not mutate.")
	fs.DurationVar(&o.updatePeriod, "update-period", time.Hour*24, "Period duration for periodic scans of all PRs.")
	fs.StringVar(&o.webhookSecretFile, "hmac-secret-file", "/etc/webhook/hmac", "Path to the file containing the GitHub HMAC secret.")

	for _, group := range []flagutil.OptionGroup{&o.github} {
		group.AddFlags(fs)
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		logrus.WithError(err).Fatal("error parsing args[1:]")
	}
	return o
}

func main() {
	o := gatherOptions()
	if err := o.Validate(); err != nil {
		logrus.Fatalf("Invalid options: %v", err)
	}

	logrus.SetFormatter(&logrus.JSONFormatter{})
	logrus.SetLevel(logrus.InfoLevel)
	log := logrus.StandardLogger().WithField("plugin", "verify-conformance")

	secrets := []string{}
	if o.github.TokenPath != "" {
		secrets = append(secrets, o.github.TokenPath)
	}
	if o.github.AppPrivateKeyPath != "" {
		secrets = append(secrets, o.github.AppPrivateKeyPath)
	}
	if o.webhookSecretFile != "" {
		secrets = append(secrets, o.webhookSecretFile)
	}
	if err := secret.Add(secrets...); err != nil {
		logrus.WithError(err).Fatal("Error starting test-infra/prow/config/secret agent.")
	}

	pa := &plugins.ConfigAgent{}
	if err := pa.Start(o.pluginConfig, []string{}, "", true, false); err != nil {
		log.WithError(err).Fatalf("Error loading plugin config from %q.", o.pluginConfig)
	}

	githubClient, err := o.github.GitHubClient(o.dryRun)
	if err != nil {
		logrus.WithError(err).Fatal("Error getting GitHub client.")
	}
	if err := githubClient.Throttle(360, 360); err != nil {
		logrus.WithError(err).Fatal("error: throttling GitHub client")
	}
	if err := plugin.HandleAll(log, githubClient, pa.Config()); err != nil {
		log.WithError(err).Error("Error during periodic update of all PRs.")
	}
}
