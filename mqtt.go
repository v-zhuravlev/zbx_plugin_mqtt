/*
** Zabbix
** Copyright (C) 2001-2019 Zabbix SIA
**
** This program is free software; you can redistribute it and/or modify
** it under the terms of the GNU General Public License as published by
** the Free Software Foundation; either version 2 of the License, or
** (at your option) any later version.
**
** This program is distributed in the hope that it will be useful,
** but WITHOUT ANY WARRANTY; without even the implied warranty of
** MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
** GNU General Public License for more details.
**
** You should have received a copy of the GNU General Public License
** along with this program; if not, write to the Free Software
** Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.
**/

package mqtt

import (
	"crypto/tls"
	"fmt"
	"time"
	"zabbix.com/pkg/itemutil"
	"zabbix.com/pkg/plugin"
	"zabbix.com/pkg/watch"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// Plugin
type Plugin struct {
	plugin.Base
	manager         *watch.Manager
	mqttSubs        map[string]*mqttSub
	mqttClients     map[string]*MQTT.Client
	passiveRequests map[string]*passiveRequest
	options         Options
}

var impl Plugin

type passiveRequest struct {
	request *plugin.Request
	mtime   time.Time
}

//Actual plugin
func (p *Plugin) Watch(requests []*plugin.Request, ctx plugin.ContextProvider) {

	p.manager.Lock()

	//clear broken or invalid connections on agent sync
	for broker, mqttClient := range impl.mqttClients {
		if !(*mqttClient).IsConnected() {
			//delete, so next subscription will try to reconnect
			delete(impl.mqttClients, broker)
		}
	}

	p.manager.Update(ctx.ClientID(), ctx.Output(), requests)
	p.manager.Unlock()

	for _, v := range p.mqttSubs {
		impl.Debugf("Registered MQTT subscriptions (broker and topic): %s %s\n", v.broker, v.topic)
	}
	impl.Debugf("Registered MQTT clients after update %v\n", p.mqttClients)

}

type mqttSub struct {
	broker   string
	topic    string
	manager  *watch.Manager
	connOpts MQTT.ClientOptions
}

func (t *mqttSub) onMessageReceived(client MQTT.Client, message MQTT.Message) {

	t.manager.Lock()
	impl.Debugf("Received message on topic(filter): %s(%s), Message: %s\n", message.Topic(), t.topic, message.Payload())
	t.manager.Notify(t, message)
	t.manager.Unlock()

}

func (t *mqttSub) mqttConnect() (mqttClient *MQTT.Client, err error) {
	impl.Infof("Checking for connection to %s\n", t.broker)
	mqttClient, found := impl.mqttClients[t.broker]
	if found {
		if (*mqttClient).IsConnected() {
			impl.Debugf("Already has connection\n")
		} else {
			return nil, MQTT.ErrNotConnected
		}
	} else {
		tmp := MQTT.NewClient(&t.connOpts)
		mqttClient = &tmp
		if token := (*mqttClient).Connect(); token.Wait() && token.Error() != nil {
			impl.Errf("%s\n", token.Error())
			impl.mqttClients[t.broker] = mqttClient
			return nil, token.Error()
		} else {
			impl.Infof("Connected to %s\n", t.broker)
			//add to list
			impl.mqttClients[t.broker] = mqttClient
		}
	}

	return mqttClient, nil
}

//Describe what need to be done for each invocation
func (t *mqttSub) mqttSubscribe(mqttClient *MQTT.Client) (err error) {

	impl.Debugf("Adding subscriptions %v\n", t.topic)

	if token := (*mqttClient).Subscribe(t.topic, byte(0), t.onMessageReceived); token.Wait() && token.Error() != nil {
		return error(token.Error())
	}
	return nil
}

//what would be unique key
func (t *mqttSub) URI() (uri string) {
	return t.broker + t.topic
}

func (t *mqttSub) Subscribe() (err error) {

	mqttClient, err := t.mqttConnect()
	if err != nil {
		return err
	}
	return t.mqttSubscribe(mqttClient)
}

func (t *mqttSub) Unsubscribe() {

	mqttClient, err := t.mqttConnect()
	if err != nil {
		impl.Errf("No client found so nothing to unsubscribe\n")
	} else {
		if token := (*mqttClient).Unsubscribe(t.topic); token.Wait() && token.Error() != nil {
			impl.Errf("%s\n", token.Error())
		}
	}
	delete(impl.mqttSubs, t.URI())

	//TODO
	//delete mqttClient
}

type itemFilter struct {
}

//Convert and filter
func (f *itemFilter) Convert(v interface{}) (value *string, err error) {

	if b, ok := v.(MQTT.Message); !ok {
		err = fmt.Errorf("unexpected traper conversion input type %T", v)
	} else {
		tmp := string(b.Payload())
		value = &tmp
	}
	return
}

func (t *mqttSub) NewFilter(key string) (filter watch.EventFilter, err error) {

	return &itemFilter{}, nil

}

//EventSourceByURI is used to unsubscribe
//from the sources without items associated to them
func (p *Plugin) EventSourceByURI(uri string) (es watch.EventSource, err error) {

	var ok bool
	if es, ok = p.mqttSubs[uri]; !ok {
		err = fmt.Errorf(`not registered listener URI "%s"`, uri)
	}
	return
}

func (p *Plugin) newSub(broker string, topic string) (listener *mqttSub) {

	clientid := p.options.ClientID
	username := p.options.Username
	password := p.options.Password

	connOpts := MQTT.NewClientOptions().AddBroker(broker).SetClientID(clientid).SetCleanSession(true)
	if username != "" {
		connOpts.SetUsername(username)
		if password != "" {
			connOpts.SetPassword(password)
		}
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: true, ClientAuth: tls.NoClientCert}
	connOpts.SetTLSConfig(tlsConfig)

	listener = &mqttSub{
		broker:   broker,
		manager:  p.manager,
		connOpts: *connOpts, //TODO move it somewhere
		topic:    topic,
	}

	return listener

}

//EventSourceByKey is used when trying to match item key
//with it's event source during item update
func (p *Plugin) EventSourceByKey(key string) (es watch.EventSource, err error) {
	var params []string
	if _, params, err = itemutil.ParseKey(key); err != nil {
		return
	}

	var broker string
	if len(params) == 0 {
		return
	}

	broker = params[0]

	var topic string
	if len(params) > 1 {
		topic = params[1]
	} else {
		topic = "#"
	}

	var ok bool
	var listener *mqttSub

	//if not exist, create?
	if listener, ok = p.mqttSubs[broker+topic]; !ok {
		impl.Debugf("MQTT Subscription %s not found, going to prepare one for broker %s\n", topic, broker)
		listener = p.newSub(broker, topic)
		p.mqttSubs[broker+topic] = listener
	}

	return listener, nil
}

func init() {
	impl.manager = watch.NewManager(&impl)
	impl.mqttSubs = make(map[string]*mqttSub)
	impl.mqttClients = make(map[string]*MQTT.Client)
	plugin.RegisterMetrics(&impl, "MQTTSubscribe", "mqtt.subscribe", "Subscribe to MQTT topic and receive messages when they arrive.")
}
