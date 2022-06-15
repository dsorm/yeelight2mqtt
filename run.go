package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const VERSION = "v1"

// for debug only, temporary
var successfulSends uint64
var unsuccessfulSends uint64

type sendCommandCtx struct {
	tries    uint64
	maxTries uint64
	command  string
	l        *Light
}

func (l *Light) sendCommand(command string, maxTries int) (response []byte, err error) {
	ctx := &sendCommandCtx{
		maxTries: uint64(maxTries),
		command:  command,
		l:        l,
	}
	return ctx.sendCommandWithCtx()
}

func (ctx *sendCommandCtx) sendCommandWithCtx() (response []byte, err error) {
	ctx.tries++
	if ctx.tries >= ctx.maxTries {
		return nil, errors.New("max tries exceeded")
	}

	ctx.l.connMutex.Lock()

	// a very dirty way to ratelimit the number of requests to the Yeelight, but it works :)
	unlockMutex := func() {
		time.Sleep(time.Second / 2)
		ctx.l.connMutex.Unlock()
	}

	if ctx.l.conn == nil {
		ctx.l.conn, err = net.Dial("tcp", fmt.Sprintf("%s:55443", ctx.l.Host))
		if err != nil {
			unlockMutex()
			return ctx.sendCommandWithCtx()
		}
	}

	// wait a while before writing command, since yeelights are a bit slow,
	// they might write the output of last command to this command or just crap out
	time.Sleep(time.Second / 4)

	// if ctx.command is empty, just read from the connection
	if ctx.command != "" {
		_, err = ctx.l.conn.Write([]byte(ctx.command + "\r\n"))
		if err != nil {
			ctx.l.conn.Close()
			ctx.l.conn = nil
			unlockMutex()
			return ctx.sendCommandWithCtx()
		}

	}

	err = ctx.l.conn.SetDeadline(time.Now().Add(time.Second * 5))
	if err != nil {
		ctx.l.conn.Close()
		ctx.l.conn = nil
		unlockMutex()
		return ctx.sendCommandWithCtx()
	}

	resp := []byte{}
	// read a line from response
	for {
		buf := make([]byte, 1024)
		n, err := ctx.l.conn.Read(buf)
		if err != nil {
			if strings.Contains(err.Error(), "timeout") {
				fmt.Printf("sendCommand() to %v failed due to timeout, retrying... (%v/%v)\n", ctx.l.Host, ctx.tries, ctx.maxTries)
			} else {
				fmt.Printf("sendCommand() to %v failed due to unknown reason, retrying... (%v/%v)\n", ctx.l.Host, ctx.tries, ctx.maxTries)
			}
			unsuccessfulSends++
			unlockMutex()
			return ctx.sendCommandWithCtx()
		}
		resp = append(resp, buf[:n]...)
		if string(resp[len(resp)-2:]) == "\r\n" {
			successfulSends++
			break
		}
	}

	// detect if the yeelight is vomiting the (currently) unnecessary output about prop change
	if strings.HasPrefix(string(resp), "{\"method\":\"props\",\"params\":{") {
		ctx.l.propChange <- string(resp)

		// read again to get the requested response
		ctx.tries--
		ctx.command = ""
		unlockMutex()
		return ctx.sendCommandWithCtx()
	}

	go unlockMutex()
	return resp, err
}

type Light struct {
	Host string
	Name string

	latestState lightProperties
	conn        net.Conn
	connMutex   sync.Mutex

	// When there is a change in Yeelight props, it is automatically sent by the light back to yeelight2mqtt
	// yeelight2mqtt only reads from the connection when a request is made or because of automatic polling,
	// so this channel is used to get rid of the data (when handling another request), so a dedicated goroutine may handle it
	propChange chan string //
}

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
	Lights           []Light
	LightPollingRate PollingRate
	MQTTSettings     MQTTSettings
	mqttClient       mqtt.Client
}

type colorMode uint8

const (
	colorModeRGB colorMode = iota
	colorModeCT
	colorModeHSV
	colorModeFlow
)

