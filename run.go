package main

import (
	"errors"
	"fmt"
	"github.com/dsorm/yeelight2mqtt/api"
	"github.com/dsorm/yeelight2mqtt/console"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"gopkg.in/yaml.v2"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

const VERSION = "v1"

type MQTTSettings struct {
	Host      string
	Port      int
	TLS       bool
	User      string
	Password  string
	BaseTopic string
	QoS       int
}

// just so the yaml looks nice and readable
type PollingRate struct {
	Seconds uint16
}

type AppState struct {
	Lights           []api.Light
	LightPollingRate PollingRate
	MQTTSettings     MQTTSettings
	mqttClient       mqtt.Client
	Debug            bool
}

func (as *AppState) publishProp(light *api.Light) {
	// print
	// consoleLogf("%v%+v\n", light.Name, light.GetState)

	// publish using mqtt

	currentState := light.GetState()

	retainedData := map[string]string{
		"$homie":      "4.0",
		"$name":       light.Name,
		"$state":      "ready",
		"$nodes":      "main, bg",
		"$extensions": "",

		"$implementation": "dsorm/yeelight2mqtt@" + VERSION,

		"main/$name":       light.Name + "_main",
		"main/$type":       "Main Light",
		"main/$properties": "on,bright,ct,rgb,hue,sat,color_mode,flowing,delayoff,flow_params,music_on,name,nl_br,moonlight_on",

		"bg/$name":       light.Name + "_bg",
		"bg/$type":       "Ambilight",
		"bg/$properties": "bg_power,bg_flowing,bg_flow_params,bg_ct,bg_lmode,bg_bright,bg_rgb,bg_hue,bg_sat",

		"main/on":          fmt.Sprintf("%v", currentState.On),
		"main/on/name":     "Power",
		"main/on/datatype": "boolean",
		"main/on/settable": "true",

		"main/bright":          fmt.Sprintf("%v", currentState.Bright),
		"main/bright/name":     "Brightness",
		"main/bright/datatype": "integer",
		"main/bright/settable": "true",
		"main/bright/unit":     "%",
		"main/bright/format":   "1:100",

		"main/ct":          fmt.Sprintf("%v", currentState.Ct),
		"main/ct/name":     "Color Temperature",
		"main/ct/datatype": "integer",
		"main/ct/settable": "true",
		"main/ct/unit":     "K",
		"main/ct/format":   "1700:6500",

		"main/rgb":          fmt.Sprintf("%v", currentState.RGB),
		"main/rgb/name":     "RGB color",
		"main/rgb/datatype": "integer",
		"main/rgb/settable": "true",
		"main/rgb/format":   "0:16777215",

		"main/hue":          fmt.Sprintf("%v", currentState.Hue),
		"main/hue/name":     "Hue",
		"main/hue/datatype": "integer",
		"main/hue/settable": "true",
		"main/hue/format":   "0:359",

		"main/sat":          fmt.Sprintf("%v", currentState.Sat),
		"main/sat/name":     "Saturation",
		"main/sat/datatype": "integer",
		"main/sat/settable": "true",
		"main/sat/format":   "0:100",

		"main/color_mode":          fmt.Sprintf("%v", currentState.Color_Mode),
		"main/color_mode/name":     "Color Mode",
		"main/color_mode/datatype": "string",
		"main/color_mode/settable": "true",
		"main/color_mode/format":   "RGB,CT,HSV,Flow", // might not be according to Homie spec, but I believe it is useful

		"main/flowing":          fmt.Sprintf("%v", currentState.Flowing),
		"main/flowing/name":     "Flowing",
		"main/flowing/datatype": "boolean",
		"main/flowing/settable": "true",

		"main/delayoff":          fmt.Sprintf("%v", currentState.Delayoff),
		"main/delayoff/name":     "Delay Off",
		"main/delayoff/datatype": "integer",
		"main/delayoff/settable": "true",
		"main/delayoff/unit":     "minutes",
		"main/delayoff/format":   "0:60",

		"main/flow_params":          fmt.Sprintf("%v", currentState.Flow_Params),
		"main/flow_params/name":     "Flow Parameters",
		"main/flow_params/datatype": "string",
		"main/flow_params/settable": "true",

		"main/music_on":          fmt.Sprintf("%v", currentState.Music_On),
		"main/music_on/name":     "Music On",
		"main/music_on/datatype": "boolean",
		"main/music_on/settable": "false",

		"main/name":          fmt.Sprintf("%v", currentState.Name),
		"main/name/name":     "Name",
		"main/name/datatype": "string",
		"main/name/settable": "false",

		"main/nl_br":          fmt.Sprintf("%v", currentState.Nl_Br),
		"main/nl_br/name":     "Moonlight Brightness",
		"main/nl_br/datatype": "integer",
		"main/nl_br/settable": "true",
		"main/nl_br/unit":     "%",
		"main/nl_br/format":   "1:100",

		"main/moonlight_on":          fmt.Sprintf("%v", currentState.Moonlight_On),
		"main/moonlight_on/name":     "Moonlight On",
		"main/moonlight_on/datatype": "boolean",
		"main/moonlight_on/settable": "true",

		"bg/on":          fmt.Sprintf("%v", currentState.Bg_On),
		"bg/on/name":     "Power",
		"bg/on/datatype": "boolean",
		"bg/on/settable": "true",

		"bg/flowing":          fmt.Sprintf("%v", currentState.Bg_Flowing),
		"bg/flowing/name":     "Flowing",
		"bg/flowing/datatype": "boolean",
		"bg/flowing/settable": "true",

		"bg/flow_params":          fmt.Sprintf("%v", currentState.Bg_Flow_Params),
		"bg/flow_params/name":     "Flow Parameters",
		"bg/flow_params/datatype": "string",
		"bg/flow_params/settable": "true",

		"bg/ct":          fmt.Sprintf("%v", currentState.Bg_Ct),
		"bg/ct/name":     "Color Temperature",
		"bg/ct/datatype": "integer",
		"bg/ct/settable": "true",
		"bg/ct/unit":     "K",
		"bg/ct/format":   "1700:6500",

		"bg/color_mode":          fmt.Sprintf("%v", currentState.Bg_Color_Mode),
		"bg/color_mode/name":     "Color Mode",
		"bg/color_mode/datatype": "string",
		"bg/color_mode/settable": "true",
		"bg/color_mode/format":   "RGB,CT,HSV,Flow",

		"bg/bright":          fmt.Sprintf("%v", currentState.Bg_Bright),
		"bg/bright/name":     "Brightness",
		"bg/bright/datatype": "integer",
		"bg/bright/settable": "true",
		"bg/bright/unit":     "%",
		"bg/bright/format":   "1:100",

		"bg/rgb":          fmt.Sprintf("%v", currentState.Bg_RGB),
		"bg/rgb/name":     "RGB color",
		"bg/rgb/datatype": "integer",
		"bg/rgb/settable": "true",
		"bg/rgb/format":   "0:16777215",

		"bg/hue":          fmt.Sprintf("%v", currentState.Bg_Hue),
		"bg/hue/name":     "Hue",
		"bg/hue/datatype": "integer",
		"bg/hue/settable": "true",
		"bg/hue/format":   "0:359",
	}

	// data := map[string]string{
	// }

	baseTopic := fmt.Sprintf("%v/%v/", as.MQTTSettings.BaseTopic, light.Name)
	for topic, value := range retainedData {
		as.mqttClient.Publish(baseTopic+topic, byte(as.MQTTSettings.QoS), true, value)
	}
	// for topic, value := range data {
	//	as.mqttClient.Publish(baseTopic+topic, 0, false, value)
	// }

}

func (as *AppState) publishSingleProp(light *api.Light, topic string, payload interface{}) {
	baseTopic := fmt.Sprintf("%v/%v/", as.MQTTSettings.BaseTopic, light.Name)
	console.Logf("%v%v = %v\n", baseTopic, topic, payload)
	as.mqttClient.Publish(baseTopic+topic, byte(as.MQTTSettings.QoS), true, payload)
}

func (as *AppState) subProp(l *api.Light) {
	topicsToSubscribe := map[string]func(client mqtt.Client, message mqtt.Message){
		"main/on/set": func(client mqtt.Client, message mqtt.Message) {
			// yeelight2mqtt internally uses bool as a bool (makes sense)
			// but yeelights use string with 'on' or 'off' as a bool
			// and the Homie specification requires a string with 'true' or 'false'
			// just to clear up the confusion for anyone reading this

			// verify and convert payload
			var yeelightBool string
			var internalBool bool
			switch string(message.Payload()) {
			case "true":
				yeelightBool = "on"
				internalBool = true
			case "false":
				yeelightBool = "off"
				internalBool = false
			default:
				console.Logf("Error while processing '%v -> %v': not 'true' or 'false'\n", message.Topic(), string(message.Payload()))
				return
			}

			// change stuff
			err := l.SetPower(yeelightBool, "smooth", "500", "")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			state := l.GetState()
			state.On = internalBool

			as.publishSingleProp(l, "main/on", fmt.Sprintf("%v", state.On))
		},

		"main/bright/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			brightness, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}
			if brightness > math.MaxUint8 {
				console.Logf("Error while processing '%v -> %v': brightness too high\n", message.Topic(), string(message.Payload()))
				return
			}

			// change stuff
			err = l.SetBright(uint8(brightness), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "main/bright", fmt.Sprintf("%v", l.GetState().Bright))
		},

		"main/ct/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			ct, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.SetCtAbx(uint(ct), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "main/ct", fmt.Sprintf("%v", l.GetState().Ct))
		},

		"main/rgb/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			rgb, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.SetRGB(uint32(rgb), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "main/rgb", fmt.Sprintf("%v", l.GetState().RGB))
		},

		"main/hue/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			hue, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.SetHSV(uint16(hue), l.GetState().Sat, "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "main/hue", fmt.Sprintf("%v", l.GetState().Hue))
		},

		"main/sat/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			sat, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.SetHSV(l.GetState().Hue, uint8(sat), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "main/sat", fmt.Sprintf("%v", l.GetState().Sat))

		},

		"main/color_mode/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			colorMode, err := api.ColorModeFromString(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to colorMode: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			powerMode := ""
			switch colorMode {
			case api.ColorModeRGB:
				powerMode = "2"
			case api.ColorModeHSV:
				powerMode = "3"
			case api.ColorModeCT:
				powerMode = "1"
			case api.ColorModeFlow:
				powerMode = "4"
			}

			state := ""
			if l.GetState().On {
				state = "on"
			} else {
				state = "off"
			}

			err = l.SetPower(state, "smooth", "500", powerMode)
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
			}

			// update state
			as.publishSingleProp(l, "main/color_mode", fmt.Sprintf("%v", l.GetState().Color_Mode))
		},

		"main/flowing/set": func(client mqtt.Client, message mqtt.Message) {
			// TODO not implemented yet
			console.Logf("%v not implemented yet, ignoring\n", "main/flowing/set")
			// verify payload

			// change stuff

			// update state
		},

		"main/delayoff/set": func(client mqtt.Client, message mqtt.Message) {
			// TODO not implemented yet
			console.Logf("%v not implemented yet, ignoring\n", "main/delayoff/set")
			// verify payload

			// change stuff

			// update state
		},

		"main/flow_params/set": func(client mqtt.Client, message mqtt.Message) {
			// TODO not implemented yet
			console.Logf("%v not implemented yet, ignoring\n", "main/flow_params/set")
			// verify payload

			// change stuff

			// update state
		},

		// "set/main/music_on" : func(client mqtt.Client, message mqtt.Message) {},
		// "set/main/name" : func(client mqtt.Client, message mqtt.Message) {},

		"main/nl_br/set": func(client mqtt.Client, message mqtt.Message) {
			// TODO not implemented yet
			console.Logf("%v not implemented yet, ignoring\n", "main/nl_br/set")

			// verify payload

			// change stuff

			// update state
		},

		"main/moonlight_on/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			str := string(message.Payload())
			mode := ""

			switch str {
			case "true":
				mode = "5"
			case "false":
				mode = "0"
			default:
				console.Logf("'%v -> %v': Error while converting to bool\n", message.Topic(), string(message.Payload()))
				return
			}

			// change stuff
			state := ""
			if l.GetState().On {
				state = "on"
			} else {
				state = "off"
			}

			err := l.SetPower(state, "smooth", "500", mode)
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "main/moonlight_on", fmt.Sprintf("%v", l.GetState().Moonlight_On))
		},

		"bg/on/set": func(client mqtt.Client, message mqtt.Message) {
			// yeelight2mqtt internally uses bool as a bool (makes sense)
			// but yeelights use string with 'on' or 'off' as a bool
			// and the Homie specification requires a string with 'true' or 'false'
			// just to clear up the confusion for anyone reading this

			// verify and convert payload
			var yeelightBool string
			var internalBool bool
			switch string(message.Payload()) {
			case "true":
				yeelightBool = "on"
				internalBool = true
			case "false":
				yeelightBool = "off"
				internalBool = false
			default:
				console.Logf("Error while processing '%v -> %v': not 'true' or 'false'\n", message.Topic(), string(message.Payload()))
				return
			}

			// change stuff
			err := l.BgSetPower(yeelightBool, "smooth", "500", "")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			state := l.GetState()
			state.On = internalBool

			as.publishSingleProp(l, "bg/on", fmt.Sprintf("%v", state.Bg_On))
		},

		"bg/flowing/set": func(client mqtt.Client, message mqtt.Message) {
			// TODO not implemented yet
			console.Logf("%v not implemented yet, ignoring\n", "bg/flowing/set")
			// verify payload

			// change stuff

			// update state
		},

		"bg/flow_params/set": func(client mqtt.Client, message mqtt.Message) {
			// TODO not implemented yet
			console.Logf("%v not implemented yet, ignoring\n", "bg/flow_params/set")
			// verify payload

			// change stuff

			// update state
		},

		"bg/ct/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			ct, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.BgSetCtAbx(uint(ct), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "bg/ct", fmt.Sprintf("%v", l.GetState().Bg_Ct))
		},

		"bg/color_mode/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			colorMode, err := api.ColorModeFromString(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to colorMode: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			powerMode := ""
			switch colorMode {
			case api.ColorModeRGB:
				powerMode = "2"
			case api.ColorModeHSV:
				powerMode = "3"
			case api.ColorModeCT:
				powerMode = "1"
			case api.ColorModeFlow:
				powerMode = "4"
			}

			state := ""
			if l.GetState().Bg_On {
				state = "on"
			} else {
				state = "off"
			}

			err = l.BgSetPower(state, "smooth", "500", powerMode)
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
			}

			// update state
			as.publishSingleProp(l, "bg/color_mode", fmt.Sprintf("%v", l.GetState().Bg_Color_Mode))
		},

		"bg/bright/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			brightness, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}
			if brightness > math.MaxUint8 {
				console.Logf("Error while processing '%v -> %v': brightness too high\n", message.Topic(), string(message.Payload()))
				return
			}

			// change stuff
			err = l.BgSetBright(uint8(brightness), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "bg/bright", fmt.Sprintf("%v", l.GetState().Bg_Bright))
		},

		"bg/rgb/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			rgb, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.BgSetRGB(uint32(rgb), "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "bg/rgb", fmt.Sprintf("%v", l.GetState().Bg_RGB))
		},

		"bg/hue/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			hue, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				console.Logf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = l.BgSetHSV(uint16(hue), l.GetState().Bg_Sat, "smooth", "500")
			if err != nil {
				console.Logf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			as.publishSingleProp(l, "bg/hue", fmt.Sprintf("%v", l.GetState().Bg_Hue))
		},
	}

	baseTopic := fmt.Sprintf("%v/%v/", as.MQTTSettings.BaseTopic, l.Name)

	for topic, callback := range topicsToSubscribe {
		token := as.mqttClient.Subscribe(baseTopic+topic, 2, callback)
		token.WaitTimeout(time.Second)
		if err := token.Error(); err != nil {
			console.Logf("Error while subscribing to topic '%v': %v\n", baseTopic+topic, err)
		}
	}
}

