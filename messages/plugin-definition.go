package messages

import MQTT "github.com/eclipse/paho.mqtt.golang"

type MessageReceiver interface {
	PluginID() string
	Topic() string
	ProcessMessage(msg MQTT.Message) error
	PublishTopic(f func(string, byte, bool, interface{}) MQTT.Token) error
}
