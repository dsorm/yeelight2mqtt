package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/dsorm/yeelight2mqtt/console"
	"log"
	"reflect"
	"strconv"
	"strings"
)

func (l *Light) sendVerify(funcName string, command string, params ...interface{}) error {
	formattedCommand := fmt.Sprintf(command, params...)
	response, err := l.SendCommand(formattedCommand, 10)

	if err != nil {
		return err
	}

	if !strings.Contains(string(response), "ok") {
		return fmt.Errorf("%v() failed:\n\tresponse from light: %v", funcName, string(response))
	}

	return nil
}

func (l *Light) GetProp() error {
	command := "{\"id\":0,\"method\":\"get_prop\",\"params\":[\"power\", \"bright\", \"ct\", \"rgb\", \"hue\", \"sat\", \"color_mode\", \"flowing\", \"delayoff\", \"flow_params\", \"music_on\", \"name\", \"bg_power\", \"bg_flowing\", \"bg_flow_params\", \"bg_ct\", \"bg_lmode\", \"bg_bright\", \"bg_rgb\", \"bg_hue\", \"bg_sat\", \"nl_br\", \"active_mode\"]}"
	resp, err := l.SendCommand(command, 3)

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
		return fmt.Errorf("GetProp() failed: output from light doesn't contain result")
	}

	// make sure m["result"] is the right type
	if reflect.TypeOf(m["result"]).Kind() != reflect.Slice {
		return fmt.Errorf("GetProp() failed: %v", m["result"])
	}
	result := m["result"].([]interface{})
	if len(result) < 21 {
		return fmt.Errorf("GetProp() failed: result too small (expected at least 21, got %v), result is %v\n", len(result), result)
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
			console.Logf("atoi: %v\n", err)
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
			console.Logf("atoi: %v\n", err)
		}
		return uint32(num)
	}
	lp := LightProperties{
		On:             result[0].(string) == "on",
		Bright:         atouint8(result[1]),
		Ct:             atouint16(result[2]),
		RGB:            atouint32(result[3]),
		Hue:            atouint16(result[4]),
		Sat:            atouint8(result[5]),
		Color_Mode:     ColorMode(atouint8(result[6])),
		Flowing:        result[7].(string) == "1",
		Delayoff:       atouint8(result[8]),
		Flow_Params:    result[9].(string),
		Music_On:       result[10].(string) == "1",
		Name:           result[11].(string),
		Bg_On:          result[12].(string) == "on",
		Bg_Flowing:     result[13].(string) == "1",
		Bg_Flow_Params: result[14].(string),
		Bg_Ct:          atouint16(result[15]),
		Bg_Color_Mode:  ColorMode(atouint8(result[16])),
		Bg_Bright:      atouint8(result[17]),
		Bg_RGB:         atouint32(result[18]),
		Bg_Hue:         atouint16(result[19]),
		Bg_Sat:         atouint8(result[20]),
		Nl_Br:          atouint8(result[21]),
		Moonlight_On:   result[22].(string) == "1",
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
func (l *Light) SetCtAbx(ct_value uint, effect string, duration string) error {
	if ct_value < 1700 || ct_value > 6500 {
		return fmt.Errorf("SetCtAbx() failed: ct_value out of range")
	}
	if effect != "sudden" && effect != "smooth" {
		return fmt.Errorf("SetCtAbx() failed: effect must be 'sudden' or 'smooth'")
	}

	durationConv, err := strconv.Atoi(duration)
	if err != nil {
		return fmt.Errorf("SetCtAbx() failed: duration must be an integer")
	}
	if durationConv < 30 {
		return fmt.Errorf("SetCtAbx() failed: duration must be at least 30 ms")
	}

	err = l.sendVerify("SetCtAbx", "{\"id\":0,\"method\":\"set_ct_abx\",\"params\":[%v, \"%v\", %v]}", ct_value, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Ct = uint16(ct_value)
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) SetRGB(rgb_value uint32, effect string, duration string) error {
	if rgb_value > 16777215 {
		return fmt.Errorf("SetRGB() failed: rgb_value out of range")
	}

	err := l.sendVerify("SetRGB", "{\"id\":0,\"method\":\"set_rgb\",\"params\":[%v, \"%v\", %v]}", rgb_value, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.RGB = rgb_value
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) SetHSV(hue uint16, sat uint8, effect string, duration string) error {
	if hue > 359 {
		return fmt.Errorf("SetHSV() failed: hue out of range")
	}
	if sat > 100 {
		return fmt.Errorf("SetHSV() failed: sat out of range")
	}

	err := l.sendVerify("SetHSV", "{\"id\":0,\"method\":\"set_hsv\",\"params\":[%v, %v, \"%v\", %v]}", hue, sat, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Hue = hue
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) SetBright(brightness uint8, effect string, duration string) error {
	if brightness < 1 || brightness > 100 {
		return fmt.Errorf("SetBright() failed: brightness out of range")
	}

	err := l.sendVerify("SetBright", "{\"id\":0,\"method\":\"set_bright\",\"params\":[%v, \"%v\", %v]}", brightness, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Bright = brightness
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) SetPower(power string, effect string, duration string, mode string) error {
	if len(mode) == 0 {
		mode = "0"
	}

	err := l.sendVerify("SetPower", "{\"id\":0,\"method\":\"set_power\",\"params\":[\"%v\", \"%v\", %v, %v]}", power, effect, duration, mode)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.On = power == "on"
	l.latestState.Color_Mode, _ = ColorModeFromString(mode)
	l.stateMutex.Unlock()
	return nil
}

func (l *Light) Toggle() error {
	err := l.sendVerify("Toggle", "{\"id\":0,\"method\":\"toggle\",\"params\":[]}")
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.On = !l.latestState.On
	l.stateMutex.Unlock()
	return nil
}

func (l *Light) SetDefault() error {
	panic("not implemented")
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
func (l *Light) StartCf(count uint64, action uint8, flow_expression string) error {
	if action > 2 {
		return fmt.Errorf("StartCf() failed: action out of range")
	}

	err := l.sendVerify("StartCf", "{\"id\":0,\"method\":\"start_cf\",\"params\":[%v, %v, \"%v\"]}", count, action, flow_expression)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Flowing = true
	l.latestState.Flow_Params = flow_expression
	return nil
}

func (l *Light) StopCf() error {
	err := l.sendVerify("StopCf", "{\"id\":0,\"method\":\"stop_cf\",\"params\":[]}")
	if err != nil {
		return nil
	}

	l.stateMutex.Lock()
	l.latestState.Flowing = false
	l.latestState.Flow_Params = ""
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) SetScene(scene string, val1 string, val2 string, val3 string) error {
	panic("not implemented")
}

/*
"type2" currently can only be 0. (means power off)
"value" is the length of the timer (in minutes).

# From Yeelight's Inter-operation Specification

"type" was omited since type is a reserved keyword in Go, and it doesn't even serve any purpose, because it can only be set to 0
*/
func (l *Light) CronAdd(value string) error {
	panic("not implemented")
}

/*
"type2" the type of the cron job. (currently only support 0)

# From Yeelight's Inter-operation Specification

"type" was omited since type is a reserved keyword in Go, and it doesn't even serve any purpose, because it can only be set to 0
*/
func (l *Light) CronGet(type2 string) error {
	panic("not implemented")
}

/*
"type2" the type of the cron job. (currently only support 0)

# From Yeelight's Inter-operation Specification

"type" was omited since type is a reserved keyword in Go, and it doesn't even serve any purpose, because it can only be set to 0
*/
func (l *Light) CronDel(type2 string) error {
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
func (l *Light) SetAdjust(action string, prop string) {
	panic("not implemented")
}

/*
		"action" the action of SetMusic command. The valid value can be:
	        0: turn off music mode.
	        1: turn on music mode.
	    "host" the IP address of the music server.
	    "port" the TCP port music application is listening on.
*/
func (l *Light) SetMusic(action string, host string, port string) {
	panic("not implemented")
}

/*
"name" the name of the device.
*/
func (l *Light) SetName(name string) {
	panic("not implemented")
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
func (l *Light) BgSetCtAbx(ct_value uint, effect string, duration string) error {
	if ct_value < 1700 || ct_value > 6500 {
		return fmt.Errorf("BgSetCtAbx() failed: ct_value out of range")
	}
	if effect != "sudden" && effect != "smooth" {
		return fmt.Errorf("BgSetCtAbx() failed: effect must be 'sudden' or 'smooth'")
	}

	durationConv, err := strconv.Atoi(duration)
	if err != nil {
		return fmt.Errorf("BgSetCtAbx() failed: duration must be an integer")
	}
	if durationConv < 30 {
		return fmt.Errorf("BgSetCtAbx() failed: duration must be at least 30 ms")
	}

	err = l.sendVerify("BgSetCtAbx", "{\"id\":0,\"method\":\"bg_set_ct_abx\",\"params\":[%v, \"%v\", %v]}", ct_value, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Bg_Ct = uint16(ct_value)
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) BgSetRGB(rgb_value uint32, effect string, duration string) error {
	if rgb_value > 16777215 {
		return fmt.Errorf("SetRGB() failed: rgb_value out of range")
	}

	err := l.sendVerify("BgSetRGB", "{\"id\":0,\"method\":\"bg_set_rgb\",\"params\":[%v, \"%v\", %v]}", rgb_value, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Bg_RGB = rgb_value
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) BgSetHSV(hue uint16, sat uint8, effect string, duration string) error {
	if hue > 359 {
		return fmt.Errorf("SetHSV() failed: hue out of range")
	}
	if sat > 100 {
		return fmt.Errorf("SetHSV() failed: sat out of range")
	}

	err := l.sendVerify("BgSetHSV", "{\"id\":0,\"method\":\"bg_set_hsv\",\"params\":[%v, %v, \"%v\", %v]}", hue, sat, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Bg_Hue = hue
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) BgSetPower(power string, effect string, duration string, mode string) error {
	if len(mode) == 0 {
		mode = "0"
	}

	err := l.sendVerify("BgSetPower", "{\"id\":0,\"method\":\"bg_set_power\",\"params\":[\"%v\", \"%v\", %v, %v]}", power, effect, duration, mode)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Bg_On = power == "on"
	l.latestState.Bg_Color_Mode, _ = ColorModeFromString(mode)
	l.stateMutex.Unlock()
	return nil
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
func (l *Light) BgSetBright(brightness uint8, effect string, duration string) error {
	if brightness < 1 || brightness > 100 {
		return fmt.Errorf("SetBright() failed: brightness out of range")
	}

	err := l.sendVerify("BgSetBright", "{\"id\":0,\"method\":\"bg_set_bright\",\"params\":[%v, \"%v\", %v]}", brightness, effect, duration)
	if err != nil {
		return err
	}

	l.stateMutex.Lock()
	l.latestState.Bg_Bright = brightness
	l.stateMutex.Unlock()
	return nil
}