func (as *AppState) LoadFromYAML(filename string) error {
	f, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(f, &as)
	if err != nil {
		return err
	}

	return nil
}

func (as *AppState) SaveToYAML(filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}

	err = yaml.NewEncoder(f).Encode(as)
	if err != nil {
		return err
	}

	return nil
}

func CreateConfig(filename string) error {
	defaultConfig := AppState{
		Lights: []api.Light{
			{
				Host: "192.168.50.2",
				Name: "light-1-example",
			},
			{
				Host: "192.168.50.3",
				Name: "light-2-example",
			},
		},
		MQTTSettings: MQTTSettings{
			Host:      "localhost",
			Port:      1883,
			User:      "",
			Password:  "",
			BaseTopic: "y2m",
			QoS:       2,
		},
		LightPollingRate: PollingRate{Seconds: 10},
	}

	return defaultConfig.SaveToYAML(filename)
}

// Receive MQTT messages and push the changes to the lights accordingly
func (as *AppState) statePushDaemon() {
	for k := range as.Lights {
		as.subProp(&as.Lights[k])
	}
	console.Logln("Subscribed to MQTT messages for the lights!")
}

// Poll every light and publish the properties at certain intervals
func (as *AppState) stateDaemon() {
	// trick to make the ticker start immediately
	ticker := time.NewTicker(1 * time.Second)
	console.Logf("Initial poll starting...\n")

	go func() {

		for {
			select {
			case <-ticker.C:
				// poll every light and publish the properties
				for k := range as.Lights {
					err := as.Lights[k].GetProp()
					if err != nil {
						console.Logln(err)
						continue
					}

					as.publishProp(&as.Lights[k])
					if as.Debug {
						console.Logf("Polled light '%v' at %v\n", as.Lights[k].Name, time.Now())
					}
				}
			}
		}
	}()

	time.Sleep(1250 * time.Millisecond)
	// set real ticker interval after the first tick
	ticker.Reset(time.Duration(as.LightPollingRate.Seconds) * time.Second)
	console.Logf("Polling the lights every %vs...\n", as.LightPollingRate.Seconds)
}

