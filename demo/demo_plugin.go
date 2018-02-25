package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/charles-d-burton/aws-mqtt/messages"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
)

type mqttPlugin struct {
	PubTopic   func(topic string, qos byte, retained bool, payload interface{}) MQTT.Token
	AudioBytes []byte
	Stream     beep.StreamCloser
}

func (plugin mqttPlugin) PluginID() string {
	return "nightmare-doorbell"
}

func (plugin mqttPlugin) Topic() string {
	return "/nightmare-cat/button_pressed"
}

//Example of how to publish a message with passed in publish topic
func (plugin mqttPlugin) PublishTopic(f func(string, byte, bool, interface{}) MQTT.Token) error {
	plugin.PubTopic = f
	if token := f(plugin.Topic(), 0, false, "mymessage"); token.Wait() && token.Error() != nil {
		log.Println(token.Error())
	}
	log.Println("Published messaged!")
	return nil
}

//ProcessMessage function assigned to MQTT callback to allow processing of incoming messages
func (plugin mqttPlugin) ProcessMessage(msg MQTT.Message) error {

	fmt.Println("Message received: ", msg.Topic(), string(msg.Payload()))

	done := make(chan struct{})
	speaker.Play(beep.Seq(plugin.Stream, beep.Callback(func() {
		plugin.Stream.Close()
		close(done)
	})))
	<-done
	go setupAudio(&plugin)
	//speaker.Play(sound)
	return nil

}

func GetPlugin() (messages.MessageReceiver, error) {
	receiver := mqttPlugin{}
	f, err := os.Open("nightmarecat.mp3")
	defer f.Close()
	contents, _ := ioutil.ReadAll(f)
	receiver.AudioBytes = contents
	if err != nil {
		return nil, fmt.Errorf("Error opening audio file: %v", err)

	}
	go setupAudio(&receiver)
	return receiver, nil
}

func setupAudio(plugin *mqttPlugin) error {

	sound, format, _ := mp3.Decode(ioutil.NopCloser(bytes.NewReader(plugin.AudioBytes)))
	err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	if err != nil {
		log.Println(err)
		return err
	}
	plugin.Stream = sound
	return nil
}
