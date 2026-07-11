package ring

// NotifyFunc, when set (by the ReTouch plugin layer), shows a doorbell or
// motion notification on the speaker's display. The agent never touches the
// display itself — ReTouch's display API owns the panel. nil (standalone
// runs) means no display notifications.
var NotifyFunc func(kind, device string)
