package mqtt

import (
	"os"
	"strconv"
	"time"
	"zabbix.com/pkg/conf"
	"zabbix.com/pkg/plugin"
	// MQTT "github.com/eclipse/paho.mqtt.golang"
)

type Options struct {
	//Interval int
	Username string // MQTT username to use
	Password string // MQTT password to use
	ClientID string // MQTT clientid to use
}

func (p *Plugin) Configure(global *plugin.GlobalOptions, private interface{}) {

	//p.options.Interval = 10
	p.options.Username = ""
	p.options.Password = ""

	hostname, _ := os.Hostname()
	p.options.ClientID = hostname + strconv.Itoa(time.Now().Second())

	if err := conf.Unmarshal(private, &p.options); err != nil {
		p.Warningf("cannot unmarshal configuration options: %s", err)
	}

	//p.Debugf("configure: interval=%d", p.options.Interval)
	p.Debugf("configure: username=%s", p.options.Username)
	p.Debugf("configure: clientid=%s", p.options.ClientID)
	//p.Debugf("configure: password is set")

}

func (p *Plugin) Validate(private interface{}) (err error) {
	return
}