func (cm colorMode) String() string {
	switch cm {
	case colorModeRGB:
		return "RGB"
	case colorModeCT:
		return "CT"
	case colorModeHSV:
		return "HSV"
	case colorModeFlow:
		return "Flow"
	}
	return ""
}

func colorModeFromString(str string) (colorMode, error) {
	switch str {
	case "RGB":
		return colorModeRGB, nil
	case "CT":
		return colorModeCT, nil
	case "HSV":
		return colorModeHSV, nil
	case "Flow":
		return colorModeFlow, nil
	}
	return 0, nil
}

type lightProperties struct {
	on             bool      // "on" or "off"
	bright         uint8     // (range 1 - 100)
	ct             uint16    // (range 1700 - 6500) (unit: Kelvin)
	rgb            uint32    // (range 0 - 16777215)
	hue            uint16    // (range 0 - 359)
	sat            uint8     // (range 0 - 100)
	color_mode     colorMode // 0: RGB mode, 1: Color temperature mode, 2: HSV mode, 3: Flow mode
	flowing        bool
	delayoff       uint8 // (range 1 - 60 minutes)
	flow_params    string
	music_on       bool
	name           string
	bg_power       bool
	bg_flowing     bool
	bg_flow_params string
	bg_ct          uint16
	bg_lmode       colorMode
	bg_bright      uint8
	bg_rgb         uint32
	bg_hue         uint16
	bg_sat         uint8
	nl_br          uint8 // (range 1 - 100)
	moonlight_on   bool
}

func (l *Light) get_prop() error {
	command := "{\"id\":0,\"method\":\"get_prop\",\"params\":[\"power\", \"bright\", \"ct\", \"rgb\", \"hue\", \"sat\", \"color_mode\", \"flowing\", \"delayoff\", \"flow_params\", \"music_on\", \"name\", \"bg_power\", \"bg_flowing\", \"bg_flow_params\", \"bg_ct\", \"bg_lmode\", \"bg_bright\", \"bg_rgb\", \"bg_hue\", \"bg_sat\", \"nl_br\", \"active_mode\"]}"
	resp, err := l.sendCommand(command, 3)

	if err != nil {
		return err
	}

	// var props lightProperties
	d := json.NewDecoder(bytes.NewReader(resp))

	var m map[string]interface{}
	err = d.Decode(&m)
	if err != nil {
		return err
	}

	// make sure m["result"] is not nill
	_, keyExists := m["result"]
	if !keyExists {
		return fmt.Errorf("get_prop() failed: output from light doesn't contain result")
	}

	// make sure m["result"] is the right type
	if reflect.TypeOf(m["result"]).Kind() != reflect.Slice {
		return fmt.Errorf("get_prop() failed: %v", m["result"])
	}
	result := m["result"].([]interface{})
	if len(result) < 21 {
		return fmt.Errorf("get_prop() failed: result too small (expected at least 21, got %v), result is %v\n", len(result), result)
	}

	atouint8 := func(i interface{}) uint8 {
		s := i.(string)
		if i.(string) == "" {
			return 0
		}

		num, err := strconv.Atoi(s)
		if err != nil {
			log.Fatalf("atoi: %v", err)
		}
		return uint8(num)
	}

	atouint16 := func(i interface{}) uint16 {
		s := i.(string)
		if i.(string) == "" {
			return 0
		}
		num, err := strconv.Atoi(s)
		if err != nil {
			fmt.Printf("atoi: %v\n", err)
		}
		return uint16(num)
	}

	atouint32 := func(i interface{}) uint32 {
		s := i.(string)
		if i.(string) == "" {
			return 0
		}

		num, err := strconv.Atoi(s)
		if err != nil {
			fmt.Printf("atoi: %v\n", err)
		}
		return uint32(num)
	}
	lp := lightProperties{
		on:             result[0].(string) == "on",
		bright:         atouint8(result[1]),
		ct:             atouint16(result[2]),
		rgb:            atouint32(result[3]),
		hue:            atouint16(result[4]),
		sat:            atouint8(result[5]),
		color_mode:     colorMode(atouint8(result[6])),
		flowing:        result[7].(string) == "1",
		delayoff:       atouint8(result[8]),
		flow_params:    result[9].(string),
		music_on:       result[10].(string) == "1",
		name:           result[11].(string),
		bg_power:       result[12].(string) == "on",
		bg_flowing:     result[13].(string) == "1",
		bg_flow_params: result[14].(string),
		bg_ct:          atouint16(result[15]),
		bg_lmode:       colorMode(atouint8(result[16])),
		bg_bright:      atouint8(result[17]),
		bg_rgb:         atouint32(result[18]),
		bg_hue:         atouint16(result[19]),
		bg_sat:         atouint8(result[20]),
		nl_br:          atouint8(result[21]),
		moonlight_on:   result[22].(string) == "1",
	}

	l.latestState = lp
	return nil
}

