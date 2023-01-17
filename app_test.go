package main

import (
	"github.com/lainio/err2/try"
	"os"
	"testing"
)

func TestApp_Run(t *testing.T) {
	try.To(os.Setenv("LOG_LEVEL", "DEBUG"))
	try.To(os.Setenv("POLL_SINCE", "2400h"))
	try.To(os.Setenv("DOCKER_HOST", "tcp://localhost:2375"))
	try.To(os.Setenv("CONTAINER_ID", "cec48cc41b5c21034cc4103c49dfed319df49d60fd92a1fef5d130c359c0482f"))
	app := try.To1(NewAppBuilder().Build())
	try.To(app.Run())
}
