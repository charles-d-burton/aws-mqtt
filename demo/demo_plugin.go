package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/charles-d-burton/aws-mqtt/messages"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
)

type mqttPlugin struct{}

func (plugin mqttPlugin) PluginID() string {
	return "nightmare-doorbell"
}

func (plugin mqttPlugin) Topic() string {
	return "/nightmare-cat/button_pressed"
}

func (plugin mqttPlugin) ProcessMessage(msg MQTT.Message) error {

	fmt.Println("Message received: ", msg.Topic(), string(msg.Payload()))
	f, err := os.Open("nightmarecat.mp3")
	defer f.Close()
	if err != nil {
		fmt.Errorf("Error opening audio file: %v", err)
	}
	sound, format, _ := mp3.Decode(f)
	err = speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	if err != nil {
		log.Println(err)
		return err
	}
	done := make(chan struct{})
	speaker.Play(beep.Seq(sound, beep.Callback(func() {
		close(done)
	})))
	<-done
	//speaker.Play(sound)
	return nil

}

func GetPlugin() (messages.MessageReceiver, error) {
	receiver := mqttPlugin{}
	return receiver, nil
}