/*
	"ct_value" is the target color temperature. The type is integer and
	range is 1700 ~ 6500 (k).

	"effect" support two values: "sudden" and "smooth".
		If effect is "sudden",  then the color temperature will be changed directly to target value,
		under this case, the third parameter "duration" is ignored.
		If effect is "smooth", then the color temperature will be changed to target value in a
		gradual fashion, under this case, the total time of gradual change is specified in third
		parameter "duration".

	"duration" specifies the total time of the gradual changing.
		The minimum support duration is 30 milliseconds.

	From Yeelight's Inter-operation Specification
*/
func (l *Light) set_ct_abx(ct_value uint, effect string, duration string) error {
	if ct_value < 1700 || ct_value > 6500 {
		return fmt.Errorf("set_ct_abx() failed: ct_value out of range")
	}
	if effect != "sudden" && effect != "smooth" {
		return fmt.Errorf("set_ct_abx() failed: effect must be 'sudden' or 'smooth'")
	}

	durationConv, err := strconv.Atoi(duration)
	if err != nil {
		return fmt.Errorf("set_ct_abx() failed: duration must be an integer")
	}
	if durationConv < 30 {
		return fmt.Errorf("set_ct_abx() failed: duration must be at least 30 ms")
	}

	command := fmt.Sprintf("{\"id\":0,\"method\":\"set_ct_abx\",\"params\":[%v, \"%v\", %v]}", ct_value, effect, duration)
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return fmt.Errorf("set_ct_abx() failed:\n\tresponse from light: %v", string(response))
	}

	return err
}

/*
	"rgb_value" is the target color, whose type is integer.
		It should be expressed in decimal integer ranges from 0 to 16777215 (hex: 0xFFFFFF).
		RED, GREEN, BLUE = 0 up to 255
		rgb_value = RED*65536 + GREEN*256 + BLUE

	"effect" support two values: "sudden" and "smooth".
		If effect is "sudden",  then the color temperature will be changed directly to target value,
		under this case, the third parameter "duration" is ignored.
		If effect is "smooth", then the color temperature will be changed to target value in a
		gradual fashion, under this case, the total time of gradual change is specified in third
		parameter "duration".

	"duration" specifies the total time of the gradual changing.
		The minimum support duration is 30 milliseconds.

	From Yeelight's Inter-operation Specification
*/
func (l *Light) set_rgb(rgb_value uint32, effect string, duration string) error {
	if rgb_value > 16777215 {
		return fmt.Errorf("set_rgb() failed: rgb_value out of range")
	}

	command := fmt.Sprintf("{\"id\":0,\"method\":\"set_rgb\",\"params\":[%v, \"%v\", %v]}", rgb_value, effect, duration)
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return fmt.Errorf("set_rgb() failed:\n\tresponse from light: %v", string(response))
	}
	return err
}

