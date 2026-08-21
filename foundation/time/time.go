package time

import "time"

const vdaTimeLayout = "2006-01-02T15:04:05.000Z"

// Time now in vda format
func Now() string {
	return time.Now().UTC().Format(vdaTimeLayout)
}
