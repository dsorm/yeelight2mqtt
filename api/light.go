package api

import (
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