/*
	"hue" is the target hue value, whose type is integer.
		It should be expressed in decimal integer ranges from 0 to 359.

	"sat" is the target saturation value whose type is integer.
		It's range is 0 to 100.

	"effect" support two values: "sudden" and "smooth".
		If effect is "sudden",  then the color temperature will be changed directly to target value,
		under this case, the third parameter "duration" is ignored.
		If effect is "smooth", then the color temperature will be changed to target value in a
		gradual fashion, under this case, the total time of gradual change is specified in third
		parameter "duration".

	"duration" specifies the total time of the gradual changing.
		The minimum support duration is 30 milliseconds.

	From Yeelight's Inter-operation Specification

*/
func (l *Light) set_hsv(hue uint16, sat uint8, effect string, duration string) error {
	if hue > 359 {
		return fmt.Errorf("set_hsv() failed: hue out of range")
	}
	if sat > 100 {
		return fmt.Errorf("set_hsv() failed: sat out of range")
	}

	command := fmt.Sprintf("{\"id\":0,\"method\":\"set_hsv\",\"params\":[%v, %v, \"%v\", %v]}", hue, sat, effect, duration)
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return fmt.Errorf("set_hsv() failed:\n\tresponse from light: %v", string(response))
	}

	return err
}

/*
	"brightness" is the target brightness.
		The type is integer and ranges from 1 to 100. The brightness is a
		percentage instead of a absolute value. 100 means maximum brightness
		while 1 means the minimum brightness.

	"effect" support two values: "sudden" and "smooth".
		If effect is "sudden",  then the color temperature will be changed directly to target value,
		under this case, the third parameter "duration" is ignored.
		If effect is "smooth", then the color temperature will be changed to target value in a
		gradual fashion, under this case, the total time of gradual change is specified in third
		parameter "duration".

	"duration" specifies the total time of the gradual changing.
		The minimum support duration is 30 milliseconds.

	From Yeelight's Inter-operation Specification
*/
func (l *Light) set_bright(brightness uint8, effect string, duration string) error {
	if brightness < 1 || brightness > 100 {
		return fmt.Errorf("set_bright() failed: brightness out of range")
	}

	command := fmt.Sprintf("{\"id\":0,\"method\":\"set_bright\",\"params\":[%v, \"%v\", %v]}", brightness, effect, duration)
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return fmt.Errorf("set_bright() failed:\n\tresponse from light: %v", string(response))
	}

	return err
}

/*
	"power" can only be "on" or "off".
		"on" means turn on the smart LED,
        "off" means turn off the smart LED.

	"effect" support two values: "sudden" and "smooth".
		If effect is "sudden", then the color temperature will be changed directly to target value,
		under this case, the third parameter "duration" is ignored.
		If effect is "smooth", then the color temperature will be changed to target value in a
		gradual fashion, under this case, the total time of gradual change is specified in third
		parameter "duration".

	"duration" specifies the total time of the gradual changing. The minimum support duration is
	30 milliseconds.

	"mode" (optional):
		0: Normal turn on operation (default value)
		1: Turn on and switch to CT mode.
		2: Turn on and switch to RGB mode.
		3: Turn on and switch to HSV mode.
		4: Turn on and switch to color flow mode.
        5: Turn on and switch to Night light mode. (Ceiling light only).

	From Yeelight's Inter-operation Specification
*/
func (l *Light) set_power(power string, effect string, duration string, mode string) error {
	if len(mode) == 0 {
		mode = "0"
	}
	command := fmt.Sprintf("{\"id\":0,\"method\":\"set_power\",\"params\":[\"%v\", \"%v\", %v, %v]}", power, effect, duration, mode)
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return errors.New("set_power failed")
	}

	return err
}

func (l *Light) toggle() error {
	command := fmt.Sprintf("{\"id\":0,\"method\":\"toggle\",\"params\":[]}")
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return errors.New("toggle failed")
	}

	return err
}

/*
	"count" is the total number of visible state changing before color flow stopped.
		0 means infinite loop on the state changing.

	"action" is the action taken after the flow is stopped.
		0 means smart LED recover to the state before the color flow started.
		1 means smart LED stay at the state when the flow is stopped.
		2 means turn off the smart LED after the flow is stopped.

	"flow_expression" is the expression of the state changing series

	From Yeelight's Inter-operation Specification
*/
func (l *Light) start_cf(count uint64, action uint8, flow_expression string) error {
	if action > 2 {
		return fmt.Errorf("start_cf() failed: action out of range")
	}

	command := fmt.Sprintf("{\"id\":0,\"method\":\"start_cf\",\"params\":[%v, %v, \"%v\"]}", count, action, flow_expression)
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		fmt.Printf("start_cf() failed:\n\tresponse from light: %v", string(response))
	}

	return err
}