func (as *AppState) mqttInit() error {
	// create a new mqtt client
	opts := mqtt.NewClientOptions()

	// check for supported protocols, and if none is specified, use tcp://
	supportedProtocols := []string{"tcp://", "ssl://", "ws://", "wss://"}
	supportedProtocol := false
	for _, protocol := range supportedProtocols {
		if strings.Contains(as.MQTTSettings.Host, protocol) {
			supportedProtocol = true
			break
		}
	}
	if !supportedProtocol && strings.Contains(as.MQTTSettings.Host, "://") {
		return errors.New("This MQTT protocol is not supported")
	}

	if !supportedProtocol {
		console.Logln("MQTT protocol not specified in host, using tcp://..")
		as.MQTTSettings.Host = "tcp://" + as.MQTTSettings.Host
	}

	opts.AddBroker(fmt.Sprintf("%s:%d", as.MQTTSettings.Host, as.MQTTSettings.Port))
	opts.SetUsername(as.MQTTSettings.User)
	opts.SetPassword(as.MQTTSettings.Password)

	console.Logf("Connecting to MQTT broker %v...\n", as.MQTTSettings.Host)
	as.mqttClient = mqtt.NewClient(opts)
	if token := as.mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	console.Logln("Connection to MQTT broker established!")
	return nil
}

