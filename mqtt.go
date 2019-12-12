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
	"strings"
	"time"
	"zabbix.com/pkg/itemutil"
	"zabbix.com/pkg/plugin"
	"zabbix.com/pkg/watch"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type Plugin struct {
	plugin.Base
	manager     *watch.Manager
	mqttClients map[string]*mqttClient
	options     Options
}

type mqttClient struct {
	broker   string
	client   *MQTT.Client
	mqttSubs map[string]*mqttSub
	connOpts MQTT.ClientOptions
}

type mqttSub struct {
	broker  string
	topic   string
	manager *watch.Manager
	state   state
}

type state uint32

const (
	initial state = iota
	subscribed
)

const (
	qos0 byte = iota
	qos1
	qos2
)

var impl Plugin

//Watch MQTT plugin
func (p *Plugin) Watch(requests []*plugin.Request, ctx plugin.ContextProvider) {

	p.manager.Lock()

	//this block cleans broken clients so they can be reinitialized
	for broker, mqttClient := range impl.mqttClients {
		if !(*mqttClient.client).IsConnected() {
			//delete broken clients, so next subscription will try to reconnect
			delete(impl.mqttClients, broker)
		}
	}

	p.manager.Update(ctx.ClientID(), ctx.Output(), requests)
	p.manager.Unlock()

	for _, c := range p.mqttClients {
		impl.Debugf("Registered MQTT clients after update %v\n", p.mqttClients)
		for _, v := range c.mqttSubs {
			impl.Debugf("Registered MQTT subscriptions (broker and topic): %s,%s\n", v.broker, v.topic)
		}
	}

}

//Return connected MQTT client
func (t *mqttSub) mqttConnect() (mqttClient *mqttClient, err error) {
	impl.Debugf("Checking for connection to %s\n", t.broker)
	mqttClient, found := impl.mqttClients[t.broker]
	if found && mqttClient.client != nil {
		if (*mqttClient.client).IsConnected() {
			impl.Debugf("Already has connection\n")
		} else {
			impl.Errf("Failed to establish connection to %s for %s: %s", t.broker, t.topic, MQTT.ErrNotConnected)
			return nil, MQTT.ErrNotConnected
		}
	} else {
		tmp := MQTT.NewClient(&mqttClient.connOpts)
		mqttClient.client = &tmp
		if token := (*mqttClient.client).Connect(); token.Wait() && token.Error() != nil {
			impl.Errf("%s\n", token.Error())
			return nil, token.Error()
		}

	}

	return mqttClient, nil
}

func (t *mqttSub) onMessageReceived(client MQTT.Client, message MQTT.Message) {

	t.manager.Lock()
	impl.Debugf("Received message on topic(filter): %s(%s), Message: %s\n", message.Topic(), t.topic, message.Payload())
	t.manager.Notify(t, message)
	t.manager.Unlock()

}

//Describe what need to be done for each invocation
func (t *mqttSub) mqttSubscribe(mqttClient *mqttClient) (err error) {

	impl.Debugf("Adding subscriptions %v\n", t.topic)

	if token := (*mqttClient.client).Subscribe(t.topic, qos0, t.onMessageReceived); token.Wait() && token.Error() != nil {
		return error(token.Error())
	}
	t.state = subscribed
	return nil
}

func (t *mqttSub) Subscribe() (err error) {

	mqttClient, err := t.mqttConnect()
	if err != nil {
		return err
	}

	err = t.mqttSubscribe(mqttClient)
	if err != nil {
		return err
	}
	return nil
}

func (t *mqttSub) Unsubscribe() {

	mqttClient, err := t.mqttConnect()
	if err != nil {
		impl.Errf("No client found so nothing to unsubscribe\n")
	} else {
		if token := (*mqttClient.client).Unsubscribe(t.topic); token.Wait() && token.Error() != nil {
			impl.Errf("%s\n", token.Error())
		}
	}

	delete(mqttClient.mqttSubs, t.topic)
	if len(mqttClient.mqttSubs) == 0 {
		impl.Debugf("No subscriptions left for MQTT client %s, removing client", t.broker)
		(*mqttClient.client).Disconnect(100)
		delete(impl.mqttClients, t.broker)
	}
}