func (l *Light) stop_cf() error {
	command := fmt.Sprintf("{\"id\":0,\"method\":\"stop_cf\",\"params\":[]}")
	response, err := l.sendCommand(command, 10)

	if !strings.Contains(string(response), "ok") {
		return errors.New("stop_cf failed")
	}

	return err
}

/*
	"class" can be "color", "hsv", "ct", "cf", "auto_dealy_off".
		"color" means change the smart LED to specified color and brightness.
	    "hsv" means change the smart LED to specified color and brightness.
	    "ct" means change the smart LED to specified ct and brightness.
	    "cf" means start a color flow in specified fashion.
	    "auto_delay_off" means turn on the smart LED to specified brightness and
		start a sleep timer to turn off the light after the specified minutes.

	"val1", "val2", "val3" are class specific.

	From Yeelight's Inter-operation Specification
*/
func (l *Light) set_scene(scene string, val1 string, val2 string, val3 string) error {
	panic("not implemented")
}

/*
	"action" the direction of the adjustment. The valid value can be:
		“increase": increase the specified property
		“decrease": decrease the specified property
		“circle": increase the specified property, after it reaches the max
		value, go back to minimum value

	"prop" the property to adjust. The valid value can be:
        “bright": adjust brightness.
		“ct": adjust color temperature.
        “color": adjust color. (When “prop" is “color", the “action" can only
		be “circle", otherwise, it will be deemed as invalid request.)

	From Yeelight's Inter-operation Specification
*/

func (l *Light) set_adjust(action string, prop string) {
	panic("not implemented")
}

/*
	"action" the action of set_music command. The valid value can be:
        0: turn off music mode.
        1: turn on music mode.
    "host" the IP address of the music server.
    "port" the TCP port music application is listening on.
*/
func (l *Light) set_music(action string, host string, port string) {
	panic("not implemented")
}

/*
   "name" the name of the device.
*/
func (l *Light) set_name(name string) {
	panic("not implemented")
}

