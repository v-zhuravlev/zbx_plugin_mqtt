# MQTT subscriber

Zabbix agent2 plugin(watcher) implementing MQTT. Work in progress.

## Build

Make sure golang is [installed](https://golang.org/doc/install) and properly configured.

Checkout zabbix:  
`git clone https://git.zabbix.com/scm/zbx/zabbix.git --depth 1 zabbix-agent2`  
`cd zabbix-agent2`  
Checkout this plugin repo:  
`git submodule add https://github.com/v-zhuravlev/zbx_plugin_mqtt.git src/go/plugins/mqtt`  

Edit file `src/go/plugins/plugins.go` by appending `_ "zabbix.com/plugins/serial"`:

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
`./configure --enable-agent2`  
`make`  

You will then find new agent with plugin included in `src/go/bin` dir

Test it by running
`./zabbix-agent2 -t agent.ping -c ../conf/zabbix_agent2.conf`

## TODO

- [ ] disconnect MQTT clients when there is no active mqtt.subscribe
- [ ] disconnect MQTT and remove subsceribe_passive when there is no active calls for mqtt.subscribe_passive
- [ ] make `last` expirable (configuration parameter)
- [x] move mqtt.get to separate plugin so no Exporter invocation
- [x] make user + password configurable in config file

## TODO PHASE 2

- [ ] add throttling with avg(), max(), min()...1min... (out of scope?)
- [ ] add support of QoS 1/2 messages
- [ ] add support for retain messages
- [ ] add ability to subscribe from Config file instead of mqtt.subscribe[]