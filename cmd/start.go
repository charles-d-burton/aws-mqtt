// Copyright © 2018 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"plugin"
	"time"

	"github.com/charles-d-burton/aws-mqtt/messages"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

//Server interface to control the server
type Server interface {
	Start() error
	Connect(connOpts *MQTT.ClientOptions) (MQTT.Client, error)
	Subscribe(client MQTT.Client) error
	LoadPlugins() error
	Restart()
	ProcessMessages()
	NewTlsConfig() *tls.Config
}

type ServerConnection struct {
	Host      string
	Port      int
	Path      string
	Qos       int
	Receivers []messages.MessageReceiver
	Messages  chan MQTT.Message
	Control   chan os.Signal
}

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start processing MQTT messages",
	Long: `Start processing message, this loads all plugins and starts
	sendind messages to the topics each plugin subscribes to.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("start called")
		var serverConnection ServerConnection

		viper.OnConfigChange(func(e fsnotify.Event) {
			fmt.Println("Config file changed:", e.Name)
			configChange(e, &serverConnection)
		})
		serverConnection.Host = viper.GetString("mqtt-config.host")
		serverConnection.Port = viper.GetInt("mqtt-config.port")
		serverConnection.Path = viper.GetString("mqtt-config.path")
		serverConnection.Qos = viper.GetInt("mqtt-config.qos")
		serverConnection.Messages = make(chan MQTT.Message, 200)
		serverConnection.Control = make(chan os.Signal, 1)

		err := serverConnection.Start()
		if err != nil {
			fmt.Errorf("Error: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

//Start main entry point to the processor.
func (server *ServerConnection) Start() error {
	err := server.LoadPlugins()
	if err != nil {
		return err
	}
	cid := uuid.New().String()
	connOpts := MQTT.NewClientOptions()
	connOpts.SetClientID(cid)
	connOpts.SetCleanSession(true)
	connOpts.SetAutoReconnect(true)
	connOpts.SetMaxReconnectInterval(1 * time.Second)
	connOpts.SetTLSConfig(server.NewTlsConfig())
	mqttClient, err := server.Connect(connOpts)
	if err != nil {
		return err
	}
	err = server.Subscribe(mqttClient)
	if err != nil {
		return err
	}
	go server.ProcessMessages()
	log.Println("[MQTT] Connected")
	quit := make(chan struct{})
	signal.Notify(server.Control, os.Interrupt)
	go func() {
		<-server.Control
		mqttClient.Disconnect(250)
		fmt.Println("[MQTT] Disconnect")
		quit <- struct{}{}
	}()
	<-quit

	return nil

}

//Connect Connect to the remote AWS IoT or TLS secured MQTT host
func (server *ServerConnection) Connect(connOpts *MQTT.ClientOptions) (MQTT.Client, error) {
	brokerURL := fmt.Sprintf("tcps://%s:%d%s", server.Host, server.Port, server.Path)
	connOpts.AddBroker(brokerURL)
	mqttClient := MQTT.NewClient(connOpts)
	token := mqttClient.Connect()
	token.WaitTimeout(30 * time.Second)
	token.Wait()
	if token.Error() != nil {
		fmt.Println(token.Error())
	}
	return mqttClient, token.Error()
}

//Subscribe Iterate through the configured plugins and subscribe to their topics, publish messages to the channel that handles them
func (server *ServerConnection) Subscribe(client MQTT.Client) error {
	// Subscribe
	if client.IsConnected() {
		for _, receiver := range server.Receivers {
			receiver.PublishTopic(client.Publish)
			topic := receiver.Topic()
			log.Printf("subscribing: ", topic)
			token := client.Subscribe(topic, byte(server.Qos), func(client MQTT.Client, msg MQTT.Message) {
				server.Messages <- msg
				//receiver.ProcessMessage(msg)
			})

			token.WaitTimeout(30 * time.Second)
			token.Wait()
			log.Printf("subscribed")
			if token.Error() != nil {
				fmt.Println(token.Error())
			}
		}
	}
	return nil
}

//Restart Stub to implement automated server restart logic on config change or when a new plugin is dropped in
func (server *ServerConnection) Restart() {

}

func (server *ServerConnection) ProcessMessages() {
	for msg := range server.Messages {
		for _, receiver := range server.Receivers {
			if receiver.Topic() == msg.Topic() {
				receiver.ProcessMessage(msg)
			}
		}
		log.Println(msg.MessageID())
	}
}

//LoadPlugins search the configured directory for plugins and load them into a slice
func (server *ServerConnection) LoadPlugins() error {
	log.Println("Loading Plugins")
	pluginList := []string{}
	err := filepath.Walk(viper.GetString("plugin-dir"), func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() {
			pluginList = append(pluginList, path)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, pFile := range pluginList {
		p, err := plugin.Open(pFile)
		if err != nil {
			log.Println(err)
		} else {
			getReceiver, err := p.Lookup("GetPlugin")
			if err != nil {
				return err
			}
			receiver, err := getReceiver.(func() (messages.MessageReceiver, error))()
			if err != nil {
				return err
			}
			server.Receivers = append(server.Receivers, receiver)
		}
	}
	if len(server.Receivers) == 0 {
		return fmt.Errorf("No plugins found!")
	}
	return nil

}

//NewTlsConfig Load in the certificates and setup the TLS configurations and certs
func (server *ServerConnection) NewTlsConfig() *tls.Config {
	// Import trusted certificates from CAfile.pem.
	// Alternatively, manually add CA certificates to
	// default openssl CA bundle.
	certpool := x509.NewCertPool()
	pemCerts, err := ioutil.ReadFile(viper.GetString("ca"))
	if err == nil {
		certpool.AppendCertsFromPEM(pemCerts)
	}

	// Import client certificate/key pair
	cert, err := tls.LoadX509KeyPair(viper.GetString("cert"), viper.GetString("key"))
	if err != nil {
		fmt.Println("Cert name: ", viper.GetString("cert"))
		fmt.Println("Key name: ", viper.GetString("key"))
		fmt.Println("Could not load X509 Key pair")
		return nil
	}

	// Just to print out the client certificate..
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		panic(err)
	}
	//log.Println(cert.Leaf)

	// Create tls.Config with desired tls properties
	return &tls.Config{
		// RootCAs = certs used to verify server cert.
		RootCAs: certpool,
		// ClientAuth = whether to request cert from server.
		// Since the server is set up for SSL, this happens
		// anyways.
		ClientAuth: tls.NoClientCert,
		// ClientCAs = certs used to validate client cert.
		ClientCAs: nil,
		// InsecureSkipVerify = verify that cert contents
		// match server. IP matches what is in cert etc.
		InsecureSkipVerify: true,
		// Certificates = list of certs client sends to server.
		Certificates: []tls.Certificate{cert},
	}
}