//Return unique MQTT subscription identifier
func (t *mqttSub) URI() (uri string) {
	return t.broker + "?" + t.topic
}

//EventSourceByURI is used to unsubscribe
//from the sources without items associated to them
func (p *Plugin) EventSourceByURI(uri string) (es watch.EventSource, err error) {

	var ok bool
	s := strings.Split(uri, "?")
	if es, ok = p.mqttClients[s[0]].mqttSubs[strings.Join(s[1:], "")]; !ok {
		err = fmt.Errorf(`not registered listener URI "%s"`, uri)
	}
	return
}

//EventSourceByKey is used when trying to match item key
//with it's event source during item update
func (p *Plugin) EventSourceByKey(key string) (es watch.EventSource, err error) {
	var params []string
	if _, params, err = itemutil.ParseKey(key); err != nil {
		return
	}

	if len(params) != 2 {
		return nil, fmt.Errorf("Please provide key as mqtt.subscribe[<MQTT broker URL>,<MQTT topic>]")
	}

	var broker string
	broker = params[0]

	var topic string
	topic = params[1]

	var ok bool
	var listener *mqttSub

	//if no client exist, create?
	if mqttC, ok := p.mqttClients[broker]; !ok {

		impl.Debugf("MQTT Client not found, going to prepare one for broker %s\n", broker)

		clientid := p.options.ClientID
		username := p.options.Username
		password := p.options.Password
		timeout := time.Duration(p.options.Timeout) * time.Second

		connOpts := MQTT.NewClientOptions().AddBroker(broker).SetClientID(clientid).SetCleanSession(true)
		if username != "" {
			connOpts.SetUsername(username)
			if password != "" {
				connOpts.SetPassword(password)
			}
		}
		tlsConfig := &tls.Config{InsecureSkipVerify: true, ClientAuth: tls.NoClientCert}
		connOpts.SetTLSConfig(tlsConfig)

		connOpts.SetConnectTimeout(timeout)

		connOpts.OnConnectionLost = func(client MQTT.Client, reason error) {
			p.Errf("Connection lost to %s, reason: %s", broker, reason.Error())
		}

		//This handler is required to sucessfully resubscribe when connection is lost
		connOpts.OnConnect = func(client MQTT.Client) {
			p.Infof("Connected to %s", broker)
			c, found := p.mqttClients[broker]
			if found {
				if c.broker == broker {
					for _, v := range c.mqttSubs {
						if v.state == subscribed {
							//if subscribed then resubscribe (reconnect case)
							err := v.mqttSubscribe(c)
							if err != nil {
								p.Errf("Failed subscribing to %s after connecting to %s\n", v.topic, broker)
							}
						}

					}
				}
			}
		}

		mqttC = &mqttClient{
			connOpts: *connOpts,
			broker:   broker,
			mqttSubs: make(map[string]*mqttSub),
		}
		p.mqttClients[broker] = mqttC
	}
	//if not exist, create?
	if listener, ok = p.mqttClients[broker].mqttSubs[topic]; !ok {
		impl.Debugf("MQTT Subscription %s not found, going to prepare one for broker %s\n", topic, broker)
		listener = &mqttSub{
			broker:  broker,
			manager: p.manager,
			topic:   topic,
			state:   initial, //required to differentiate reconnection vs connection
		}
		p.mqttClients[broker].mqttSubs[topic] = listener
	}
	return listener, nil

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

func init() {
	impl.manager = watch.NewManager(&impl)
	impl.mqttClients = make(map[string]*mqttClient)
	plugin.RegisterMetrics(&impl, "MQTTSubscribe", "mqtt.subscribe", "Subscribe to MQTT topic and receive messages when they arrive.")
}
