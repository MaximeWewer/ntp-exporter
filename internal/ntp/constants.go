package ntp

import "time"

// DelayBetweenSamples is the default delay between consecutive NTP samples
// sent to the same server (ntpd rejects packets closer than 2s with "limited")
const DelayBetweenSamples = 2 * time.Second
