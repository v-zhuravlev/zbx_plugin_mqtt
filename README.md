# Zabbix agent2 MQTT subscriber plugin

Zabbix agent2 plugin(watcher) implementing MQTT.

![image](https://user-images.githubusercontent.com/14870891/70622869-e340c400-1c2d-11ea-8ec0-bd1a89e970ee.png)


## Build

Make sure golang is [installed](https://golang.org/doc/install) and properly configured.

Checkout zabbix:  
`git clone https://git.zabbix.com/scm/zbx/zabbix.git --depth 1 zabbix-agent2`  
`cd zabbix-agent2`  
Checkout this plugin repo:  
`git submodule add https://github.com/v-zhuravlev/zbx_plugin_mqtt.git src/go/plugins/mqtt`  

Edit file `src/go/plugins/plugins.go` by appending `_ "zabbix.com/plugins/mqtt"`:

```go
package plugins

import (
	_ "zabbix.com/plugins/log"
	_ "zabbix.com/plugins/systemrun"
	_ "zabbix.com/plugins/zabbix/async"
	_ "zabbix.com/plugins/zabbix/stats"
	_ "zabbix.com/plugins/zabbix/sync"
	_ "zabbix.com/plugins/mqtt"
)
```

`./bootstrap.sh`  
`pushd .`  
`cd src/go/`  
`go mod vendor`  
`popd`  
Apply patches:  
`git apply  src/go/plugins/mqtt/patches/manager.patch`  
(see [this upstream PR](https://github.com/eclipse/paho.mqtt.golang/pull/388)):  
`git apply  src/go/plugins/mqtt/patches/router.patch`  
`./configure --enable-agent2 --enable-static`  
`make`  

You will then find new agent with plugin included in `src/go/bin` dir

Test it by running
`./zabbix-agent2 -t agent.ping -c ../conf/zabbix_agent2.conf`

## Configuration

In zabbix_agent2.conf:

- set `ServerActive=` to IP of your Zabbix server or proxy
- set `Hostname=` to host name you will use in Zabbix.
- (optional) set MQTT name and password, if required by your broker:

```shell
Plugins.MQTTSubscribe.Username=<username here>
Plugins.MQTTSubscribe.Password=<password here>
```

- (optional) set MQTT ClientID. If not set, ClientID will be generated automatically.

```shell
Plugins.MQTTSubscribe.ClientID=zabbix-agent2-mqtt-client
```

- (optional) set MQTT Connection timeout in seconds. If not set, global Zabbix agent `Timeout=` will be used.

```shell
Plugins.MQTTSubscribe.Timeout=5
```

## Supported keys

### mqtt.subscribe

 `mqtt.subscribe[<MQTT broker URL>,<MQTT topic>]`

 for example:

 `mqtt.subscribe[tcp://192.168.1.1:1883,devices/+/values]`

 Item must be of type `Zabbix agent(active)`.  
 Also note, that update interval is ignored, values will be received once published to the MQTT broker. Still, try to set update interval as closer as possible to the data update frequency, so you get proper graphs in Zabbix.

![image](https://user-images.githubusercontent.com/14870891/70622682-82b18700-1c2d-11ea-9d94-e9029eb42c8c.png)

## Troubleshooting

Change `LogType=console` and `DebugLevel=4` in config file.

## Known problems and limitations

- Only Zabbix agent (active) is supported, thus impossible to use single Zabbix agent2 with multiple hosts. Waiting for `Passive bulk` implementation in Zabbix.
- Global timeout is ignored if there are no plugin specific options are defined https://support.zabbix.com/browse/ZBX-17070

## Next steps

- [x] disconnect MQTT clients when there is no active mqtt.subscribe
- [x] move mqtt.get to separate plugin so no Exporter invocation
- [x] make user + password configurable in config file
- [x] validate item params

and perhaps...

- [ ] add support of TLS options
- [ ] add throttling with avg(), max(), min()...1min... (out of scope?)
- [ ] add support of QoS 1/2 messages
- [ ] add support for retain messages
- [ ] add ability to subscribe from Config file instead of mqtt.subscribe[]
- [ ] implement ReconnectHandler https://github.com/eclipse/paho.mqtt.golang/blob/master/options.go#L51

## Changelog

### v0.1

Initial version
