package api

import "github.com/dsorm/yeelight2mqtt/console"

func (l *Light) SetRefreshCallback(callback func(message string)) {
	l.refreshCallback = callback
}

func RunRefreshDaemons(lights *[]Light) {
	deref := *lights
	for k := range deref {
		go deref[k].RefreshDaemon()
	}
}

func (l *Light) RefreshDaemon() {
	for {
		select {
		case msg := <-l.refreshMsg:
			if l.refreshCallback != nil {
				l.refreshCallback(msg)
			} else {
				console.Logf("Error: refresh callback not set for light %s", l.Name)
			}
		}
	}
}
