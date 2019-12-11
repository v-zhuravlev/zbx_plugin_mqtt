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
	"fmt"
	"zabbix.com/pkg/itemutil"
	"zabbix.com/pkg/plugin"
	"zabbix.com/pkg/watch"
	"crypto/tls"
	"time"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

// Plugin
type Plugin struct {
	plugin.Base
	manager     *watch.Manager
	mqttSubs   map[string]*mqttSub
	mqttClients map[string]*MQTT.Client
	passiveRequests   map[string]*passiveRequest
	options Options
}

var impl Plugin

type passiveRequest struct {
	request *plugin.Request
	mtime time.Time
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

	//reduce to key=mqtt.subscribe only
	// subscribes := make([]*plugin.Request,0,1)
	// for _,v := range requests {
	// 	if v.Key == "mqtt.subscribe" {
	// 		subscribes = append(subscribes, v)
	// 	}
	//}
	
	//append passive requests to active
	for _,v := range p.passiveRequests {
		requests = append(requests, v.request)
	}
	
	p.manager.Update(ctx.ClientID(), ctx.Output(), requests)
	p.manager.Unlock()
	
	//impl.Debugf("mqtt subscriptions(mqttSubs) we have after update %v\n", p.mqttSubs)
	for _,v := range p.mqttSubs {
		impl.Debugf("mqtt subs broker and topic: %s %s\n",v.broker, v.topic)
	}
	impl.Debugf("mqtt mqttClients() we have after update %v\n", p.mqttClients)

}


func (p *Plugin) Export(key string, params []string, ctx plugin.ContextProvider) (result interface{}, err error) {
	if key == "mqtt.subscribe_passive" {
		p.Debugf("export %s%v", key, params)	
		p.manager.Lock()
		defer p.manager.Unlock()
		
		es, err := p.EventSourceByKey(itemutil.MakeKey(key, params)); if err != nil {
			return  nil, fmt.Errorf("Failed to create MQTT subscription(EventsourceByKey):%s", err)
		}
		mqttSub, ok := es.(*mqttSub); if !ok {
			return  nil, fmt.Errorf("Failed to create MQTT subscription(not a mqttSub)")
		}

		fullKey := itemutil.MakeKey(key, params)
		pRequest, ok := p.passiveRequests[fullKey]; if !ok {
			request := &plugin.Request{
				Itemid: ctx.ItemID(),
				Key: fullKey,
			}
			pRequest = &passiveRequest{
				request: request,
			}
			p.passiveRequests[fullKey] = pRequest
		}
		//updated modify time
		pRequest.mtime = time.Now()

		
		// err = mqttSub.Subscribe(); if err != nil {
		// 	return  nil, fmt.Errorf("Failed to create MQTT subscription(Subscribe):%s", err)
		// }
		

		if (*mqttSub).last != nil {
			impl.Debugf("Returning last value: %s\n",key)
			filter := &itemFilter{}
			return filter.Convert(*mqttSub.last)
		}
			
		//break
		//impl.Debugf("mqtt subscriptions(mqttSubs) we have after update %v\n", p.mqttSubs)
		for _,v := range p.passiveRequests {
			impl.Debugf("mqtt passibe subs: %s, modified %s\n",v.request.Key, v.mtime)
		}
		impl.Debugf("mqtt mqttClients() we have after passive update %v\n", p.mqttClients)

	}
	return &plugin.Result{}, nil
}

//our structure, would be mqtt struct
type mqttSub struct {
	broker  string // use it like key
	topic   string
	manager *watch.Manager
	connOpts MQTT.ClientOptions
	last *MQTT.Message // last message. Passive subscribers can get it
}


func (t *mqttSub) onMessageReceived(client MQTT.Client, message MQTT.Message) {
	
	t.manager.Lock()
	impl.Debugf("Received message on topic(filter): %s(%s), Message: %s\n", message.Topic(), t.topic, message.Payload())
	t.manager.Notify(t, message)
	t.last = &message

	// for _,v := range impl.mqttZabbixTrappers {
	// 	if v.topic == t.topic {
	// 		impl.Debugf("Found Zabbix trapper subscription: %s for host %s", v.key, v.host)
	// 		ZabbixSender(string(message.Payload()),v)
	// 	}	
	// }

	
	t.manager.Unlock()

}

func (t *mqttSub) mqttConnect() (mqttClient *MQTT.Client, err error) {
	impl.Infof("Checking for connection to %s\n", t.broker)
	mqttClient, found := impl.mqttClients[t.broker]
	if found {

		if (*mqttClient).IsConnected() {
			impl.Debugf("Already has connection\n")
			//t.mqttClient = mqttClient
		} else {
			//delete, next subscription will try to reconnect
			//delete(impl.mqttClients,t.broker)
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

// subscribe. create actual collector
func (t *mqttSub) Subscribe() (err error) {

	mqttClient, err := t.mqttConnect()
	if err != nil {
		return err
	}
	return t.mqttSubscribe(mqttClient)
}

//how to terminate
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

//NewFilter parses item params. In MQTT We expect:
//Server host:port, topic,
func (t *mqttSub) NewFilter(key string) (filter watch.EventFilter, err error) {

	return &itemFilter{}, nil

}

//EventSourceByURI is used to unsubscribe
//from the sources without items associatede to them
func (p *Plugin) EventSourceByURI(uri string) (es watch.EventSource, err error) {

	var ok bool
	if es, ok = p.mqttSubs[uri]; !ok {
		err = fmt.Errorf(`not registered listener URI "%s"`, uri)
	}
	return
}


func (p *Plugin) newSub(broker string, topic string) (listener *mqttSub){

	
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
		topic: topic,
	}

	return listener
	

}

//EventSourceByKey is used when trying to match item key
// with it's event source during item update
func (p *Plugin) EventSourceByKey(key string) (es watch.EventSource, err error) {
	var params []string
	if _, params, err = itemutil.ParseKey(key); err != nil {
		return
	}
	

	//mqtt broker here
	//The full url of the MQTT broker to connect to ex: tcp://127.0.0.1:1883")
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

	// var qos int
	// if len(params) > 2 {
	// 	qos, err = strconv.Atoi(params[2]); if err !=nil {
	// 		return
	
	// 	}
	// } else {
	// 	qos = 0
	// }

	var ok bool
	var listener *mqttSub

	//if not exist, create?
	if listener, ok = p.mqttSubs[broker+topic]; !ok {
		impl.Debugf("MQTT Subscription %s not found, going to prepare one for broker %s\n", topic, broker)
		listener = p.newSub(broker,topic)
		p.mqttSubs[broker+topic] = listener
	}

	return listener, nil
}





func init() {

	impl.manager = watch.NewManager(&impl)
	impl.mqttSubs = make(map[string]*mqttSub)
	impl.mqttClients = make(map[string]*MQTT.Client)
	impl.passiveRequests = make(map[string]*passiveRequest)
	plugin.RegisterMetrics(&impl, "MQTTSubscribe", "mqtt.subscribe", "Subscribe to MQTT topic and receive messages when they arrive.")
	plugin.RegisterMetrics(&impl, "MQTTSubscribe", "mqtt.subscribe_passive", "Subscribe to MQTT topic using Zabbix agent in passive mode.")

}