func (as *AppState) publishProp(light *Light) {
	// print
	// fmt.Printf("%v%+v\n", light.Name, light.latestState)

	// publish using mqtt

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

		"main/on":          fmt.Sprintf("%v", light.latestState.on),
		"main/on/name":     "Power",
		"main/on/datatype": "boolean",
		"main/on/settable": "true",

		"main/bright":          fmt.Sprintf("%v", light.latestState.bright),
		"main/bright/name":     "Brightness",
		"main/bright/datatype": "integer",
		"main/bright/settable": "true",
		"main/bright/unit":     "%",
		"main/bright/format":   "1:100",

		"main/ct":          fmt.Sprintf("%v", light.latestState.ct),
		"main/ct/name":     "Color Temperature",
		"main/ct/datatype": "integer",
		"main/ct/settable": "true",
		"main/ct/unit":     "K",
		"main/ct/format":   "1700:6500",

		"main/rgb":          fmt.Sprintf("%v", light.latestState.rgb),
		"main/rgb/name":     "RGB color",
		"main/rgb/datatype": "integer",
		"main/rgb/settable": "true",
		"main/rgb/format":   "0:16777215",

		"main/hue":          fmt.Sprintf("%v", light.latestState.hue),
		"main/hue/name":     "Hue",
		"main/hue/datatype": "integer",
		"main/hue/settable": "true",
		"main/hue/format":   "0:359",

		"main/sat":          fmt.Sprintf("%v", light.latestState.sat),
		"main/sat/name":     "Saturation",
		"main/sat/datatype": "integer",
		"main/sat/settable": "true",
		"main/sat/format":   "0:100",

		"main/color_mode":          fmt.Sprintf("%v", light.latestState.color_mode),
		"main/color_mode/name":     "Color Mode",
		"main/color_mode/datatype": "string",
		"main/color_mode/settable": "true",
		"main/color_mode/format":   "RGB,CT,HSV,Flow", // might not be according to Homie spec, but I believe it is useful

		"main/flowing":          fmt.Sprintf("%v", light.latestState.flowing),
		"main/flowing/name":     "Flowing",
		"main/flowing/datatype": "boolean",
		"main/flowing/settable": "true",

		"main/delayoff":          fmt.Sprintf("%v", light.latestState.delayoff),
		"main/delayoff/name":     "Delay Off",
		"main/delayoff/datatype": "integer",
		"main/delayoff/settable": "true",
		"main/delayoff/unit":     "minutes",
		"main/delayoff/format":   "0:60",

		"main/flow_params":          fmt.Sprintf("%v", light.latestState.flow_params),
		"main/flow_params/name":     "Flow Parameters",
		"main/flow_params/datatype": "string",
		"main/flow_params/settable": "true",

		"main/music_on":          fmt.Sprintf("%v", light.latestState.music_on),
		"main/music_on/name":     "Music On",
		"main/music_on/datatype": "boolean",
		"main/music_on/settable": "false",

		"main/name":          fmt.Sprintf("%v", light.latestState.name),
		"main/name/name":     "Name",
		"main/name/datatype": "string",
		"main/name/settable": "false",

		"main/nl_br":          fmt.Sprintf("%v", light.latestState.nl_br),
		"main/nl_br/name":     "Moonlight Brightness",
		"main/nl_br/datatype": "integer",
		"main/nl_br/settable": "true",
		"main/nl_br/unit":     "%",
		"main/nl_br/format":   "1:100",

		"main/moonlight_on":          fmt.Sprintf("%v", light.latestState.moonlight_on),
		"main/moonlight_on/name":     "Moonlight On",
		"main/moonlight_on/datatype": "boolean",
		"main/moonlight_on/settable": "true",

		"bg/bg_power":          fmt.Sprintf("%v", light.latestState.bg_power),
		"bg/bg_power/name":     "Power",
		"bg/bg_power/datatype": "boolean",
		"bg/bg_power/settable": "true",

		"bg/bg_flowing":          fmt.Sprintf("%v", light.latestState.bg_flowing),
		"bg/bg_flowing/name":     "Flowing",
		"bg/bg_flowing/datatype": "boolean",
		"bg/bg_flowing/settable": "true",

		"bg/bg_flow_params":          fmt.Sprintf("%v", light.latestState.bg_flow_params),
		"bg/bg_flow_params/name":     "Flow Parameters",
		"bg/bg_flow_params/datatype": "string",
		"bg/bg_flow_params/settable": "true",

		"bg/bg_ct":          fmt.Sprintf("%v", light.latestState.bg_ct),
		"bg/bg_ct/name":     "Color Temperature",
		"bg/bg_ct/datatype": "integer",
		"bg/bg_ct/settable": "true",
		"bg/bg_ct/unit":     "K",
		"bg/bg_ct/format":   "1700:6500",

		"bg/bg_lmode":          fmt.Sprintf("%v", light.latestState.bg_lmode),
		"bg/bg_lmode/name":     "Light Mode",
		"bg/bg_lmode/datatype": "string",
		"bg/bg_lmode/settable": "true",
		"bg/bg_lmode/format":   "RGB,CT,HSV,Flow",

		"bg/bg_bright":          fmt.Sprintf("%v", light.latestState.bg_bright),
		"bg/bg_bright/name":     "Brightness",
		"bg/bg_bright/datatype": "integer",
		"bg/bg_bright/settable": "true",
		"bg/bg_bright/unit":     "%",
		"bg/bg_bright/format":   "1:100",

		"bg/bg_rgb":          fmt.Sprintf("%v", light.latestState.bg_rgb),
		"bg/bg_rgb/name":     "RGB color",
		"bg/bg_rgb/datatype": "integer",
		"bg/bg_rgb/settable": "true",
		"bg/bg_rgb/format":   "0:16777215",

		"bg/bg_hue":          fmt.Sprintf("%v", light.latestState.bg_hue),
		"bg/bg_hue/name":     "Hue",
		"bg/bg_hue/datatype": "integer",
		"bg/bg_hue/settable": "true",
		"bg/bg_hue/format":   "0:359",
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

func (as *AppState) publishSingleProp(light *Light, topic string, payload interface{}) {
	baseTopic := fmt.Sprintf("%v/%v/", as.MQTTSettings.BaseTopic, light.Name)
	fmt.Printf("%v%v = %v\n", baseTopic, topic, payload)
	as.mqttClient.Publish(baseTopic+topic, byte(as.MQTTSettings.QoS), true, payload)
}

func (as *AppState) subProp(light *Light) {
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
				fmt.Printf("Error while processing '%v -> %v': not 'true' or 'false'\n", message.Topic(), string(message.Payload()))
			}

			// change stuff
			err := light.set_power(yeelightBool, "smooth", "500", "")
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.on = internalBool
			as.publishSingleProp(light, "main/on", fmt.Sprintf("%v", light.latestState.on))
		},

		"main/bright/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			brightness, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}
			if brightness > math.MaxUint8 {
				fmt.Printf("Error while processing '%v -> %v': brightness too high\n", message.Topic(), string(message.Payload()))
				return
			}

			// change stuff
			err = light.set_bright(uint8(brightness), "smooth", "500")
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.bright = uint8(brightness)
			as.publishSingleProp(light, "main/bright", fmt.Sprintf("%v", light.latestState.bright))
		},

		"main/ct/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			ct, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				fmt.Printf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = light.set_ct_abx(uint(ct), "smooth", "500")
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.ct = uint16(ct)
			as.publishSingleProp(light, "main/ct", fmt.Sprintf("%v", light.latestState.ct))
		},

		"main/rgb/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			rgb, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				fmt.Printf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = light.set_rgb(uint32(rgb), "smooth", "500")
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.rgb = uint32(rgb)
			as.publishSingleProp(light, "main/rgb", fmt.Sprintf("%v", light.latestState.rgb))
		},

		"main/hue/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			hue, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				fmt.Printf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = light.set_hsv(uint16(hue), light.latestState.sat, "smooth", "500")
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.hue = uint16(hue)
			as.publishSingleProp(light, "main/hue", fmt.Sprintf("%v", light.latestState.hue))
		},

		"main/sat/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			sat, err := strconv.Atoi(string(message.Payload()))
			if err != nil {
				fmt.Printf("'%v -> %v': Error while converting to int: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			err = light.set_hsv(light.latestState.hue, uint8(sat), "smooth", "500")
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.sat = uint8(sat)
			as.publishSingleProp(light, "main/sat", fmt.Sprintf("%v", light.latestState.sat))

		},

		"main/color_mode/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload
			colorMode, err := colorModeFromString(string(message.Payload()))
			if err != nil {
				fmt.Printf("'%v -> %v': Error while converting to colorMode: %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// change stuff
			powerMode := ""
			switch colorMode {
			case colorModeRGB:
				powerMode = "2"
			case colorModeHSV:
				powerMode = "3"
			case colorModeCT:
				powerMode = "1"
			case colorModeFlow:
				powerMode = "4"
			}

			state := ""
			if light.latestState.on {
				state = "on"
			} else {
				state = "off"
			}

			err = light.set_power(state, "smooth", "500", powerMode)
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
			}

			// update state
			light.latestState.color_mode = colorMode
			as.publishSingleProp(light, "main/color_mode", fmt.Sprintf("%v", light.latestState.color_mode))
		},

		"main/flowing/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"main/delayoff/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"main/flow_params/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		// "set/main/music_on" : func(client mqtt.Client, message mqtt.Message) {},
		// "set/main/name" : func(client mqtt.Client, message mqtt.Message) {},

		"main/nl_br/set": func(client mqtt.Client, message mqtt.Message) {
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
				fmt.Printf("'%v -> %v': Error while converting to bool\n", message.Topic(), string(message.Payload()))
			}

			// change stuff
			state := ""
			if light.latestState.on {
				state = "on"
			} else {
				state = "off"
			}

			err := light.set_power(state, "smooth", "500", mode)
			if err != nil {
				fmt.Printf("Error while processing '%v -> %v': %v\n", message.Topic(), string(message.Payload()), err)
				return
			}

			// update state
			light.latestState.moonlight_on = str == "true"
		},

		"bg/bg_power/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_flowing/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_flow_params/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_ct/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_lmode/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_bright/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_rgb/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},

		"bg/bg_hue/set": func(client mqtt.Client, message mqtt.Message) {
			// verify payload

			// change stuff

			// update state
		},
	}

	baseTopic := fmt.Sprintf("%v/%v/", as.MQTTSettings.BaseTopic, light.Name)

	for topic, callback := range topicsToSubscribe {
		as.mqttClient.Subscribe(baseTopic+topic, 2, callback)
	}
}

