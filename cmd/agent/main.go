package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/anfernee/proxy-service/pkg/agent/agentclient"
	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"io/ioutil"
	"os"
	"github.com/spf13/cobra"
)

func main() {
	agent := &Agent{}
	o := newGrpcProxyAgentOptions()
	command := newAgentCommand(agent, o)
	flags := command.Flags()
	flags.AddFlagSet(o.Flags())
	if err := command.Execute(); err != nil {
		glog.Errorf( "error: %v\n", err)
		os.Exit(1)
	}
}

type GrpcProxyAgentOptions struct {
	agentCert string
	agentKey  string
	caCert     string
}

func (o *GrpcProxyAgentOptions) Flags() *pflag.FlagSet {
	flags := pflag.NewFlagSet("proxy", pflag.ContinueOnError)
	flags.StringVar(&o.agentCert, "agentCert", o.agentCert, "If non-empty secure communication with this cert.")
	flags.StringVar(&o.agentKey, "agentKey", o.agentKey, "If non-empty secure communication with this key.")
	flags.StringVar(&o.caCert, "caCert", o.caCert, "If non-empty the CAs we use to validate clients.")
	return flags
}

func (o *GrpcProxyAgentOptions) Print() {
	glog.Warningf("AgentCert set to \"%s\".\n", o.agentCert)
	glog.Warningf("AgentKey set to \"%s\".\n", o.agentKey)
	glog.Warningf("CACert set to \"%s\".\n", o.caCert)
}

func (o *GrpcProxyAgentOptions) Validate() error {
	if o.agentKey != "" {
		if _, err := os.Stat(o.agentKey); os.IsNotExist(err) {
			return err
		}
		if o.agentCert == "" {
			return fmt.Errorf("cannot have agent cert empty when agent key is set to \"%s\"", o.agentKey)
		}
	}
	if o.agentCert != "" {
		if _, err := os.Stat(o.agentCert); os.IsNotExist(err) {
			return err
		}
		if o.agentKey == "" {
			return fmt.Errorf("cannot have agent key empty when agent cert is set to \"%s\"", o.agentCert)
		}
	}
	if o.caCert != "" {
		if _, err := os.Stat(o.caCert); os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func newGrpcProxyAgentOptions() *GrpcProxyAgentOptions {
	o := GrpcProxyAgentOptions{
		agentCert: "",
		agentKey:  "",
		caCert:    "",
	}
	return &o
}

func newAgentCommand(a *Agent, o *GrpcProxyAgentOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:  "agent",
		Long: `A gRPC agent, Connects to the proxy and then allows traffic to be forwarded to it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.run(o)
		},
	}

	return cmd
}

type Agent struct {

}

func (a *Agent) run(o *GrpcProxyAgentOptions) error {
	o.Print()
	if err := o.Validate(); err != nil {
		return err
	}

	agentCert, err := tls.LoadX509KeyPair(o.agentCert, o.agentKey)
	if err != nil {
		return err
	}
	certPool := x509.NewCertPool()
	caCert, err := ioutil.ReadFile(o.caCert)
	if err != nil {
		return err
	}
	ok := certPool.AppendCertsFromPEM(caCert)
	if !ok {
		return fmt.Errorf("failed to append CA cert to the cert pool")
	}

	transportCreds := credentials.NewTLS(&tls.Config{
		ServerName:   "127.0.0.1",
		Certificates: []tls.Certificate{agentCert},
		RootCAs:      certPool,
	})
	dialOption := grpc.WithTransportCredentials(transportCreds)
	client := agentclient.NewAgentClient("localhost:8091")

	if err := client.Connect(dialOption); err != nil {
		return err
	}

	stopCh := make(chan struct{})

	client.Serve(stopCh)
	return nil
}
