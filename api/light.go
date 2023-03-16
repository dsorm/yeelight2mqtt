package api

import (
	"fmt"
	"net"
	"sync"
)

type Light struct {
	Host string
	Name string

	stateMutex  sync.Mutex
	latestState LightProperties
	conn        net.Conn
	connMutex   sync.Mutex

	// When there is a change in Yeelight props, it is automatically sent by the light back to yeelight2mqtt
	// yeelight2mqtt only reads from the connection when a request is made or because of automatic polling,
	// so this channel is used to get rid of the data (when handling another request), so a dedicated goroutine may handle it
	refreshMsg      chan string
	refreshCallback func(message string)
}

type LightProperties struct {
	On             bool      // "on" or "off"
	Bright         uint8     // (range 1 - 100)
	Ct             uint16    // (range 1700 - 6500) (unit: Kelvin)
	RGB            uint32    // (range 0 - 16777215)
	Hue            uint16    // (range 0 - 359)
	Sat            uint8     // (range 0 - 100)
	Color_Mode     ColorMode // 0: RGB mode, 1: Color temperature mode, 2: HSV mode, 3: Flow mode
	Flowing        bool
	Delayoff       uint8 // (range 1 - 60 minutes)
	Flow_Params    string
	Music_On       bool
	Name           string
	Bg_On          bool
	Bg_Flowing     bool
	Bg_Flow_Params string
	Bg_Ct          uint16
	Bg_Color_Mode  ColorMode
	Bg_Bright      uint8
	Bg_RGB         uint32
	Bg_Hue         uint16
	Bg_Sat         uint8
	Nl_Br          uint8 // (range 1 - 100)
	Moonlight_On   bool
}

type ColorMode uint8

const (
	ColorModeRGB ColorMode = iota
	ColorModeCT
	ColorModeHSV
	ColorModeFlow
)

func (cm ColorMode) String() string {
	switch cm {
	case ColorModeRGB:
		return "RGB"
	case ColorModeCT:
		return "CT"
	case ColorModeHSV:
		return "HSV"
	case ColorModeFlow:
		return "Flow"
	}
	return ""
}

func ColorModeFromString(str string) (ColorMode, error) {
	switch str {
	case "RGB":
		return ColorModeRGB, nil
	case "CT":
		return ColorModeCT, nil
	case "HSV":
		return ColorModeHSV, nil
	case "Flow":
		return ColorModeFlow, nil
	}
	return 0, nil
}
func (l *Light) String() string {
	// prints out all the possible properties of Light and it's parameters
	str := ""
	str += fmt.Sprintf("Host: %s\n", l.Host)
	str += fmt.Sprintf("Name: %s\n", l.Name)
	str += fmt.Sprintf("latestState: %s\n", l.latestState)
	str += fmt.Sprintf("conn: %s\n", l.conn)
	str += fmt.Sprintf("connMutex: %s\n", l.connMutex)
	str += fmt.Sprintf("refreshMsg: %s\n", l.refreshMsg)
	str += fmt.Sprintf("refreshCallback: %s\n", l.refreshCallback)
	return str
}

func (lp *LightProperties) String() string {
	// prints out all the possible properties of LightProperties and it's parameters
	str := ""
	str += fmt.Sprintf("On: %t\n", lp.On)
	str += fmt.Sprintf("Bright: %d\n", lp.Bright)
	str += fmt.Sprintf("Ct: %d\n", lp.Ct)
	str += fmt.Sprintf("RGB: %d\n", lp.RGB)
	str += fmt.Sprintf("Hue: %d\n", lp.Hue)
	str += fmt.Sprintf("Sat: %d\n", lp.Sat)
	str += fmt.Sprintf("Color_Mode: %d\n", lp.Color_Mode)
	str += fmt.Sprintf("Flowing: %t\n", lp.Flowing)
	str += fmt.Sprintf("Delayoff: %d\n", lp.Delayoff)
	str += fmt.Sprintf("Flow_Params: %s\n", lp.Flow_Params)
	str += fmt.Sprintf("Music_On: %t\n", lp.Music_On)
	str += fmt.Sprintf("Name: %s\n", lp.Name)
	str += fmt.Sprintf("Bg_On: %t\n", lp.Bg_On)
	str += fmt.Sprintf("Bg_Flowing: %t\n", lp.Bg_Flowing)
	str += fmt.Sprintf("Bg_Flow_Params: %s\n", lp.Bg_Flow_Params)
	str += fmt.Sprintf("Bg_Ct: %d\n", lp.Bg_Ct)
	str += fmt.Sprintf("Bg_Color_Mode: %d\n", lp.Bg_Color_Mode)
	str += fmt.Sprintf("Bg_Bright: %d\n", lp.Bg_Bright)
	str += fmt.Sprintf("Bg_RGB: %d\n", lp.Bg_RGB)
	str += fmt.Sprintf("Bg_Hue: %d\n", lp.Bg_Hue)
	str += fmt.Sprintf("Bg_Sat: %d\n", lp.Bg_Sat)
	str += fmt.Sprintf("Nl_Br: %d\n", lp.Nl_Br)
	str += fmt.Sprintf("Moonlight_On: %t\n", lp.Moonlight_On)
	return str
}