func main() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)

	// go func() {
	// 	select {
	// 	case _ = <-c:
	// 		sendsTotal := float32(unsuccessfulSends) + float32(successfulSends)
	//
	// 		// calculate the value of failure rate in percent
	// 		failureRate := float32(unsuccessfulSends) / sendsTotal * 100
	//
	// 		consoleLogf("Unsuccessful sends: %v, successful sends: %v, failure rate %.2f%%", unsuccessfulSends, successfulSends, failureRate)
	// 		os.Exit(1)
	// 	}
	// }()

	console.Logf("yeelight2mqtt %v starting...\n", VERSION)

	as := AppState{}

	err := as.LoadFromYAML("config.yaml")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			console.Logln("Config.yaml doesn't exist, creating a sample config.yaml...")
			err = CreateConfig("config.yaml")
			if err != nil {
				log.Fatalf("%v", err)
			}
			console.Logln("Change the values in config.yaml as needed and start yeelight2mqtt again.")

			return
		}

		log.Fatalf("An error has occured while trying to load config.yaml: %v", err)
	}

	err = as.mqttInit()
	if err != nil {
		log.Fatalf("An error has occured while trying to initialize MQTT: %v", err)
	}

	for k := range as.Lights {
		as.Lights[k].SetRefreshCallback(func(message string) {
			// TODO do stuff with the message
			console.Logf("Received message from '%v': %v\n", as.Lights[k].Host, message)
		})
	}
	api.RunRefreshDaemons(&as.Lights)
	as.stateDaemon()
	as.statePushDaemon()

	// block indefinitely
	<-(chan int)(nil)
}