func (as *AppState) LoadFromYAML(filename string) error {
	f, err := ioutil.ReadFile(filename)
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
		Lights: []Light{
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

func (as *AppState) propChangeDaemon() {
	propChangeConsumer := func(l *Light) {
		for {
			response := <-l.propChange
			fmt.Printf("PropChangeConsumer caught a change: %v", response)
			// TODO do something with the prop change notification
		}
	}

	for k := range as.Lights {
		as.Lights[k].propChange = make(chan string, 4)
		go propChangeConsumer(&as.Lights[k])
	}
}

// Receive MQTT messages and push the changes to the lights accordingly
func (as *AppState) statePushDaemon() {
	for k := range as.Lights {
		as.subProp(&as.Lights[k])
	}
	fmt.Println("Subscribed to MQTT messages for the lights!")
}

// Poll every light and publish the properties at certain intervals
func (as *AppState) stateDaemon() {
	// trick to make the ticker start immediately
	ticker := time.NewTicker(1 * time.Second)
	fmt.Printf("Initial poll starting...\n")

	go func() {
		for {
			select {
			case <-ticker.C:
				// poll every light and publish the properties
				for k := range as.Lights {
					err := as.Lights[k].get_prop()
					if err != nil {
						fmt.Println(err)
						continue
					}

					as.publishProp(&as.Lights[k])
				}
			}
		}
	}()

	time.Sleep(1250 * time.Millisecond)
	// set real ticker interval after the first tick
	ticker.Reset(time.Duration(as.LightPollingRate.Seconds) * time.Second)
	fmt.Printf("Polling the lights every %vs...\n", as.LightPollingRate.Seconds)
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
		fmt.Println("MQTT protocol not specified in host, using tcp://..")
		as.MQTTSettings.Host = "tcp://" + as.MQTTSettings.Host
	}

	opts.AddBroker(fmt.Sprintf("%s:%d", as.MQTTSettings.Host, as.MQTTSettings.Port))
	opts.SetUsername(as.MQTTSettings.User)
	opts.SetPassword(as.MQTTSettings.Password)

	fmt.Printf("Connecting to MQTT broker %v...\n", as.MQTTSettings.Host)
	as.mqttClient = mqtt.NewClient(opts)
	if token := as.mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	fmt.Println("Connection to MQTT broker established!")
	return nil
}

func main() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)

	go func() {
		select {
		case _ = <-c:
			sendsTotal := float32(unsuccessfulSends) + float32(successfulSends)

			// calculate the value of failure rate in percent
			failureRate := float32(unsuccessfulSends) / sendsTotal * 100

			fmt.Printf("Unsuccessful sends: %v, successful sends: %v, failure rate %.2f%%", unsuccessfulSends, successfulSends, failureRate)
			os.Exit(1)
		}
	}()

	fmt.Printf("yeelight2mqtt %v starting...\n", VERSION)

	as := AppState{}

	err := as.LoadFromYAML("config.yaml")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("Config.yaml doesn't exist, creating a sample config.yaml...")
			err = CreateConfig("config.yaml")
			if err != nil {
				log.Fatalf("%v", err)
			}
			fmt.Println("Change the values in config.yaml as needed and start yeelight2mqtt again.")

			return
		}

		log.Fatalf("An error has occured while trying to load config.yaml: %v", err)
	}

	err = as.mqttInit()
	if err != nil {
		log.Fatalf("An error has occured while trying to initialize MQTT: %v", err)
	}

	as.propChangeDaemon()
	as.stateDaemon()
	as.statePushDaemon()

	// block indefinitely
	<-(chan int)(nil)
}
