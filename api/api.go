package api

// WARNING: only changes the internal state of the light, does not send a command to the light
func (l *Light) GetState() LightProperties {
	l.stateMutex.Lock()
	ls := l.latestState
	l.stateMutex.Unlock()
	return ls
}
