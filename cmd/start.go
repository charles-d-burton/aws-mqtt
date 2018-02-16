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
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
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
	Restart()
	ProcessMessages()
	NewTlsConfig() *tls.Config
}

type ServerConnection struct {
	Host     string
	Port     int
	Path     string
	Topic    string
	Qos      int
	Messages chan MQTT.Message
	Control  chan os.Signal
}

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("start called")
		var serverConnection ServerConnection
		//cer, err := tls.LoadX509KeyPair(viper.GetString("cert"), viper.GetString("key"))

		viper.OnConfigChange(func(e fsnotify.Event) {
			fmt.Println("Config file changed:", e.Name)
			configChange(e, &serverConnection)
		})
		serverConnection.Host = viper.GetString("mqtt-config.host")
		serverConnection.Port = viper.GetInt("mqtt-config.port")
		serverConnection.Path = viper.GetString("mqtt-config.path")
		serverConnection.Qos = viper.GetInt("mqtt-config.qos")
		serverConnection.Topic = viper.GetString("mqtt-config.topic")
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

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// startCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// startCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func (server *ServerConnection) Start() error {
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
	server.ProcessMessages()
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

func (server *ServerConnection) Connect(connOpts *MQTT.ClientOptions) (MQTT.Client, error) {
	/* connOpts.SetDefaultPublishHandler(func(client MQTT.Client, msg MQTT.Message) {
		server.Messages <- msg
	}) */
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

func (server *ServerConnection) Subscribe(client MQTT.Client) error {
	// Subscribe

	log.Printf("subscribing")
	if client.IsConnected() {
		token := client.Subscribe(server.Topic, byte(server.Qos), func(client MQTT.Client, msg MQTT.Message) {
			server.Messages <- msg
		})
		token.WaitTimeout(30 * time.Second)
		token.Wait()
		log.Printf("subscribed")
		if token.Error() != nil {
			fmt.Println(token.Error())
		}
		return token.Error()
	}
	return fmt.Errorf("Client is not connected!")
}

func (server *ServerConnection) Restart() {

}

type message struct {
	KeepAlive   string `json:"keepalive"`
	ButtonPress string `json:"buttonpress"`
}

func (server *ServerConnection) ProcessMessages() {
	for msg := range server.Messages {
		fmt.Println("Message received: ", msg.Topic(), string(msg.Payload()))
		f, err := os.Open("nightmarecat.mp3")
		if err != nil {
			fmt.Errorf("Error opening audio file: %v", err)
		}
		sound, format, _ := mp3.Decode(f)
		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		speaker.Play(sound)
	}
}

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
